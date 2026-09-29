package service

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type bpsRetryStub struct {
	HTTPUpstream
	responses []map[string]any
	bodies    [][]byte
}

func (u *bpsRetryStub) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	data, _ := io.ReadAll(req.Body)
	u.bodies = append(u.bodies, data)
	index := len(u.bodies) - 1
	if index >= len(u.responses) {
		index = len(u.responses) - 1
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(bpsJSON(u.responses[index])))}, nil
}

func TestOpenAIBPSToolCorrectionNeverExposesRejectedOfficeCall(t *testing.T) {
	for _, stream := range []bool{false, true} {
		s, a := bpsFixture()
		invalid := bpsNative("bad", "workbook.edit", map[string]any{"cell": "A1"})
		valid := bpsNative("good", "exec", map[string]any{"command": "pwd"})
		up := &bpsRetryStub{responses: []map[string]any{bpsResponse(invalid), bpsResponse(valid)}}
		s.httpUpstream = up
		c, rec := bpsContext(1, "/responses")
		source := map[string]any{"model": "gpt-6-astra", "input": "inspect", "stream": stream, "tools": []any{bpsFunction("exec")}}
		result, err := s.Forward(c.Request.Context(), c, a, []byte(bpsJSON(source)))
		require.NoError(t, err)
		require.Len(t, up.bodies, 2)
		require.Equal(t, "2", gjson.GetBytes(up.bodies[1], "metadata.agent_iteration").String())
		require.Equal(t, "bad", gjson.GetBytes(up.bodies[1], "input.#(call_id==bad).call_id").String())
		require.NotContains(t, rec.Body.String(), "workbook.edit")
		require.NotContains(t, rec.Body.String(), `"call_id":"bad"`)
		require.Contains(t, rec.Body.String(), `"call_id":"good"`)
		require.Equal(t, 200, result.Usage.InputTokens)
		require.Equal(t, 24, result.Usage.OutputTokens)
	}
}

func TestOpenAIBPSToolCorrectionLimits(t *testing.T) {
	s, a := bpsFixture()
	up := &bpsRetryStub{responses: []map[string]any{bpsResponse(bpsText("no call")), bpsResponse(bpsNative("good", "exec", map[string]any{"command": "pwd"}))}}
	s.httpUpstream = up
	c, rec := bpsContext(1, "/responses")
	source := map[string]any{"model": "gpt-6-astra", "input": "inspect", "tools": []any{bpsFunction("exec")}, "tool_choice": "required"}
	_, err := s.Forward(c.Request.Context(), c, a, []byte(bpsJSON(source)))
	require.NoError(t, err)
	require.Len(t, up.bodies, 2)
	require.Contains(t, rec.Body.String(), `"call_id":"good"`)

	s, a = bpsFixture()
	up = &bpsRetryStub{responses: []map[string]any{bpsResponse(bpsNative("bad", "workbook.edit", map[string]any{"cell": "A1"}))}}
	s.httpUpstream = up
	c, rec = bpsContext(1, "/responses")
	source["stream"] = true
	_, err = s.Forward(c.Request.Context(), c, a, []byte(bpsJSON(source)))
	require.Error(t, err)
	require.Len(t, up.bodies, 4)
	require.NotContains(t, rec.Body.String(), "workbook.edit")
	require.Contains(t, rec.Body.String(), "bps_invalid_tool_call")
}

func TestOpenAIBPSNativeCompactionRequiresBoundAccount(t *testing.T) {
	s, a := bpsFixture()
	c, _ := bpsContext(1, "/responses/compact")
	source := map[string]any{"model": "gpt-6-astra", "input": "first"}
	require.NoError(t, s.PrepareOpenAIBPSRouting(c, []byte(bpsJSON(source))))
	require.NoError(t, s.bpsBind(c.Request.Context(), s.bpsScope(c, []byte(bpsJSON(source))), a))
	prepared, err := s.prepareOpenAIBPS(c.Request.Context(), c, a, []byte(bpsJSON(source)))
	require.NoError(t, err)
	compact, err := s.bpsTransformResponse(c.Request.Context(), a, prepared, bpsResponse(bpsNativeCompaction()))
	require.NoError(t, err)
	source["input"] = compact["output"]
	body := []byte(bpsJSON(source))
	require.NoError(t, s.PrepareOpenAIBPSRouting(c, body))
	_, err = s.prepareOpenAIBPS(c.Request.Context(), c, a, body)
	require.NoError(t, err)
	other := *a
	other.ID = a.ID + 1
	_, err = s.prepareOpenAIBPS(c.Request.Context(), c, &other, body)
	require.Error(t, err)
}

