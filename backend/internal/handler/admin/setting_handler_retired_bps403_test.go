package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestRetiredBPS403SettingsAreIgnored(t *testing.T) {
	keys := []string{"bps403_session_block_enabled", "bps403_capture_enabled", "bps403_session_block_ttl_seconds"}
	for _, legacy := range []bool{false, true} {
		name := "fresh"
		stored := map[string]string{}
		if legacy {
			name = "legacy"
			stored = map[string]string{
				keys[0]: "true",
				keys[1]: "true",
				keys[2]: "120",
			}
		}
		t.Run(name, func(t *testing.T) {
			h, repo := newStepUpSwitchTestHandler(t, stored)
			rec := doUpdateSettings(t, h, map[string]any{
				"site_name":               "Retired settings test",
				"request_capture_enabled": false,
				keys[0]:                   false,
				keys[1]:                   false,
				keys[2]:                   60,
			}, nil)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			require.False(t, gjson.Get(rec.Body.String(), "data.request_capture_enabled").Bool())
			for _, key := range keys {
				require.False(t, gjson.Get(rec.Body.String(), "data."+key).Exists(), key)
				if !legacy {
					require.NotContains(t, repo.values, key)
				}
			}
			if legacy {
				require.Equal(t, "true", repo.values[keys[0]])
				require.Equal(t, "true", repo.values[keys[1]])
				require.Equal(t, "120", repo.values[keys[2]])
			}

			rec = httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
			h.GetSettings(c)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			for _, key := range keys {
				require.False(t, gjson.Get(rec.Body.String(), "data."+key).Exists(), key)
			}
		})
	}
}
