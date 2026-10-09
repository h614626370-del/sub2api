package imagemaster

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestImageMasterTextInputsAndOptions(t *testing.T) {
	for _, input := range []any{
		"  draw a tree  ",
		[]any{map[string]any{"role": "user", "content": "  draw a tree  "}},
		[]any{map[string]any{"role": "user", "type": "message", "content": []any{map[string]any{"type": "input_text", "text": "  draw a tree  "}}}},
	} {
		raw, err := json.Marshal(map[string]any{
			"model": "gpt-5.5", "input": input, "instructions": "Keep the text.",
			"tools": []any{map[string]any{"type": "image_generation", "model": "gpt-image-2", "n": 1,
				"size": "1024x1024", "quality": "high", "background": "transparent",
				"output_format": "webp", "output_compression": 87, "moderation": "auto", "action": "auto", "partial_images": 2}},
		})
		require.NoError(t, err)
		p, err := Prepare(raw)
		require.NoError(t, err)
		body, contentType, err := p.encode(context.Background(), 1<<20, nil)
		require.NoError(t, err)
		require.Equal(t, "application/json", contentType)
		var payload map[string]any
		require.NoError(t, json.Unmarshal(body, &payload))
		require.Equal(t, map[string]any{
			"model": "gpt-image-2", "prompt": "Keep the text.\n\n  draw a tree  ", "n": float64(1),
			"stream": false, "response_format": "b64_json", "size": "1024x1024", "quality": "high",
			"background": "transparent", "output_format": "webp", "output_compression": float64(87), "moderation": "auto",
		}, payload)
	}
}

func TestImageMasterDetectsImageToolAmongInvalidSiblings(t *testing.T) {
	for _, raw := range []string{
		`{"tools":[42,{"type":"image_generation"}]}`,
		`{"tools":[{"type":"image_generation"},{"type":42}]}`,
		`{"tools":[null,{"type":"image_generation"}]}`,
	} {
		require.True(t, HasImageTool([]byte(raw)), raw)
	}
	for _, raw := range []string{`null`, `{"tools":null}`, `{"tools":[42]}`, `{"tools":[{"type":"function"}]}`, `{`} {
		require.False(t, HasImageTool([]byte(raw)), raw)
	}
}

func TestImageMasterUnsupportedTextRequestsNeverDispatch(t *testing.T) {
	m := testManager(t)
	for name, change := range map[string]func(map[string]any, map[string]any){
		"missing image model": func(_ map[string]any, tool map[string]any) { delete(tool, "model") },
		"invalid model":       func(_ map[string]any, tool map[string]any) { tool["model"] = "text-model" },
		"empty prompt":        func(v, _ map[string]any) { v["input"] = " "; v["instructions"] = "not a prompt" },
		"history ID":          func(v, _ map[string]any) { v["previous_response_id"] = "resp_previous" },
		"conversation":        func(v, _ map[string]any) { v["conversation"] = "conv_previous" },
		"prompt template":     func(v, _ map[string]any) { v["prompt"] = map[string]any{"id": "hidden-template"} },
		"unknown top-level":   func(v, _ map[string]any) { v["future_context"] = "do not discard" },
		"text constraint":     func(v, _ map[string]any) { v["text"] = map[string]any{"format": "json"} },
		"reasoning":           func(v, _ map[string]any) { v["reasoning"] = map[string]any{"effort": "high"} },
		"store response":      func(v, _ map[string]any) { v["store"] = true },
		"message history": func(v, _ map[string]any) {
			v["input"] = []any{map[string]any{"role": "user", "id": "msg_old", "content": "draw"}}
		},
		"multiple tools": func(v, tool map[string]any) {
			v["tools"] = []any{tool, map[string]any{"type": "web_search"}}
		},
		"multiple turns": func(v, _ map[string]any) {
			v["input"] = []any{map[string]any{"role": "user", "content": "one"}, map[string]any{"role": "user", "content": "two"}}
		},
		"assistant": func(v, _ map[string]any) { v["input"] = []any{map[string]any{"role": "assistant", "content": "draw"}} },
		"unknown content": func(v, _ map[string]any) {
			v["input"] = []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_file", "file_id": "file_test"}}}}
		},
		"no tool selected":    func(v, _ map[string]any) { v["tool_choice"] = "none" },
		"unknown tool option": func(_ map[string]any, tool map[string]any) { tool["future_instruction"] = "do not lose" },
		"multiple output":     func(_ map[string]any, tool map[string]any) { tool["n"] = 2 },
		"mask without image": func(_ map[string]any, tool map[string]any) {
			tool["input_image_mask"] = map[string]any{"image_url": testImage(t)}
		},
		"edit without image": func(_ map[string]any, tool map[string]any) { tool["action"] = "edit" },
		"bad compression":    func(_ map[string]any, tool map[string]any) { tool["output_compression"] = 1.5 },
		"bad partial images": func(_ map[string]any, tool map[string]any) { tool["partial_images"] = "invalid" },
		"async":              func(v, _ map[string]any) { v["background"] = true },
	} {
		t.Run(name, func(t *testing.T) {
			var v map[string]any
			require.NoError(t, json.Unmarshal(textRequest(false), &v))
			change(v, firstImageTool(t, v))
			raw, err := json.Marshal(v)
			require.NoError(t, err)
			w := serve(t, m, raw, func(http.ResponseWriter, *http.Request) { t.Error("must not dispatch") })
			require.Equal(t, 400, w.Code)
		})
	}
}

func TestImageMasterRejectsUnsafeImageOptions(t *testing.T) {
	for _, change := range []func(map[string]any, map[string]any){
		func(_ map[string]any, tool map[string]any) { tool["action"] = "generate" },
		func(image, _ map[string]any) { image["detail"] = "low" },
		func(image, _ map[string]any) { image["crop"] = "top" },
		func(image, _ map[string]any) { image["file_id"] = "file_hidden" },
	} {
		var body map[string]any
		require.NoError(t, json.Unmarshal(editRequest(t, testImage(t)), &body))
		image := objectAt(t, objectAt(t, body, "input", 0), "content", 1)
		change(image, firstImageTool(t, body))
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		_, err = Prepare(raw)
		require.Error(t, err)
	}
}

func TestImageMasterDirectRequiredErrorsStayExplicitAndRedacted(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, status := range []int{200, 502} {
			m := testManager(t)
			w := serve(t, m, textRequest(stream), func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":{"code":"image_direct_required","message":"secret-upstream-detail"}}`))
			})
			require.Contains(t, w.Body.String(), "image_direct_required")
			require.NotContains(t, w.Body.String(), "secret-upstream-detail")
			require.False(t, strings.Contains(w.Body.String(), "event: response.completed"))
		}
	}
}
