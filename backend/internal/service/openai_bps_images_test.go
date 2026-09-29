package service

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAIBPSImagesUploadAndRewrite(t *testing.T) {
	s, account := bpsFixture()
	s.httpUpstream = &bpsHTTPStub{
		contentType: "application/json",
		body:        `{"openai_file_id":"file_picture_1"}`,
	}
	pngData := validBPSPNG(t)
	body := []byte(bpsJSON(map[string]any{
		"model": "gpt-6-astra",
		"input": []any{
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "describe"},
					map[string]any{"type": "input_image", "image_url": "data:image/png;base64," + pngData},
				},
			},
		},
	}))
	s.bpsRefuseInline(account, map[string]bool{"message": true})
	rewritten, _, err := s.prepareOpenAIBPSImages(t.Context(), account, body, map[string]bool{})
	require.NoError(t, err)
	require.Equal(t, 1, bpsTestValue[*bpsHTTPStub](t, s.httpUpstream).requests)

	var source map[string]any
	require.NoError(t, json.Unmarshal(rewritten, &source))
	part := source["input"].([]any)[0].(map[string]any)["content"].([]any)[1].(map[string]any)
	require.Equal(t, "file_picture_1", part["file_id"])
	require.Equal(t, "auto", part["detail"])
	require.NotContains(t, part, "image_url")
}

func TestOpenAIBPSImagesCacheAndLimits(t *testing.T) {
	s, account := bpsFixture()
	s.httpUpstream = &bpsHTTPStub{
		contentType: "application/json",
		body:        `{"openai_file_id":"file_picture_1"}`,
	}
	pngData := validBPSPNG(t)
	body := []byte(bpsJSON(map[string]any{
		"model": "gpt-6-astra",
		"input": []any{map[string]any{
			"type": "message", "role": "user",
			"content": []any{map[string]any{"type": "input_image", "image_url": "data:image/png;base64," + pngData}},
		}},
	}))
	s.bpsRefuseInline(account, map[string]bool{"message": true})
	_, _, err := s.prepareOpenAIBPSImages(t.Context(), account, body, map[string]bool{})
	require.NoError(t, err)
	_, _, err = s.prepareOpenAIBPSImages(t.Context(), account, body, map[string]bool{})
	require.NoError(t, err)
	require.Equal(t, 1, bpsTestValue[*bpsHTTPStub](t, s.httpUpstream).requests)

	other := *account
	other.ID++
	s.bpsRefuseInline(&other, map[string]bool{"message": true})
	_, _, err = s.prepareOpenAIBPSImages(t.Context(), &other, body, map[string]bool{})
	require.NoError(t, err)
	require.Equal(t, 2, bpsTestValue[*bpsHTTPStub](t, s.httpUpstream).requests)
}

type bpsPictureUpstream struct {
	HTTPUpstream
	requests     []map[string]any
	uploads      int
	refuseInline bool
	refuseCached bool
}

