//go:build unit

package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIOAuthBaseURLNormalization(t *testing.T) {
	for _, tc := range []struct {
		raw  any
		want string
		bad  bool
	}{
		{"", "", false},
		{"  https://relay.example/custom/codex/  ", "https://relay.example/custom/codex", false},
		{"https://relay.example/custom/responses/", "https://relay.example/custom", false},
		{"https://relay.example", "https://relay.example", false},
		{"http://relay.example/codex", "http://relay.example/codex", false},
		{"ftp://relay.example", "", true},
		{"https://user:secret@relay.example", "", true},
		{"https://relay.example?token=secret", "", true},
		{"https://relay.example#fragment", "", true},
		{"/relative", "", true},
		{123, "", true},
	} {
		t.Run(fmt.Sprint(tc.raw), func(t *testing.T) {
			account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"base_url": tc.raw}}
			err := normalizeOpenAIOAuthBaseURL(account)
			if tc.bad {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, account.GetCredential("base_url"))
			if tc.want == "" {
				require.NotContains(t, account.Credentials, "base_url")
			}
		})
	}
}

func TestOpenAIOAuthBaseURLRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &OpenAIGatewayService{cfg: &config.Config{}}
	for _, base := range []string{"", "https://relay.example:8443/custom/codex/"} {
		account := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
			Credentials: map[string]any{"base_url": base, "chatgpt_account_id": "account-test"}}
		wantBase := "https://relay.example:8443/custom/codex"
		wantHost := "relay.example:8443"
		if base == "" {
			wantBase = openAICodexBaseURL
			wantHost = "chatgpt.com"
		}
		for _, path := range []string{"/v1/responses", "/v1/responses/compact"} {
			for _, passthrough := range []bool{false, true} {
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, path, nil)
				body := []byte(`{"model":"gpt-5.5","input":[]}`)
				var req *http.Request
				var err error
				if passthrough {
					req, err = svc.buildUpstreamRequestOpenAIPassthrough(context.Background(), c, account, body, "test-token")
				} else {
					req, err = svc.buildUpstreamRequest(context.Background(), c, account, body, "test-token", true, "", true)
				}
				require.NoError(t, err)
				suffix := "/responses"
				if path == "/v1/responses/compact" {
					suffix += "/compact"
				}
				require.Equal(t, wantBase+suffix, req.URL.String())
				require.Equal(t, wantHost, req.Host)
				require.Equal(t, "Bearer test-token", req.Header.Get("Authorization"))
			}
		}
		wsURL, err := svc.buildOpenAIResponsesWSURL(account)
		require.NoError(t, err)
		require.Equal(t, "wss://"+strings.TrimPrefix(wantBase, "https://")+"/responses", wsURL)
		alphaURL, err := svc.openAIAlphaSearchURL(account)
		require.NoError(t, err)
		require.Equal(t, wantBase+"/alpha/search", alphaURL)
		for _, suffix := range []string{"/models", "/images/generations", "/images/edits", "/realtime/calls?intent=quicksilver&architecture=avas"} {
			got, err := svc.resolveOpenAIOAuthURL(context.Background(), account, openAICodexBaseURL+suffix)
			require.NoError(t, err)
			require.Equal(t, wantBase+suffix, got)
		}
	}
}

func TestOpenAIOAuthBaseURLSecurityAndIsolation(t *testing.T) {
	svc := &OpenAIGatewayService{cfg: &config.Config{}}
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Credentials: map[string]any{"base_url": "http://relay.example/codex"}}
	_, err := svc.resolveOpenAIOAuthURL(context.Background(), account, chatgptCodexURL)
	require.Error(t, err, "HTTP must honor existing operator policy")
	svc.cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	got, err := svc.buildOpenAIResponsesWSURL(account)
	require.NoError(t, err)
	require.Equal(t, "ws://relay.example/codex/responses", got)
	svc.cfg.Security.URLAllowlist.Enabled = true
	svc.cfg.Security.URLAllowlist.UpstreamHosts = []string{"allowed.example"}
	account.Credentials["base_url"] = "https://blocked.example/codex"
	_, err = svc.resolveOpenAIOAuthURL(context.Background(), account, chatgptCodexURL)
	require.Error(t, err)

	account.Platform = PlatformOpenAIBPS
	got, err = svc.resolveOpenAIOAuthURL(context.Background(), account, OpenAIBPSResponsesURL)
	require.NoError(t, err)
	require.Equal(t, OpenAIBPSResponsesURL, got)
	account.Platform = PlatformOpenAI
	account.Type = AccountTypeAPIKey
	got, err = svc.resolveOpenAIOAuthURL(context.Background(), account, openaiPlatformAPIURL)
	require.NoError(t, err)
	require.Equal(t, openaiPlatformAPIURL, got)
}

