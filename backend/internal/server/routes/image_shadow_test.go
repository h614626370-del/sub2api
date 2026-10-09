package routes

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/imagemaster"
	"github.com/Wei-Shaw/sub2api/internal/pkg/imagepolicy"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type shadowTestKeys struct{ key *service.APIKey }

func (s shadowTestKeys) GetByID(context.Context, int64) (*service.APIKey, error) { return s.key, nil }

func TestShadowGatewayLeavesOriginalUntouchedAndReauthenticates(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, path := range []string{"/v1/responses", "/responses", "/backend-api/codex/responses"} {
		t.Run(path, func(t *testing.T) {
			m := imageMasterForRoutes(t) // Both toggles on: shadow takes precedence.
			cfg := m.Config()
			cfg.Shadow = imagemaster.ShadowConfig{Enabled: true, GroupID: 9, APIKeyID: 90}
			require.NoError(t, m.SaveConfig(cfg))
			groupID := int64(9)
			testKey := &service.APIKey{ID: 90, UserID: 99, Key: "synthetic-test", GroupID: &groupID, Group: &service.Group{ID: 9, Platform: service.PlatformOpenAI, AllowImageGeneration: true}, Status: service.StatusAPIKeyActive}
			entered, release := make(chan struct{}), make(chan struct{})
			var imageCalls atomic.Int32
			auth := middleware.APIKeyAuthMiddleware(func(c *gin.Context) {
				require.Equal(t, "Bearer synthetic-test", c.GetHeader("Authorization"))
				require.Empty(t, c.GetHeader("Cookie"))
				require.Empty(t, c.GetHeader("X-Forwarded-For"))
				_, inherited := c.Get("private-original-key")
				require.False(t, inherited)
				c.Set(string(middleware.ContextKeyAPIKey), testKey)
				c.Next()
			})
			images := func(c *gin.Context) {
				imageCalls.Add(1)
				require.True(t, imagepolicy.DirectOnly(c.Request.Context()))
				key, _ := middleware.GetAPIKeyFromContext(c)
				require.EqualValues(t, 99, key.UserID)
				close(entered)
				<-release
				c.JSON(200, gin.H{"data": []gin.H{{"b64_json": "synthetic-image"}}})
			}
			r := gin.New()
			r.Use(func(c *gin.Context) {
				source := int64(2)
				c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 7, UserID: 8, GroupID: &source, Group: &service.Group{ID: 2, Platform: service.PlatformOpenAI, AllowImageGeneration: true}})
				c.Set("private-original-key", "original")
				c.Next()
			})
			raw := `{"model":"gpt-5.5","stream":true,"input":"same prompt","tools":[{"type":"image_generation","model":"gpt-image-2"}]}`
			responseBody := "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"image_generation_call\",\"status\":\"completed\",\"result\":\"original-image\"}]}}\n\ndata: [DONE]\n\n"
			r.POST(path, imageShadowMiddleware(m, shadowTestKeys{testKey}, auth, images), imageMasterMiddleware(m, images), func(c *gin.Context) {
				b, err := io.ReadAll(c.Request.Body)
				require.NoError(t, err)
				require.Equal(t, raw, string(b))
				require.Equal(t, "Bearer original-secret", c.GetHeader("Authorization"))
				require.False(t, imagepolicy.DirectOnly(c.Request.Context()))
				c.Header("Content-Type", "text/event-stream")
				c.Header("X-Original", "unchanged")
				_, _ = c.Writer.WriteString(responseBody[:20])
				c.Writer.Flush()
				_, _ = c.Writer.Write([]byte(responseBody[20:]))
			})
			req := httptest.NewRequest("POST", path, strings.NewReader(raw))
			req.Header.Set("Authorization", "Bearer original-secret")
			req.Header.Set("Cookie", "original-cookie")
			req.Header.Set("X-Forwarded-For", "192.0.2.4")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			require.Equal(t, responseBody, w.Body.String())
			require.Equal(t, "unchanged", w.Header().Get("X-Original"))
			require.True(t, w.Flushed)
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("no shadow request")
			}
			close(release)
			require.Eventually(t, func() bool { return m.ShadowSnapshot().Active == 0 }, time.Second, 10*time.Millisecond)
			require.EqualValues(t, 1, imageCalls.Load())
			row := m.ShadowSnapshot().Items[0]
			require.Equal(t, "completed", row.Original.Outcome)
			require.Equal(t, "completed", row.Test.Outcome)
		})
	}
}

