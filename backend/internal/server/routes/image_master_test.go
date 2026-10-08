package routes

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/imagemaster"
	"github.com/Wei-Shaw/sub2api/internal/pkg/imagepolicy"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func imageMasterForRoutes(t *testing.T) *imagemaster.Manager {
	t.Helper()
	m, err := imagemaster.New(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(m.Close)
	cfg := m.Config()
	cfg.Enabled = true
	require.NoError(t, m.SaveConfig(cfg))
	return m
}

func TestImageMasterGatewayPreservesAdmissionAndIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, path := range []string{"/v1/responses", "/responses", "/backend-api/codex/responses"} {
		t.Run(path, func(t *testing.T) {
			m := imageMasterForRoutes(t)
			group := &service.Group{ID: 1, Platform: service.PlatformOpenAI, AllowImageGeneration: true, StreamOnly: true,
				ModelAllowlist: service.GroupModelAllowlist{Enabled: true, Models: []string{"gpt-5.5", "gpt-image-2"}}}
			originalCalls, actualCalls := 0, 0
			actual := func(c *gin.Context) {
				actualCalls++
				c.Set("image_master_test_gateway_account", int64(87))
				key, ok := middleware.GetAPIKeyFromContext(c)
				require.True(t, ok)
				require.EqualValues(t, 123, key.ID)
				require.Equal(t, "192.0.2.3", ip.GetTrustedClientIP(c))
				require.True(t, imagepolicy.DirectOnly(c.Request.Context()))
				require.Equal(t, "/v1/images/generations", c.Request.URL.Path)
				c.JSON(200, gin.H{"data": []gin.H{{"b64_json": "image"}}})
			}
			r := gin.New()
			require.NoError(t, r.SetTrustedProxies(nil))
			r.Use(func(c *gin.Context) {
				if c.GetHeader("Authorization") != "Bearer test" {
					c.AbortWithStatus(401)
					return
				}
				id := int64(1)
				c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 123, UserID: 5, GroupID: &id, Group: group})
				c.Next()
			})
			var attributedAccount int64
			r.Use(func(c *gin.Context) {
				c.Next()
				attributedAccount = c.GetInt64("image_master_test_gateway_account")
			})
			r.POST(path, middleware.GroupModelAllowlist(), middleware.GroupStreamOnly(),
				imageMasterMiddleware(m, actual, middleware.GroupModelAllowlist()),
				func(c *gin.Context) {
					require.False(t, imagepolicy.DirectOnly(c.Request.Context()))
					originalCalls++
					c.JSON(200, gin.H{"ordinary": true})
				})
			call := func(raw, auth string) *httptest.ResponseRecorder {
				req := httptest.NewRequest("POST", path, strings.NewReader(raw))
				req.Header.Set("Authorization", auth)
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("X-Forwarded-For", "10.0.0.99")
				req.RemoteAddr = "192.0.2.3:443"
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				return w
			}
			raw := `{"model":"gpt-5.5","stream":true,"input":"test","tools":[{"type":"image_generation","model":"gpt-image-2"}]}`
			require.Equal(t, 401, call(raw, "").Code)
			require.Contains(t, call(raw, "Bearer test").Body.String(), "response.completed")
			require.Equal(t, 1, actualCalls)
			require.EqualValues(t, 87, attributedAccount)
			require.Zero(t, originalCalls)
			malformedTools := `{"model":"gpt-5.5","stream":true,"input":"test","tools":[42,{"type":"image_generation","model":"gpt-image-2"}]}`
			require.Equal(t, 400, call(malformedTools, "Bearer test").Code)
			require.Equal(t, 1, actualCalls)
			require.Zero(t, originalCalls)
			// The original client model AND extracted image model must pass.
			group.ModelAllowlist.Models = []string{"gpt-5.5"}
			require.Contains(t, call(raw, "Bearer test").Body.String(), "response.failed")
			require.Equal(t, 1, actualCalls)
			group.ModelAllowlist.Models = []string{"gpt-image-2"}
			require.Equal(t, 404, call(raw, "Bearer test").Code)
			require.Equal(t, 1, actualCalls)
			group.ModelAllowlist.Enabled = false
			require.Equal(t, 400, call(strings.Replace(raw, `"stream":true`, `"stream":false`, 1), "Bearer test").Code)
			group.AllowImageGeneration = false
			require.Equal(t, 403, call(raw, "Bearer test").Code)
			// Ordinary chat is never converted.
			require.Contains(t, call(`{"model":"gpt-5.5","stream":true,"input":"hello"}`, "Bearer test").Body.String(), "ordinary")
			require.Equal(t, 1, originalCalls)
			cfg := m.Config()
			cfg.Enabled = false
			require.NoError(t, m.SaveConfig(cfg))
			require.Contains(t, call(raw, "Bearer test").Body.String(), "ordinary")
		})
	}
}

func TestImageMasterAdminRoutesRequireSessionAndDoNotAcceptExternalCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	m := imageMasterForRoutes(t)
	r := gin.New()
	admin := r.Group("/api/v1/admin")
	admin.Use(func(c *gin.Context) {
		if c.GetHeader("Authorization") != "Bearer admin-session" {
			c.AbortWithStatus(401)
			return
		}
		c.Next()
	})
	registerImageMasterRoutes(admin, m, middleware.StepUpAuthMiddleware(func(c *gin.Context) {
		if c.GetHeader("X-Test-Step-Up") != "verified" {
			c.AbortWithStatus(403)
			return
		}
		c.Next()
	}))
	call := func(method, path, body, auth string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/v1/admin/image-master"+path, strings.NewReader(body))
		req.Header.Set("Authorization", auth)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	require.Equal(t, 401, call("GET", "", "", "").Code)
	require.Equal(t, 200, call("GET", "", "", "Bearer admin-session").Code)
	raw, _ := json.Marshal(m.Config())
	for field, value := range map[string]any{
		"upstream_url": "forbidden", "sub2api_key": "forbidden", "management_key": "forbidden",
		"max_concurrent": 1, "max_queue": 1, "paused": true,
		"control_model": "gpt-5.6-luna", "direct_edits": true,
	} {
		var cfg map[string]any
		require.NoError(t, json.Unmarshal(raw, &cfg))
		cfg[field] = value
		b, _ := json.Marshal(cfg)
		require.Equal(t, 400, call("PUT", "/settings", string(b), "Bearer admin-session").Code)
	}
	require.Equal(t, 400, call("PUT", "/settings", string(raw)+"{}", "Bearer admin-session").Code)
	require.Equal(t, 200, call("PUT", "/settings", string(raw), "Bearer admin-session").Code)
	require.Equal(t, 403, call("GET", "/requests/123/raw", "", "Bearer admin-session").Code)
	require.Equal(t, 404, call("GET", "/sub2api/accounts", "", "Bearer admin-session").Code)
	require.Equal(t, 404, call("POST", "/pause", `{"paused":true}`, "Bearer admin-session").Code)
	require.Equal(t, 200, call("DELETE", "/requests", "", "Bearer admin-session").Code)
	require.True(t, bytes.HasPrefix(call("GET", "/export", "", "Bearer admin-session").Body.Bytes(), []byte("[")))
}
