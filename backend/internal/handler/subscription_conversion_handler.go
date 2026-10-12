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
		if _, ok := conversionAdmin(c); !ok {
			return
		}
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
		if _, ok := conversionAdmin(c); !ok {
			return
		}
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

func conversionAdmin(c *gin.Context) (int64, bool) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "User not found in context")
		return 0, false
	}
	role, ok := middleware.GetUserRoleFromContext(c)
	if !ok || role != service.RoleAdmin {
		response.Forbidden(c, "Admin access required")
		return 0, false
	}
	return subject.UserID, true
}

func conversionUserID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("user_id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid user ID")
		return 0, false
	}
	return id, true
}

func (h *SubscriptionHandler) AdminConversionOverview(settings *service.SettingService) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if _, ok := conversionAdmin(c); !ok {
			return
		}
		userID, ok := conversionUserID(c)
		if !ok {
			return
		}
		cfg, err := settings.GetSubscriptionConversionSettings(c.Request.Context())
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		overview, err := h.subscriptionService.ConversionOverview(c.Request.Context(), userID, cfg)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		response.Success(c, overview)
	}
}

func (h *SubscriptionHandler) AdminConvertToBalance(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	adminID, ok := conversionAdmin(c)
	if !ok {
		return
	}
	userID, ok := conversionUserID(c)
	if !ok {
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
	receipt, err := h.subscriptionService.ConvertToBalance(c.Request.Context(), userID, id, input.Quote, adminID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, receipt)
}
