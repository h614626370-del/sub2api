package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestBPSUnnamedBuiltinToolsCompatibility(t *testing.T) {
	for _, declaration := range []string{
		`"tools":[{"type":"tool_search"},{"type":"web_search"},{"type":"future_builtin"}],"input":"hi"`,
		`"input":[{"role":"developer","type":"additional_tools","tools":[{"type":"tool_search"}]},{"role":"user","content":"hi"}]`,
	} {
		t.Run(declaration, func(t *testing.T) {
			s, a := bpsFixture()
			c, _ := bpsContext(1, "/responses")
			body := []byte(`{"model":"gpt-6-astra",` + declaration + `}`)
			r, err := s.prepareOpenAIBPS(c.Request.Context(), c, a, body)
			require.NoError(t, err)
			require.Empty(t, r.Tools)
			require.NotContains(t, string(r.Body), "tool_search")
			require.False(t, gjson.GetBytes(r.Body, "tools").Exists())

			upstream := &bpsHTTPStub{contentType: "application/json", body: bpsJSON(bpsResponse(bpsText("hello")))}
			s.httpUpstream = upstream
			_, err = s.forwardOpenAIBPS(c.Request.Context(), c, a, body)
			require.NoError(t, err)
			require.Equal(t, 1, upstream.requests)
		})
	}
}

func TestBPSMixedToolDeclarationCompatibility(t *testing.T) {
	s, a := bpsFixture()
	c, _ := bpsContext(1, "/responses")
	body := []byte(`{"model":"gpt-6-astra","input":"hi","tools":[
		{"type":"tool_search"},
		{"type":"namespace","name":"functions","tools":[
			{"type":"tool_search"},
			{"type":"function","name":"exec","parameters":{"type":"object"}},
			{"type":"custom","name":"apply_patch","format":{"type":"text"}}
		]}
	]}`)
	r, err := s.prepareOpenAIBPS(c.Request.Context(), c, a, body)
	require.NoError(t, err)
	require.Len(t, r.Tools, 2)
	require.NotNil(t, r.Tools["functions.exec"].Schema)
	require.Equal(t, "custom", r.Tools["functions.apply_patch"].Kind)
	require.NotContains(t, string(r.Body), "tool_search")
	response, err := s.bpsTransformResponse(c.Request.Context(), a, r,
		bpsResponse(bpsNative("mixed-call", "functions.exec", map[string]any{})))
	require.NoError(t, err)
	call := bpsTestValue[map[string]any](t, bpsTestValue[[]any](t, response["output"])[0])
	require.Equal(t, "function_call", call["type"])
	require.Equal(t, "functions", call["namespace"])
	require.Equal(t, "exec", call["name"])
}

func TestBPSNamedUnknownToolCompatibility(t *testing.T) {
	s, a := bpsFixture()
	c, _ := bpsContext(1, "/responses")
	r, err := s.prepareOpenAIBPS(c.Request.Context(), c, a, []byte(`{
		"model":"gpt-6-astra","input":"hi","tools":[
			{"type":"tool_search","name":"search","parameters":{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}},
			{"name":"implicit_function"}
		]}`))
	require.NoError(t, err)
	require.Equal(t, "tool_search", r.Tools["search"].Kind)
	require.NotNil(t, r.Tools["search"].Schema)
	require.Equal(t, "function", r.Tools["implicit_function"].Kind)
	response, err := s.bpsTransformResponse(c.Request.Context(), a, r,
		bpsResponse(bpsNative("named-call", "search", map[string]any{"query": "files"})))
	require.NoError(t, err)
	call := bpsTestValue[map[string]any](t, bpsTestValue[[]any](t, response["output"])[0])
	require.Equal(t, "function_call", call["type"], "generic transport is not native tool_search support")
	require.Equal(t, "search", call["name"])
	_, err = s.bpsTransformResponse(c.Request.Context(), a, r,
		bpsResponse(bpsNative("invalid-call", "search", map[string]any{})))
	require.ErrorContains(t, err, "schema")
}

func TestBPSToolCompatibilityPreservesValidation(t *testing.T) {
	for _, declaration := range []string{
		`[{"type":"function"}]`,
		`[{"type":"custom"}]`,
		`[{"type":"namespace","tools":[]}]`,
		`[{"name":""}]`,
		`[null]`,
		`[{"type":"future","name":"remote","parameters":{"$ref":"file:///etc/passwd"}}]`,
		`[{"type":"future","name":"duplicate"},{"type":"function","name":"duplicate"}]`,
	} {
		t.Run(declaration, func(t *testing.T) {
			s, a := bpsFixture()
			c, _ := bpsContext(1, "/responses")
			_, err := s.prepareOpenAIBPS(c.Request.Context(), c, a, []byte(
				`{"model":"gpt-6-astra","input":"hi","tools":`+declaration+`}`))
			require.Error(t, err)
		})
	}
	s, a := bpsFixture()
	c, _ := bpsContext(1, "/responses")
	_, err := s.prepareOpenAIBPS(c.Request.Context(), c, a, []byte(
		`{"model":"gpt-6-astra","input":"hi","tools":[{"type":"tool_search"}],"tool_choice":"required"}`))
	require.ErrorContains(t, err, "at least one client tool")
}
