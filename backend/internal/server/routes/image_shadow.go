package routes

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/imagemaster"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

type shadowKeyLookup interface {
	GetByID(context.Context, int64) (*service.APIKey, error)
}
type shadowTarget struct{ group, key, sourceUser, sourceGroup int64 }
type shadowTargetKey struct{}

// A separate engine reauthenticates the test key and runs normal image gates.
// It cannot recurse into the mirror and inherits no client credentials or Gin keys.
func imageShadowMiddleware(m *imagemaster.Manager, keys shadowKeyLookup, auth middleware.APIKeyAuthMiddleware, images gin.HandlerFunc, gates ...gin.HandlerFunc) gin.HandlerFunc {
	if m == nil || keys == nil {
		return func(c *gin.Context) { c.Next() }
	}
	internal := gin.New()
	m.SetShadowTargetValidator(func(ctx context.Context, cfg imagemaster.ShadowConfig) error {
		key, err := keys.GetByID(ctx, cfg.APIKeyID)
		if err != nil || key == nil || key.GroupID == nil || *key.GroupID != cfg.GroupID || key.Status != service.StatusAPIKeyActive || key.Group == nil || key.Group.Platform != service.PlatformOpenAI || !service.GroupAllowsImageGeneration(key.Group) || key.Group.StreamOnly {
			return errors.New("invalid test key or group")
		}
		return nil
	})
	_ = internal.SetTrustedProxies(nil)
	internal.Use(gin.Recovery(), handler.InboundEndpointMiddleware(), gin.HandlerFunc(auth))
	internal.Use(func(c *gin.Context) {
		target, ok := c.Request.Context().Value(shadowTargetKey{}).(shadowTarget)
		key, found := middleware.GetAPIKeyFromContext(c)
		if !ok || !found || key == nil || key.GroupID == nil || *key.GroupID != target.group || key.ID != target.key || key.UserID == target.sourceUser || key.Group == nil || key.Group.Platform != service.PlatformOpenAI {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		c.Next()
	})
	internal.Use(gates...)
	internal.POST("/v1/images/generations", images)
	internal.POST("/v1/images/edits", images)
	return func(c *gin.Context) {
		cfg := m.Config()
		if !cfg.Shadow.Enabled || c.Request.Method != http.MethodPost {
			c.Next()
			return
		}
		switch c.Request.URL.Path {
		case "/v1/responses", "/responses", "/backend-api/codex/responses":
		default:
			c.Next()
			return
		}
		key, ok := middleware.GetAPIKeyFromContext(c)
		if !ok || key == nil || key.GroupID == nil || key.Group == nil || (key.Group.Platform != service.PlatformOpenAI && key.Group.Platform != service.PlatformComposite) {
			c.Next()
			return
		}
		// In comparison mode the ordinary Responses handler always owns delivery.
		c.Set("image_master_shadow_passthrough", true)
		originalBody := c.Request.Body
		if originalBody == nil {
			c.Next()
			return
		}
		raw, err := io.ReadAll(io.LimitReader(originalBody, imagemaster.ShadowBodyLimit+1))
		// Replay both the consumed prefix and the remaining reader. This preserves
		// body-limit/read errors for the original route instead of masking them.
		var tail io.Reader = originalBody
		if err != nil {
			tail = &shadowReadError{err: err}
		}
		c.Request.Body = &shadowReadCloser{Reader: io.MultiReader(bytes.NewReader(raw), tail), closer: originalBody}
		if len(raw) > imagemaster.ShadowBodyLimit {
			m.ShadowBodySkipped()
		}
		if err != nil || len(raw) > imagemaster.ShadowBodyLimit {
			c.Next()
			return
		}
		if !gjson.ValidBytes(raw) {
			c.Next()
			return
		}
		hasTool := false
		gjson.GetBytes(raw, "tools").ForEach(func(_, v gjson.Result) bool {
			if v.Get("type").String() == "image_generation" {
				hasTool = true
			}
			return !hasTool
		})
		if !hasTool {
			c.Next()
			return
		}
		target := shadowTarget{cfg.Shadow.GroupID, cfg.Shadow.APIKeyID, key.UserID, *key.GroupID}
		dispatch := func(w http.ResponseWriter, req *http.Request) {
			testKey, err := keys.GetByID(req.Context(), target.key)
			if err != nil || testKey == nil || testKey.GroupID == nil || *testKey.GroupID != target.group || testKey.UserID == target.sourceUser || target.group == target.sourceGroup {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			req = req.Clone(context.WithValue(req.Context(), shadowTargetKey{}, target))
			req.Header.Set("Authorization", "Bearer "+testKey.Key)
			req.RemoteAddr = "127.0.0.1:0"
			internal.ServeHTTP(w, req)
		}
		job := m.BeginShadowWithConfig(raw, imagemaster.Identity{UserID: key.UserID, APIKeyID: key.ID}, *key.GroupID, c.Writer.Header().Get("X-Request-ID"), cfg, dispatch)
		if job == nil {
			c.Next()
			return
		}
		writer := &shadowObserver{ResponseWriter: c.Writer}
		c.Writer = writer
		defer func() {
			job.CompleteOriginal(writer.body.Bytes(), writer.Header().Get("Content-Type"), writer.Status(), writer.overflow, c.Request.Context().Err() != nil || writer.writeError)
			c.Writer = writer.ResponseWriter
		}()
		c.Next()
	}
}

type shadowReadCloser struct {
	io.Reader
	closer io.Closer
}

func (r *shadowReadCloser) Close() error { return r.closer.Close() }

type shadowReadError struct{ err error }

func (r *shadowReadError) Read([]byte) (int, error) { return 0, r.err }

// Embed the actual Gin writer to preserve flushing, hijacking, status and
// headers. A full diagnostic buffer only stops observation, never delivery.
type shadowObserver struct {
	gin.ResponseWriter
	body                 bytes.Buffer
	overflow, writeError bool
}

func (w *shadowObserver) observe(p []byte) {
	if w.overflow {
		return
	}
	if len(p) > imagemaster.ShadowObservationLimit-w.body.Len() {
		w.overflow = true
		w.body.Reset()
		return
	}
	_, _ = w.body.Write(p)
}
func (w *shadowObserver) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	w.observe(p[:n])
	if err != nil {
		w.writeError = true
	}
	return n, err
}
func (w *shadowObserver) WriteString(p string) (int, error) { return w.Write([]byte(p)) }
func (w *shadowObserver) Unwrap() http.ResponseWriter       { return w.ResponseWriter }