func TestShadowRejectsOriginalIdentityOrGroupWithoutBlockingUser(t *testing.T) {
	for _, kind := range []string{"same_user", "same_group", "changed_group", "auth_failure"} {
		t.Run(kind, func(t *testing.T) {
			m := imageMasterForRoutes(t)
			cfg := m.Config()
			cfg.Enabled = false
			cfg.Shadow = imagemaster.ShadowConfig{Enabled: true, GroupID: 9, APIKeyID: 90}
			require.NoError(t, m.SaveConfig(cfg))
			gid, source := int64(9), int64(2)
			uid := int64(99)
			switch kind {
			case "same_user":
				uid = 8
			case "same_group":
				source = 9
			case "changed_group":
				gid = 10
			}
			key := &service.APIKey{ID: 90, UserID: uid, GroupID: &gid, Group: &service.Group{Platform: service.PlatformOpenAI}}
			auth := middleware.APIKeyAuthMiddleware(func(c *gin.Context) { c.AbortWithStatus(401) })
			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{UserID: 8, GroupID: &source, Group: &service.Group{Platform: service.PlatformOpenAI}})
			})
			r.POST("/responses", imageShadowMiddleware(m, shadowTestKeys{key}, auth, func(c *gin.Context) { t.Error("must not generate") }), func(c *gin.Context) { c.String(200, "original") })
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("POST", "/responses", strings.NewReader(`{"model":"gpt-5.5","input":"test","tools":[{"type":"image_generation","model":"gpt-image-2"}]}`)))
			require.Equal(t, "original", w.Body.String())
			require.Equal(t, 200, w.Code)
			require.Eventually(t, func() bool { return m.ShadowSnapshot().Active == 0 }, time.Second, 10*time.Millisecond)
			require.Equal(t, "failed", m.ShadowSnapshot().Items[0].Test.Outcome)
		})
	}
}

type shadowFailingBody struct{ read bool }

func (b *shadowFailingBody) Read(p []byte) (int, error) {
	if b.read {
		return 0, errors.New("synthetic body error")
	}
	b.read = true
	return copy(p, "prefix"), nil
}
func (*shadowFailingBody) Close() error { return nil }
func TestShadowReadErrorsStayWithOriginalHandler(t *testing.T) {
	m := imageMasterForRoutes(t)
	cfg := m.Config()
	cfg.Shadow = imagemaster.ShadowConfig{Enabled: true, GroupID: 9, APIKeyID: 90}
	require.NoError(t, m.SaveConfig(cfg))
	r := gin.New()
	r.Use(func(c *gin.Context) {
		gid := int64(2)
		c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{GroupID: &gid, Group: &service.Group{Platform: service.PlatformOpenAI}})
	})
	r.POST("/responses", imageShadowMiddleware(m, shadowTestKeys{}, middleware.APIKeyAuthMiddleware(func(c *gin.Context) {}), func(c *gin.Context) {}), func(c *gin.Context) {
		b, err := io.ReadAll(c.Request.Body)
		require.Equal(t, "prefix", string(b))
		require.EqualError(t, err, "synthetic body error")
		c.String(400, "original read failure")
	})
	req := httptest.NewRequest("POST", "/responses", nil)
	req.Body = &shadowFailingBody{}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, "original read failure", w.Body.String())
	require.Empty(t, m.ShadowSnapshot().Items)
}
