package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSettingsCodexTicketProxyWriteReadAndHotReload(t *testing.T) {
	key := service.SettingKeyOpenAICodexTicketHarvestProxyURL
	oldProxy := "http://user:old-secret@old.example.com:8080"
	newProxy := "socks5h://user:new-secret@new.example.com:1080"
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{key: oldProxy})
	require.Equal(t, oldProxy, h.settingService.GetOpenAICodexTicketHarvestProxyURL(context.Background()))
	rec := doUpdateSettings(t, h, map[string]any{key: newProxy}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, newProxy, repo.values[key])
	require.Equal(t, newProxy, h.settingService.GetOpenAICodexTicketHarvestProxyURL(context.Background()))
	require.NotContains(t, rec.Body.String(), "new-secret")
	require.Contains(t, rec.Body.String(), `"openai_codex_ticket_harvest_proxy_configured":true`)
	// Omission, empty input and the masked GET value all preserve the real secret.
	for _, body := range []map[string]any{{"site_name": "updated"}, {key: ""}, {key: service.MaskProxyURL(newProxy)}} {
		rec = doUpdateSettings(t, h, body, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Equal(t, newProxy, repo.values[key])
	}
	get := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(get)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
	h.GetSettings(c)
	require.Equal(t, http.StatusOK, get.Code)
	require.NotContains(t, get.Body.String(), "new-secret")
	require.Contains(t, get.Body.String(), "new.example.com")
}

func TestSettingsCodexTicketRejectInvalidProxyWithoutLeakingPassword(t *testing.T) {
	key := service.SettingKeyOpenAICodexTicketHarvestProxyURL
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{key: "http://previous.example.com:8080"})
	rec := doUpdateSettings(t, h, map[string]any{key: "ftp://user:invalid-secret@proxy.example.com:21"}, nil)
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.NotContains(t, rec.Body.String(), "invalid-secret")
	require.Equal(t, "http://previous.example.com:8080", repo.values[key])
}

func TestSettingsCodexTicketPolicyHotReloadAndValidation(t *testing.T) {
	key := service.SettingKeyOpenAICodexTicketPolicy
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})
	policy := h.settingService.GetOpenAICodexTicketPolicy(context.Background())
	policy.TTLSeconds = 240
	policy.RefreshBeforeSeconds = 30
	policy.CookieEnabled = true
	policy.CookieRequired = true
	rec := doUpdateSettings(t, h, map[string]any{key: policy}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, policy, h.settingService.GetOpenAICodexTicketPolicy(context.Background()))
	require.Contains(t, rec.Body.String(), `"ttl_seconds":240`)
	stored := repo.values[key]
	rec = doUpdateSettings(t, h, map[string]any{"site_name": "Other setting"}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, stored, repo.values[key])
	policy.RefreshBeforeSeconds = 240
	rec = doUpdateSettings(t, h, map[string]any{key: policy}, nil)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, stored, repo.values[key])
	policy.RefreshBeforeSeconds = 0
	policy.CookieEnabled = false
	rec = doUpdateSettings(t, h, map[string]any{key: policy}, nil)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	policy.CookieRequired = false
	rec = doUpdateSettings(t, h, map[string]any{key: policy}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Zero(t, h.settingService.GetOpenAICodexTicketPolicy(context.Background()).RefreshBeforeSeconds)
}
