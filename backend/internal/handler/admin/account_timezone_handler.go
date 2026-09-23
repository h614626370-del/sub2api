package admin

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *AccountHandler) accountTimezoneManager(c *gin.Context) (service.AccountTimezoneManager, int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return nil, 0, false
	}
	manager, ok := h.adminService.(service.AccountTimezoneManager)
	if !ok {
		response.ErrorWithDetails(c, 503, "Account timezone service unavailable", "ACCOUNT_TIMEZONE_UNAVAILABLE", nil)
	}
	return manager, id, ok
}

func (h *AccountHandler) GetTimezone(c *gin.Context) {
	manager, id, ok := h.accountTimezoneManager(c)
	if !ok {
		return
	}
	state, err := manager.GetAccountTimezone(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, state)
}

func (h *AccountHandler) DetectTimezone(c *gin.Context) {
	manager, id, ok := h.accountTimezoneManager(c)
	if !ok {
		return
	}
	var req struct {
		Refresh bool `json:"refresh"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request body")
		return
	}
	state, err := manager.DetectAccountTimezone(c.Request.Context(), id, req.Refresh)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, state)
}

func (h *AccountHandler) SetTimezone(c *gin.Context) {
	response.ErrorWithDetails(c, 400, "Account timezone overrides are no longer supported; configure the global OAuth timezone instead", "ACCOUNT_TIMEZONE_OVERRIDE_UNSUPPORTED", nil)
}