func TestOpenAIOAuthBaseURLUpdateAndClear(t *testing.T) {
	repo := &updateAccountCredsRepoStub{account: &Account{ID: 77, Platform: PlatformOpenAI,
		Type: AccountTypeOAuth, Status: StatusActive,
		Credentials: map[string]any{"access_token": "existing-at", "refresh_token": "existing-rt"}}}
	svc := &adminServiceImpl{accountRepo: repo}
	for _, base := range []string{" https://relay.example/codex/ ", ""} {
		updated, err := svc.UpdateAccount(context.Background(), 77, &UpdateAccountInput{Credentials: map[string]any{"base_url": base}})
		require.NoError(t, err)
		require.Equal(t, "existing-at", updated.GetCredential("access_token"))
		require.Equal(t, "existing-rt", updated.GetCredential("refresh_token"))
		if base == "" {
			require.NotContains(t, updated.Credentials, "base_url")
		} else {
			require.Equal(t, "https://relay.example/codex", updated.GetCredential("base_url"))
		}
	}
	_, err := svc.UpdateAccount(context.Background(), 77, &UpdateAccountInput{Credentials: map[string]any{"base_url": "https://user:secret@relay.example"}})
	require.Error(t, err)
	require.Equal(t, 2, repo.updateCalls)
}

func TestOpenAIOAuthBaseURLShadowUsesParent(t *testing.T) {
	parent := Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"base_url": "https://parent.example/codex"}}
	shadow := &Account{ID: 8, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &parent.ID}
	svc := &OpenAIGatewayService{accountRepo: stubOpenAIAccountRepo{accounts: []Account{parent}}}
	got, err := svc.resolveOpenAIOAuthURL(context.Background(), shadow, chatgptCodexURL)
	require.NoError(t, err)
	require.Equal(t, "https://parent.example/codex/responses", got)
}

func TestOpenAIOAuthBaseURLHarvest(t *testing.T) {
	account := ticketTestAccount(41)
	account.Credentials["base_url"] = "https://harvest.example/custom"
	called := false
	upstream := &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
		called = true
		require.Equal(t, "https://harvest.example/custom/responses", req.URL.String())
		require.Equal(t, "harvest.example", req.Host)
		require.Equal(t, "Bearer test-token", req.Header.Get("Authorization"))
		return codexTicketResponse(), nil
	}}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, upstream)
	_, _, _, _, err := svc.fireOpenAICodexTicketProbe(context.Background(), account, "test-token", "gpt-6-astra", "", ticketProbeChallenge(t), time.Second)
	require.NoError(t, err)
	require.True(t, called)
}

func TestOpenAIOAuthBaseURLModels(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		require.Equal(t, "/custom/models", r.URL.Path)
		require.Equal(t, "Bearer test-access-token", r.Header.Get("Authorization"))
		require.Equal(t, "0.137.0", r.URL.Query().Get("client_version"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"slug":"gpt-5.5","display_name":"GPT-5.5"}]}`))
	}))
	defer server.Close()
	account := newCodexModelsTestAccount()
	account.Credentials["base_url"] = server.URL + "/custom"
	svc := &OpenAIGatewayService{cfg: &config.Config{}}
	svc.cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	manifest, err := svc.FetchCodexModelsManifest(context.Background(), account, "0.137.0", "")
	require.NoError(t, err)
	require.Contains(t, string(manifest.Body), "gpt-5.5")
	require.True(t, called)
}

func TestOpenAIOAuthBaseURLCreate(t *testing.T) {
	account, err := buildAccountForCreate(&CreateAccountInput{Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Credentials: map[string]any{"base_url": " https://relay.example/codex/responses/ ", "access_token": "test"}}, nil)
	require.NoError(t, err)
	require.Equal(t, "https://relay.example/codex", account.GetCredential("base_url"))
	_, err = buildAccountForCreate(&CreateAccountInput{Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Credentials: map[string]any{"base_url": "not-a-url"}}, nil)
	require.Error(t, err)
}
