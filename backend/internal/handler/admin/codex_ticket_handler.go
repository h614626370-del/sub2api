package admin

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type CodexTicketHandler struct {
	repo    service.OpenAICodexTicketAuditRepository
	service *service.OpenAIGatewayService
}

func NewCodexTicketHandler(repo service.OpenAICodexTicketAuditRepository, gatewayService *service.OpenAIGatewayService) *CodexTicketHandler {
	return &CodexTicketHandler{repo: repo, service: gatewayService}
}

type discardCodexTicketRequest struct {
	AccountID *int64 `json:"account_id"`
}

func (h *CodexTicketHandler) DiscardTickets(c *gin.Context) {
	if h == nil || h.service == nil {
		response.ErrorWithDetails(c, http.StatusServiceUnavailable, "Codex ticket service is unavailable", "CODEX_TICKET_UNAVAILABLE", nil)
		return
	}
	var request discardCodexTicketRequest
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&request); err != nil {
			response.BadRequest(c, "Invalid request body")
			return
		}
	}
	if request.AccountID != nil && *request.AccountID <= 0 {
		response.BadRequest(c, "Invalid account_id")
		return
	}
	result, err := h.service.DiscardOpenAICodexTickets(c.Request.Context(), request.AccountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *CodexTicketHandler) ListAudits(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	if pageSize > 200 {
		pageSize = 200
	}
	filter := &service.OpenAICodexTicketAuditFilter{Page: page, PageSize: pageSize, Model: strings.TrimSpace(c.Query("model")), Outcome: strings.TrimSpace(c.Query("outcome"))}
	if raw := strings.TrimSpace(c.Query("account_id")); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			response.BadRequest(c, "Invalid account_id")
			return
		}
		filter.AccountID = &id
	}
	if raw := strings.TrimSpace(c.Query("from")); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			response.BadRequest(c, "Invalid from")
			return
		}
		filter.From = &t
	}
	if raw := strings.TrimSpace(c.Query("to")); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			response.BadRequest(c, "Invalid to")
			return
		}
		filter.To = &t
	}
	if h == nil || h.repo == nil {
		response.ErrorWithDetails(c, http.StatusServiceUnavailable, "Codex ticket audit is unavailable", "CODEX_TICKET_AUDIT_UNAVAILABLE", nil)
		return
	}
	result, err := h.repo.List(c.Request.Context(), filter)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, result.Items, int64(result.Total), result.Page, result.PageSize)
}

func (h *CodexTicketHandler) GetAudit(c *gin.Context) {
	id, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid audit id")
		return
	}
	if h == nil || h.repo == nil {
		response.ErrorWithDetails(c, http.StatusServiceUnavailable, "Codex ticket audit is unavailable", "CODEX_TICKET_AUDIT_UNAVAILABLE", nil)
		return
	}
	item, err := h.repo.GetByID(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *CodexTicketHandler) ClearAudits(c *gin.Context) {
	if h == nil || h.repo == nil {
		response.ErrorWithDetails(c, http.StatusServiceUnavailable, "Codex ticket audit is unavailable", "CODEX_TICKET_AUDIT_UNAVAILABLE", nil)
		return
	}
	cleared, err := h.repo.Clear(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"cleared": cleared})
}
