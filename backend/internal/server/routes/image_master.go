package routes

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/imagemaster"
	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/requestmodel"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type imageMasterKeys struct{}

// Run a dedicated in-process route chain, not an HTTP loopback or the public
// router. Authentication and stream-only admission already ran on the original
// request; target-model policy and routing must run on the rewritten request.
func imageMasterMiddleware(m *imagemaster.Manager, images gin.HandlerFunc, gates ...gin.HandlerFunc) gin.HandlerFunc {
	if m == nil {
		return func(c *gin.Context) { c.Next() }
	}
	internal := gin.New()
	_ = internal.SetTrustedProxies(nil)
	internal.Use(gin.Recovery())
	internal.Use(func(c *gin.Context) {
		keys, ok := c.Request.Context().Value(imageMasterKeys{}).(map[string]any)
		if !ok {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Keys = keys
		c.Next()
	})
	internal.Use(handler.InboundEndpointMiddleware())
	internal.Use(gates...)
	internal.POST("/v1/images/generations", images)
	internal.POST("/v1/images/edits", images)
	return func(c *gin.Context) {
		cfg := m.Config()
		if !cfg.Enabled || c.Request.Method != http.MethodPost {
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
		if !ok || key == nil || key.Group == nil ||
			(key.Group.Platform != service.PlatformOpenAI && key.Group.Platform != service.PlatformComposite) {
			c.Next()
			return
		}
		body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
		if err != nil {
			status := http.StatusBadRequest
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				status = http.StatusRequestEntityTooLarge
			}
			c.AbortWithStatusJSON(status, gin.H{"error": gin.H{"message": "Could not read image request"}})
			return
		}
		requestmodel.ResetRequestBody(c.Request, body)
		if !imagemaster.HasImageTool(body) {
			c.Next()
			return
		}
		if !service.GroupAllowsImageGeneration(key.Group) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": gin.H{"message": service.ImageGenerationPermissionMessage()}})
			return
		}
		keys := c.Copy().Keys
		keys["sub2api.verified_forwarded_ip"] = ip.GetTrustedClientIP(c)
		req := c.Request.Clone(context.WithValue(c.Request.Context(), imageMasterKeys{}, keys))
		req.Header.Set("X-Request-ID", c.Writer.Header().Get("X-Request-ID"))
		m.Serve(c.Writer, req, body, imagemaster.Identity{UserID: key.UserID, APIKeyID: key.ID}, internal.ServeHTTP)
		// Dispatch has stopped before Serve returns. Restore its routing and
		// account/error attribution for the outer request's existing ops logger.
		for name, value := range keys {
			c.Set(name, value)
		}
		c.Abort()
	}
}

func registerImageMasterRoutes(admin *gin.RouterGroup, m *imagemaster.Manager, stepUp middleware.StepUpAuthMiddleware) {
	group := admin.Group("/image-master")
	group.Use(func(c *gin.Context) {
		if m == nil {
			response.Error(c, 503, "Image master storage is unavailable")
			c.Abort()
			return
		}
		c.Next()
	})
	group.GET("", func(c *gin.Context) { response.Success(c, m.Snapshot()) })
	group.PUT("/settings", func(c *gin.Context) {
		var cfg imagemaster.Config
		decoder := json.NewDecoder(io.LimitReader(c.Request.Body, 8193))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&cfg) != nil || decoder.Decode(new(any)) != io.EOF || cfg.Validate() != nil {
			response.BadRequest(c, "Invalid image master settings")
			return
		}
		if err := m.SaveConfig(cfg); err != nil {
			response.Error(c, 503, "Could not save image master settings")
			return
		}
		response.Success(c, cfg)
	})
	group.POST("/requests/:id/cancel", func(c *gin.Context) {
		if !m.Cancel(c.Param("id")) {
			response.Error(c, 409, "Request has already finished or does not exist")
			return
		}
		response.Success(c, gin.H{"canceled": true})
	})
	group.DELETE("/requests", func(c *gin.Context) {
		if err := m.Delete(""); err != nil {
			response.Error(c, 503, "Could not clear finished requests")
			return
		}
		response.Success(c, gin.H{"deleted": true})
	})
	group.DELETE("/requests/:id", func(c *gin.Context) {
		if err := m.Delete(c.Param("id")); err != nil {
			response.Error(c, 409, "Cancel the request before deleting, or check storage")
			return
		}
		response.Success(c, gin.H{"deleted": true})
	})
	rawHandlers := []gin.HandlerFunc{}
	if stepUp != nil {
		rawHandlers = append(rawHandlers, gin.HandlerFunc(stepUp))
	}
	rawHandlers = append(rawHandlers, func(c *gin.Context) {
		data, err := m.Raw(c.Param("id"))
		if err != nil {
			response.NotFound(c, "Raw request not found")
			return
		}
		c.Header("Cache-Control", "no-store")
		response.Success(c, data)
	})
	group.GET("/requests/:id/raw", rawHandlers...)
	group.GET("/export", func(c *gin.Context) {
		c.Header("Content-Disposition", `attachment; filename="image-master-requests.json"`)
		c.Header("Cache-Control", "no-store")
		c.JSON(200, m.Snapshot().Items)
	})
}
