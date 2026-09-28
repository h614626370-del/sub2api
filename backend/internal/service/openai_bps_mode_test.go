package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func bpsModeAccount() *Account {
	return &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"access_token": "test-token", "refresh_token": "test-refresh", "chatgpt_account_id": "test-workspace"},
		Extra:       map[string]any{OpenAIBPSEnabledKey: true}}
}

func TestOpenAIBPSModeNormalization(t *testing.T) {
	a := bpsModeAccount()
	require.NoError(t, NormalizeOpenAIBPSMode(a, nil))
	require.Equal(t, OpenAIBPSDefaultModels(), a.Extra[OpenAIBPSModelsKey])
	require.True(t, a.UsesOpenAIBPS("gpt-6-astra"))
	require.False(t, a.UsesOpenAIBPS("other"))
	a.Extra[OpenAIBPSModelsKey] = []any{" custom ", "", "custom", "gpt-5.6-sol"}
	require.NoError(t, NormalizeOpenAIBPSMode(a, nil))
	require.Equal(t, []string{"custom", "gpt-5.6-sol"}, a.Extra[OpenAIBPSModelsKey])
	a.Extra[OpenAIBPSModelsKey] = []any{}
	require.Error(t, NormalizeOpenAIBPSMode(a, nil))
	a.Extra[OpenAIBPSEnabledKey] = false
	require.NoError(t, NormalizeOpenAIBPSMode(a, nil))
	require.False(t, a.UsesOpenAIBPS("custom"))
}

func TestOpenAIBPSModeCannotBypassValidationThroughExtraUpdate(t *testing.T) {
	s := &adminServiceImpl{}
	for _, key := range []string{OpenAIBPSEnabledKey, OpenAIBPSModelsKey} {
		t.Run(key, func(t *testing.T) {
			err := s.UpdateAccountExtra(context.Background(), 1, map[string]any{key: nil})
			require.ErrorContains(t, err, "Update BPS mode through the account edit endpoint")
			_, err = s.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{Extra: map[string]any{key: nil}})
			require.ErrorContains(t, err, "Update BPS mode individually through the account edit endpoint")
		})
	}
}

func TestOpenAIBPSModeRejectsInvalidConfiguration(t *testing.T) {
	for _, raw := range []any{"gpt-6-astra", []any{123}, []any{"gpt-*"}} {
		a := bpsModeAccount()
		a.Extra[OpenAIBPSModelsKey] = raw
		require.Error(t, NormalizeOpenAIBPSMode(a, nil))
	}
	a := bpsModeAccount()
	a.Extra[OpenAIBPSEnabledKey] = "true"
	require.Error(t, NormalizeOpenAIBPSMode(a, nil))
	for _, kind := range []string{AccountTypeAPIKey, AccountTypeSetupToken} {
		a := bpsModeAccount()
		a.Type = kind
		require.Error(t, NormalizeOpenAIBPSMode(a, nil))
		require.False(t, a.OpenAIBPSEnabled())
	}
	a = bpsModeAccount()
	parent := int64(9)
	a.ParentAccountID = &parent
	require.Error(t, NormalizeOpenAIBPSMode(a, nil))
	require.False(t, a.UsesOpenAIBPS("gpt-6-astra"))
}

func TestOpenAIBPSModeUsesMappedModelAndPreservesCredentials(t *testing.T) {
	a := bpsModeAccount()
	a.Credentials["model_mapping"] = map[string]any{"alias": "gpt-6-astra"}
	require.NoError(t, NormalizeOpenAIBPSMode(a, nil))
	require.True(t, a.UsesOpenAIBPS("alias"))
	require.Equal(t, "gpt-6-astra", resolveOpenAIAccountUpstreamModelForRequest(a, "alias", true))
	require.Equal(t, 2, openAICompactSupportTier(a, "alias"))
	require.Equal(t, "test-refresh", a.GetOpenAIRefreshToken())
	previous := a.Extra
	a.Extra = map[string]any{"unrelated": true}
	require.NoError(t, NormalizeOpenAIBPSMode(a, previous))
	require.True(t, a.OpenAIBPSEnabled())
}

