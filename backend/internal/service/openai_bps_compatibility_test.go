package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestBPSCompatibilityNestedFunctionsAndNamespaces(t *testing.T) {
	s, a := bpsFixture()
	c, _ := bpsContext(1, "/responses")
	body := []byte(`{"model":"gpt-6-astra","input":"inspect","tools":[{"type":"namespace","name":"one","tools":[{"type":"function","function":{"name":"read","parameters":{"type":"object"}}}]},{"type":"namespace","name":"two","tools":[{"type":"function","name":"read"}]}],"tool_choice":{"type":"function","namespace":"one","function":{"name":"read"}}}`)
	r, err := s.prepareOpenAIBPS(c.Request.Context(), c, a, body)
	require.NoError(t, err)
	require.True(t, r.RequireTool)
	require.Contains(t, r.Tools, "one.read")
	require.NotContains(t, r.Tools, "two.read")
	r, err = s.prepareOpenAIBPS(c.Request.Context(), c, a, []byte(`{"model":"gpt-6-astra","input":"continue"}`))
	require.NoError(t, err)
	require.Len(t, r.Tools, 2, "forced selection must not overwrite the persisted full directory")
}

func TestBPSCompatibilityDeclarationsAndIsolation(t *testing.T) {
	s, a := bpsFixture()
	c, _ := bpsContext(1, "/responses")
	prepare := func(body string) (*bpsRequest, error) {
		return s.prepareOpenAIBPS(c.Request.Context(), c, a, []byte(body))
	}
	for _, role := range []string{"user", "assistant", ""} {
		_, err := prepare(`{"model":"gpt-6-astra","input":[{"type":"additional_tools","role":"` + role + `","tools":[{"type":"function","name":"injected"}]}]}`)
		require.ErrorContains(t, err, "must have role developer")
	}
	_, err := prepare(`{"model":"gpt-6-astra","input":"hi","tools":[{"type":"web_search"}]}`)
	require.ErrorContains(t, err, `Unsupported BPS tool type "web_search" at tools[0]`)
	_, err = prepare(`{"model":"gpt-6-astra","input":"hi","tools":[{"type":"function","name":"one","function":{"name":"two"}}]}`)
	require.ErrorContains(t, err, "Conflicting function")
	_, err = prepare(`{"model":"gpt-6-astra","input":[{"type":"additional_tools","role":"developer","tools":[{"type":"custom","name":"exec"}]}]}`)
	require.NoError(t, err)
	r, err := prepare(`{"model":"gpt-6-astra","input":"hi","tool_choice":"none"}`)
	require.NoError(t, err)
	require.Empty(t, r.Tools)
	r, err = prepare(`{"model":"gpt-6-astra","input":"next","tools":[]}`)
	require.NoError(t, err)
	require.Contains(t, r.Tools, "exec")
	for _, key := range []int64{1, 2} {
		other, _ := bpsContext(key, "/responses")
		if key == 1 {
			other.Request.Header.Set("session_id", "another")
		}
		r, err := s.prepareOpenAIBPS(other.Request.Context(), other, a, []byte(`{"model":"gpt-6-astra","input":"next"}`))
		require.NoError(t, err)
		require.Empty(t, r.Tools)
	}
	b := *a
	b.ID++
	r, err = s.prepareOpenAIBPS(c.Request.Context(), c, &b, []byte(`{"model":"gpt-6-astra","input":"next"}`))
	require.NoError(t, err)
	require.Empty(t, r.Tools)
	_, err = prepare(`{"model":"gpt-6-astra","input":[{"type":"additional_tools","role":"developer","tools":[]}]}`)
	require.NoError(t, err)
	r, err = prepare(`{"model":"gpt-6-astra","input":"next"}`)
	require.NoError(t, err)
	require.Empty(t, r.Tools)
}

func TestBPSCompatibilityNoImplicitToolCache(t *testing.T) {
	s, a := bpsFixture()
	c, _ := bpsContext(1, "/responses")
	c.Request.Header.Del("session_id")
	_, err := s.prepareOpenAIBPS(c.Request.Context(), c, a, []byte(`{"model":"gpt-6-astra","input":"same text","tools":[{"type":"function","name":"exec"}]}`))
	require.NoError(t, err)
	r, err := s.prepareOpenAIBPS(c.Request.Context(), c, a, []byte(`{"model":"gpt-6-astra","input":"same text"}`))
	require.NoError(t, err)
	require.Empty(t, r.Tools)
}

func TestBPSCompatibilityCompactPreservesToolDirectory(t *testing.T) {
	s, a := bpsFixture()
	c, _ := bpsContext(1, "/responses")
	_, err := s.prepareOpenAIBPS(c.Request.Context(), c, a, []byte(`{"model":"gpt-6-astra","input":"inspect","tools":[{"type":"function","name":"exec"}]}`))
	require.NoError(t, err)
	compact, _ := bpsContext(1, "/responses/compact")
	r, err := s.prepareOpenAIBPS(compact.Request.Context(), compact, a, []byte(`{"model":"gpt-6-astra","input":[{"role":"developer","type":"additional_tools","tools":[]}]}`))
	require.NoError(t, err)
	require.Empty(t, r.Tools)
	r, err = s.prepareOpenAIBPS(c.Request.Context(), c, a, []byte(`{"model":"gpt-6-astra","input":"continue"}`))
	require.NoError(t, err)
	require.Contains(t, r.Tools, "exec")
}

