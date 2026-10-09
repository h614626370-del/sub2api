package handler

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *SubscriptionHandler) ConversionSettings(settings *service.SettingService) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg, err := settings.GetSubscriptionConversionSettings(c.Request.Context())
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		response.Success(c, cfg)
	}
}

func (h *SubscriptionHandler) SaveConversionSettings(settings *service.SettingService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var cfg service.SubscriptionConversionSettings
		if err := c.ShouldBindJSON(&cfg); err != nil {
			response.BadRequest(c, "Invalid conversion settings")
			return
		}
		ctx := c.Request.Context()
		if err := settings.SetSubscriptionConversionSettings(ctx, &cfg); err != nil {
			response.ErrorFrom(c, err)
			return
		}
		response.Success(c, cfg)
	}
}

func (h *SubscriptionHandler) ConversionOverview(settings *service.SettingService) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		subject, ok := middleware.GetAuthSubjectFromContext(c)
		if !ok {
			response.Unauthorized(c, "User not found in context")
			return
		}
		cfg, err := settings.GetSubscriptionConversionSettings(c.Request.Context())
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		overview, err := h.subscriptionService.ConversionOverview(c.Request.Context(), subject.UserID, cfg)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		response.Success(c, overview)
	}
}

func (h *SubscriptionHandler) ConvertToBalance(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not found in context")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid subscription ID")
		return
	}
	var input struct {
		Quote string `json:"quote" binding:"required,len=64"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, "Invalid conversion confirmation")
		return
	}
	receipt, err := h.subscriptionService.ConvertToBalance(c.Request.Context(), subject.UserID, id, input.Quote)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, receipt)
}