func TestOpenAIBPSCooldownDoesNotAffectOrdinaryChannel(t *testing.T) {
	a := bpsModeAccount()
	require.NoError(t, NormalizeOpenAIBPSMode(a, nil))
	require.Equal(t, []string{"bps:gpt-6-astra"}, a.modelRateLimitKeysForRequest(context.Background(), "gpt-6-astra"))
	a.Extra[OpenAIBPSEnabledKey] = false
	require.NotContains(t, a.modelRateLimitKeysForRequest(context.Background(), "gpt-6-astra"), "bps:gpt-6-astra")
}

func TestOpenAIBPSChannelLockDoesNotBiasInitialSelection(t *testing.T) {
	a := bpsModeAccount()
	require.NoError(t, NormalizeOpenAIBPSMode(a, nil))
	plain := bpsModeAccount()
	plain.Extra = nil
	c, _ := bpsContext(1, "/v1/responses")
	c.Request = c.Request.WithContext(WithOpenAIChannelSelection(c.Request.Context()))
	require.True(t, openAIChannelAllowed(c.Request.Context(), a, "gpt-6-astra"))
	require.True(t, openAIChannelAllowed(c.Request.Context(), plain, "gpt-6-astra"))
	require.NoError(t, SelectOpenAIChannel(c, a, "gpt-6-astra"))
	require.False(t, openAIChannelAllowed(c.Request.Context(), plain, "gpt-6-astra"))
	require.Error(t, SelectOpenAIChannel(c, plain, "gpt-6-astra"))
	require.True(t, openAIChannelAllowed(context.Background(), plain, "gpt-6-astra"))
}

func TestOpenAIBPSModeRestoresBoundConversation(t *testing.T) {
	s, _ := bpsFixture()
	a := bpsModeAccount()
	require.NoError(t, NormalizeOpenAIBPSMode(a, nil))
	c, _ := bpsContext(1, "/v1/responses")
	body := bpsRequestBody("hi", false)
	require.NoError(t, s.PrepareOpenAIBPSRouting(c, body))
	require.NoError(t, s.bpsBind(c.Request.Context(), s.bpsScope(c, body), a))
	next, _ := bpsContext(1, "/v1/responses")
	require.NoError(t, s.RestoreOpenAIBPSRouting(next, body))
	require.True(t, OpenAIBPSHasBinding(next.Request.Context()))
	require.True(t, bpsBoundAccountAllowed(next.Request.Context(), a))
	a.Extra[OpenAIBPSEnabledKey] = false
	require.False(t, openAIChannelAllowed(next.Request.Context(), a, "gpt-6-astra"))
	other, _ := bpsContext(2, "/v1/responses")
	require.NoError(t, s.RestoreOpenAIBPSRouting(other, body))
	require.False(t, OpenAIBPSHasBinding(other.Request.Context()))
}

func TestOpenAIBPSModeEndpointBoundary(t *testing.T) {
	a := bpsModeAccount()
	require.NoError(t, NormalizeOpenAIBPSMode(a, nil))
	for _, path := range []string{"/v1/chat/completions", "/v1/messages", "/v1/images/generations"} {
		c, rec := bpsContext(1, path)
		require.Error(t, rejectBPSUnsupportedEndpoint(c, a, bpsRequestBody("hi", false)))
		require.Equal(t, http.StatusBadRequest, rec.Code)
	}
	c, _ := bpsContext(1, "/v1/responses/compact")
	require.NoError(t, rejectBPSUnsupportedEndpoint(c, a, bpsRequestBody("hi", false)))
}

func TestOpenAIBPSRetiredPlatformCannotBeCreated(t *testing.T) {
	_, err := buildAccountForCreate(&CreateAccountInput{Platform: PlatformOpenAIBPS, Type: AccountTypeOAuth}, nil)
	require.ErrorContains(t, err, "OpenAI OAuth")
}