func TestBPSCompatibilityInvalidSelectionDoesNotReplaceDirectory(t *testing.T) {
	s, a := bpsFixture()
	c, _ := bpsContext(1, "/responses")
	_, err := s.prepareOpenAIBPS(c.Request.Context(), c, a, []byte(`{"model":"gpt-6-astra","input":"inspect","tools":[{"type":"function","name":"exec"}]}`))
	require.NoError(t, err)
	for _, choice := range []string{`[]`, `42`, `{"name":"missing"}`, `{"name":"replacement","function":{"name":"different"}}`} {
		_, err = s.prepareOpenAIBPS(c.Request.Context(), c, a, []byte(`{"model":"gpt-6-astra","input":"inspect","tools":[{"type":"function","name":"replacement"}],"tool_choice":`+choice+`}`))
		require.Error(t, err)
	}
	r, err := s.prepareOpenAIBPS(c.Request.Context(), c, a, []byte(`{"model":"gpt-6-astra","input":"continue"}`))
	require.NoError(t, err)
	require.Contains(t, r.Tools, "exec")
	require.NotContains(t, r.Tools, "replacement")
}

func TestBPSCompatibilityCatalogPreservesOpaqueEntries(t *testing.T) {
	a := bpsModeAccount()
	require.NoError(t, NormalizeOpenAIBPSMode(a, nil))
	body := []byte(`{"models":[null,"opaque",{"slug":"gpt-6-astra","supports_search_tool":true}],"revision":9007199254740993}`)
	updated, changed, err := applyBPSCatalogForAccounts(body, PlatformOpenAI, []Account{*a}, nil, nil, true)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "null", gjson.GetBytes(updated, "models.0").Raw)
	require.Equal(t, "opaque", gjson.GetBytes(updated, "models.1").String())
	require.False(t, gjson.GetBytes(updated, "models.2.supports_search_tool").Bool())
	require.Equal(t, "9007199254740993", gjson.GetBytes(updated, "revision").Raw)
}

func TestBPSCompatibilityCatalogModeAndRoutes(t *testing.T) {
	a := bpsModeAccount()
	require.NoError(t, NormalizeOpenAIBPSMode(a, nil))
	a.Credentials["model_mapping"] = map[string]any{"alias": "gpt-6-astra", "ordinary": "gpt-5.5"}
	b := *a
	b.ID = 2
	b.Extra = map[string]any{}
	accounts := []Account{*a, b}
	body := []byte(`{"models":[{"slug":"alias","supports_search_tool":true,"input_modalities":["text","image"],"experimental_supported_tools":["web_search"]},{"slug":"ordinary","supports_search_tool":true}],"other":"preserved"}`)
	updated, changed, err := applyBPSCatalogForAccounts(body, PlatformOpenAI, accounts, nil, nil, true)
	require.NoError(t, err)
	require.True(t, changed)
	require.False(t, gjson.GetBytes(updated, "models.0.supports_search_tool").Bool())
	require.Equal(t, `["text"]`, gjson.GetBytes(updated, "models.0.input_modalities").Raw)
	require.Equal(t, `[]`, gjson.GetBytes(updated, "models.0.experimental_supported_tools").Raw)
	require.True(t, gjson.GetBytes(updated, "models.1.supports_search_tool").Bool())
	require.Equal(t, "preserved", gjson.GetBytes(updated, "other").String())
	require.NotEqual(t, CodexModelsManifestETag(body), CodexModelsManifestETag(updated))
	_, changed, err = applyBPSCatalogForAccounts(updated, PlatformOpenAI, accounts, nil, nil, true)
	require.NoError(t, err)
	require.False(t, changed)
	g := &Group{Platform: PlatformOpenAI, ModelRoutingEnabled: true, ModelRouting: map[string][]int64{"alias": {2}}}
	updated, changed, err = applyBPSCatalogForAccounts(body, PlatformOpenAI, accounts, g, nil, true)
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, body, updated)
	generated, err := buildCodexModelsManifestForAccounts(PlatformOpenAI, []string{"alias"}, accounts, nil, nil, true)
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(generated, "models.0.supports_search_tool").Bool())
	require.False(t, gjson.GetBytes(generated, "models.0.prefer_websockets").Bool())
}

func TestBPSCompatibilityMergedCatalogETagTracksMode(t *testing.T) {
	a := bpsModeAccount()
	require.NoError(t, NormalizeOpenAIBPSMode(a, nil))
	a.Extra[OpenAIBPSEnabledKey] = false
	accounts := map[int64][]Account{9: {*a}}
	s := &OpenAIGatewayService{accountRepo: codexModelsVisibilityAccountRepo{byGroup: accounts}}
	g := &Group{ID: 9, Platform: PlatformOpenAI}
	body := []byte(`{"models":[{"slug":"gpt-6-astra","supports_search_tool":true}]}`)
	oldTag := CodexModelsManifestETag(body)
	plain := &OpenAIModelsResponse{Body: body, ETag: oldTag}
	require.NoError(t, s.MergeGroupConfiguredCodexModels(context.Background(), g, plain, ""))
	require.True(t, gjson.GetBytes(plain.Body, "models.0.supports_search_tool").Bool())
	accounts[9][0].Extra[OpenAIBPSEnabledKey] = true
	enabled := &OpenAIModelsResponse{Body: body, ETag: oldTag}
	require.NoError(t, s.MergeGroupConfiguredCodexModels(context.Background(), g, enabled, oldTag))
	require.False(t, enabled.NotModified)
	require.False(t, gjson.GetBytes(enabled.Body, "models.0.supports_search_tool").Bool())
	require.NotEqual(t, oldTag, enabled.ETag)
	again := &OpenAIModelsResponse{Body: body, ETag: oldTag}
	require.NoError(t, s.MergeGroupConfiguredCodexModels(context.Background(), g, again, enabled.ETag))
	require.True(t, again.NotModified)
	require.Equal(t, `{"models":[{"slug":"gpt-6-astra","supports_search_tool":true}]}`, string(body), "cached upstream body must remain untouched")
}
