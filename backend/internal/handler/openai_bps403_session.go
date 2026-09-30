package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
)

func (h *OpenAIGatewayHandler) rejectIfBPS403SessionBlocked(c *gin.Context, identity service.CyberSessionIdentityResolution, format cyberSessionBlockFormat) bool {
	if h.gatewayService.FindBPS403SessionBlockedForIdentity(c.Request.Context(), identity) == "" {
		return false
	}
	message := service.BPS403SessionBlockedMessage
	if service.StopOpenAICompactSSEKeepaliveCommitted(c) {
		service.MarkOpsStreamError(c, "permission_error", message, http.StatusForbidden)
		if writeResponsesFailedSSE(c, "permission_error", service.BPS403SessionBlockedCode, message) {
			return true
		}
	}
	err := gin.H{"type": "permission_error", "code": service.BPS403SessionBlockedCode, "message": message}
	if format == cyberBlockFormatAnthropic {
		c.JSON(http.StatusForbidden, gin.H{"type": "error", "error": err})
	} else {
		c.JSON(http.StatusForbidden, gin.H{"error": err})
	}
	return true
}

func writeBPS403SessionBlockedWSError(ctx context.Context, conn *coderws.Conn) {
	if conn == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	payload, err := json.Marshal(gin.H{"event_id": "evt_bps403_session_blocked", "type": "error", "error": gin.H{"type": "permission_error", "code": service.BPS403SessionBlockedCode, "message": service.BPS403SessionBlockedMessage}})
	if err != nil {
		return
	}
	writeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_ = service.WriteCapturedWSClient(writeCtx, conn, coderws.MessageText, payload)
}
