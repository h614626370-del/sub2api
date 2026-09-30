package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type bps403AdmissionCache struct {
	service.GatewayCache
	blocked map[string]bool
}

func (s *bps403AdmissionCache) SetCyberSessionBlocked(_ context.Context, _ string, keys []string, _ time.Duration) error {
	for _, key := range keys {
		s.blocked[key] = true
	}
	return nil
}
func (s *bps403AdmissionCache) IsCyberSessionScopeActive(context.Context, string) (bool, error) {
	return false, nil
}
func (s *bps403AdmissionCache) FindCyberSessionBlocked(_ context.Context, keys []string) (string, error) {
	for _, key := range keys {
		if s.blocked[key] {
			return key, nil
		}
	}
	return "", nil
}

func TestBPS403HTTPAdmissionIndependentOfCyber(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, enabled := range []bool{true, false} {
		settings := map[string]string{service.SettingKeyCyberSessionBlockEnabled: "false"}
		if enabled {
			settings[service.SettingKeyBPS403SessionBlockEnabled] = "true"
		}
		settingSvc := service.NewSettingService(&contentModerationHandlerSettingRepo{values: settings}, nil)
		cache := &bps403AdmissionCache{blocked: map[string]bool{}}
		cfg := &config.Config{}
		gateway := service.NewOpenAIGatewayService(nil, nil, nil, nil, nil, nil, nil, cache, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, nil, nil, &service.DeferredService{}, nil, nil, nil, nil, nil, settingSvc, nil)
		h := &OpenAIGatewayHandler{gatewayService: gateway}
		seed, _ := gin.CreateTestContext(httptest.NewRecorder())
		seed.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		seed.Request.Header.Set("session_id", "blocked-session")
		identity := service.ResolveCyberSessionIdentity(7, seed, []byte(`{}`))
		require.True(t, identity.Resolved())
		sum := sha256.Sum256([]byte("bps403-session:v1|" + identity.BlockKey))
		cache.blocked[hex.EncodeToString(sum[:])] = true
		for _, format := range []cyberSessionBlockFormat{cyberBlockFormatResponses, cyberBlockFormatChat, cyberBlockFormatAnthropic} {
			for _, tc := range []struct {
				key     int64
				session string
				blocked bool
			}{
				{7, "blocked-session", enabled}, {8, "blocked-session", false}, {7, "new-session", false}, {7, "", false},
			} {
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				c.Request.Header.Set("session_id", tc.session)
				require.Equal(t, tc.blocked, h.rejectIfCyberSessionBlocked(c, &service.APIKey{ID: tc.key}, []byte(`{}`), "gpt-5.4", format))
				if tc.blocked {
					require.Equal(t, http.StatusForbidden, rec.Code)
					require.Equal(t, service.BPS403SessionBlockedCode, gjson.Get(rec.Body.String(), "error.code").String())
					if format == cyberBlockFormatAnthropic {
						require.Equal(t, "error", gjson.Get(rec.Body.String(), "type").String())
					}
				} else {
					require.False(t, c.Writer.Written())
				}
			}
		}
	}
}