func TestOpenAIBPSCompactSSERequiresMatchingDone(t *testing.T) {
	for _, event := range []string{"", bpsEvent("response.output_item.done", map[string]any{"item": map[string]any{"type": "compaction", "encrypted_content": "different"}, "output_index": 0})} {
		s, a := bpsFixture()
		s.httpUpstream = &bpsHTTPStub{contentType: "text/event-stream", body: event + bpsEvent("response.completed", map[string]any{"response": bpsResponse(bpsNativeCompaction())})}
		c, rec := bpsContext(1, "/responses/compact")
		_, err := s.Forward(c.Request.Context(), c, a, bpsRequestBody("hello", true))
		require.Error(t, err)
		require.Contains(t, rec.Body.String(), "bps_compaction_failed")
		require.NotContains(t, rec.Body.String(), "opaque-native-compaction")
	}
}

func TestOpenAIBPSParallelCallsAndModelAliasAuthorization(t *testing.T) {
	s, a := bpsFixture()
	c, _ := bpsContext(1, "/responses")
	a.Credentials["model_mapping"] = map[string]any{"gpt-5.6-luna": "gpt-5.6-luna"}
	source := map[string]any{"model": "gpt-6-luna", "input": "inspect", "tools": []any{bpsFunction("exec")}}
	r, err := s.prepareOpenAIBPS(c.Request.Context(), c, a, []byte(bpsJSON(source)))
	require.NoError(t, err)
	require.Equal(t, "gpt-5.6-luna", r.Model)
	source["model"] = "gpt-6-astra"
	_, err = s.prepareOpenAIBPS(c.Request.Context(), c, a, []byte(bpsJSON(source)))
	require.Error(t, err)

	a.Credentials["model_mapping"] = map[string]any{"gpt-6-astra": "gpt-6-astra"}
	r, err = s.prepareOpenAIBPS(c.Request.Context(), c, a, []byte(bpsJSON(source)))
	require.NoError(t, err)
	calls := bpsResponse(bpsNative("one", "exec", map[string]any{"command": "pwd"}), bpsNative("two", "exec", map[string]any{"command": "ls"}))
	converted, err := s.bpsTransformResponse(c.Request.Context(), a, r, calls)
	require.NoError(t, err)
	require.Len(t, converted["output"], 2)
	source["parallel_tool_calls"] = false
	r, err = s.prepareOpenAIBPS(c.Request.Context(), c, a, []byte(bpsJSON(source)))
	require.NoError(t, err)
	_, err = s.bpsTransformResponse(c.Request.Context(), a, r, bpsResponse(bpsNative("three", "exec", map[string]any{"command": "pwd"}), bpsNative("four", "exec", map[string]any{"command": "ls"})))
	require.ErrorContains(t, err, "one client tool")
	_, err = s.bpsTransformResponse(c.Request.Context(), a, r, bpsResponse(bpsNative("same", "exec", map[string]any{"command": "pwd"}), bpsNative("same", "exec", map[string]any{"command": "ls"})))
	require.ErrorContains(t, err, "repeated tool call")
}

func TestOpenAIBPSAutomaticCompactionCanContinue(t *testing.T) {
	s, a := bpsFixture()
	c, _ := bpsContext(1, "/responses")
	source := map[string]any{"model": "gpt-6-astra", "input": "first"}
	body := []byte(bpsJSON(source))
	require.NoError(t, s.PrepareOpenAIBPSRouting(c, body))
	require.NoError(t, s.bpsBind(c.Request.Context(), s.bpsScope(c, body), a))
	r, err := s.prepareOpenAIBPS(c.Request.Context(), c, a, body)
	require.NoError(t, err)
	compact := bpsNativeCompaction()
	response, err := s.bpsTransformResponse(c.Request.Context(), a, r, bpsResponse(compact, bpsText("continued")))
	require.NoError(t, err)
	source["input"] = response["output"]
	body = []byte(bpsJSON(source))
	next, _ := bpsContext(1, "/responses")
	require.NoError(t, s.PrepareOpenAIBPSRouting(next, body))
	replay, err := s.prepareOpenAIBPS(next.Request.Context(), next, a, body)
	require.NoError(t, err)
	require.Contains(t, bpsJSON(gjson.GetBytes(replay.Body, "input").Value()), "opaque-native-compaction")
	require.Equal(t, r.TurnID, replay.TurnID)
}