func (u *bpsPictureUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	raw, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	status, body := 200, bpsJSON(bpsResponse(bpsText("done")))
	if strings.HasSuffix(req.URL.Path, "/attachments") {
		u.uploads++
		body = bpsJSON(map[string]any{"openai_file_id": "file_picture_" + string(rune('0'+u.uploads))})
		if !strings.Contains(req.Header.Get("Content-Type"), "multipart/form-data") || !bytes.Contains(raw, []byte("Content-Type: image/png")) {
			status, body = 400, `{ "error": {"message":"invalid upload"} }`
		}
	} else {
		var sent map[string]any
		if err := json.Unmarshal(raw, &sent); err != nil {
			return nil, err
		}
		u.requests = append(u.requests, sent)
		parts := sent["input"].([]any)
		foundInline, foundFile := false, false
		for _, item := range parts {
			msg, _ := item.(map[string]any)
			content, _ := msg["content"].([]any)
			for _, value := range content {
				part, _ := value.(map[string]any)
				if part["type"] == "input_image" {
					foundInline = part["image_url"] != nil
					foundFile = part["file_id"] != nil
				}
			}
		}
		if (u.refuseInline && foundInline) || (u.refuseCached && foundFile && u.uploads == 1) {
			status, body = 400, `{"error":{"message":"image rejected"}}`
		}
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
}

func TestOpenAIBPSImagesForwardRetryAndCacheRenewal(t *testing.T) {
	s, account := bpsFixture()
	upstream := &bpsPictureUpstream{refuseInline: true}
	s.httpUpstream = upstream
	c, recorder := bpsContext(1, "/v1/responses")
	body := bpsRequestBody([]any{map[string]any{"type": "message", "role": "user",
		"content": []any{map[string]any{"type": "input_image", "image_url": "data:image/png;base64," + validBPSPNG(t)}}}}, false)
	_, err := s.forwardOpenAIBPS(c.Request.Context(), c, account, body)
	require.NoError(t, err)
	require.Equal(t, 200, recorder.Code)
	require.Len(t, upstream.requests, 2)
	require.Equal(t, 1, upstream.uploads)
	require.Equal(t, "file_picture_1", upstream.requests[1]["input"].([]any)[1].(map[string]any)["content"].([]any)[0].(map[string]any)["file_id"])

	upstream.refuseCached = true
	c, _ = bpsContext(1, "/v1/responses")
	_, err = s.forwardOpenAIBPS(c.Request.Context(), c, account, body)
	require.NoError(t, err)
	require.Equal(t, 2, upstream.uploads)
	require.Len(t, upstream.requests, 4)
}

func TestOpenAIBPSImagesInlineAccepted(t *testing.T) {
	s, account := bpsFixture()
	upstream := &bpsPictureUpstream{}
	s.httpUpstream = upstream
	c, recorder := bpsContext(1, "/v1/responses")
	body := bpsRequestBody([]any{map[string]any{"type": "message", "role": "user",
		"content": []any{map[string]any{"type": "input_image", "image_url": map[string]any{
			"url": "data:image/png;base64," + validBPSPNG(t)}}}}}, false)
	_, err := s.forwardOpenAIBPS(c.Request.Context(), c, account, body)
	require.NoError(t, err)
	require.Equal(t, 200, recorder.Code)
	require.Len(t, upstream.requests, 1)
	require.Zero(t, upstream.uploads)
}

func TestOpenAIBPSImagesValidationBeforeUpload(t *testing.T) {
	s, account := bpsFixture()
	upstream := &bpsPictureUpstream{}
	s.httpUpstream = upstream
	c, _ := bpsContext(1, "/v1/responses")
	body := bpsRequestBody([]any{map[string]any{"type": "message", "role": "user",
		"content": []any{map[string]any{"type": "input_image", "image_url": "data:image/png;base64,invalid!"}}}}, false)
	_, err := s.forwardOpenAIBPS(c.Request.Context(), c, account, body)
	require.Error(t, err)
	require.Empty(t, upstream.requests)
	require.Zero(t, upstream.uploads)

	s.bpsRefuseInline(account, map[string]bool{"message": true})
	c, _ = bpsContext(1, "/v1/responses")
	invalid := []byte(`{"model":"gpt-6-astra","input":[{"type":"message","role":"user","content":[{"type":"input_image","image_url":"data:image/png;base64,` + validBPSPNG(t) + `"}]}],"reasoning":{"effort":"invalid"}}`)
	_, err = s.forwardOpenAIBPS(c.Request.Context(), c, account, invalid)
	require.Error(t, err)
	require.Zero(t, upstream.uploads)
}

func validBPSPNG(t *testing.T) string {
	t.Helper()
	var data bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	require.NoError(t, png.Encode(&data, img))
	return base64.StdEncoding.EncodeToString(data.Bytes())
}
