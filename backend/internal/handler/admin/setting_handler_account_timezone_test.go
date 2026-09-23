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

func TestSettingsOAuthTimezoneWriteReadAndHotReload(t *testing.T) {
	key := service.SettingKeyOpenAIOAuthDefaultTimezone
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyOpenAICodexTicketEnabled: "true",
		service.SettingKeyOpenAISolGroupID:         "20",
	})
	ctx := context.Background()
	require.Empty(t, h.settingService.GetOpenAIOAuthDefaultTimezone(ctx))
	rec := doUpdateSettings(t, h, map[string]any{key: " Asia/Shanghai "}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "Asia/Shanghai", repo.values[key])
	require.Equal(t, "Asia/Shanghai", h.settingService.GetOpenAIOAuthDefaultTimezone(ctx))
	require.Equal(t, "true", repo.values[service.SettingKeyOpenAICodexTicketEnabled])
	require.Equal(t, "20", repo.values[service.SettingKeyOpenAISolGroupID])
	require.Contains(t, rec.Body.String(), `"openai_oauth_default_timezone":"Asia/Shanghai"`)
	for _, payload := range []map[string]any{{"site_name": "Updated"}, {key: nil}} {
		rec = doUpdateSettings(t, h, payload, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Equal(t, "Asia/Shanghai", repo.values[key])
	}
	get := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(get)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
	h.GetSettings(c)
	require.Equal(t, http.StatusOK, get.Code)
	require.Contains(t, get.Body.String(), `"openai_oauth_default_timezone":"Asia/Shanghai"`)
	rec = doUpdateSettings(t, h, map[string]any{key: ""}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Empty(t, repo.values[key])
	require.Empty(t, h.settingService.GetOpenAIOAuthDefaultTimezone(ctx))
}

func TestSettingsOAuthTimezoneRejectsInvalidValues(t *testing.T) {
	key := service.SettingKeyOpenAIOAuthDefaultTimezone
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{key: "UTC"})
	for _, invalid := range []string{"Local", "Not/AZone", "+08:00", "UTC\nAsia/Tokyo"} {
		rec := doUpdateSettings(t, h, map[string]any{key: invalid}, nil)
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		require.Equal(t, "UTC", repo.values[key])
	}
}
