package service

import (
	"context"
	"time"
)

type OpenAICodexTicketAudit struct {
	ID              int64             `json:"id"`
	AccountID       int64             `json:"account_id"`
	Model           string            `json:"model"`
	StartedAt       time.Time         `json:"started_at"`
	FinishedAt      time.Time         `json:"finished_at"`
	DurationMS      int               `json:"duration_ms"`
	Outcome         string            `json:"outcome"`
	Reason          string            `json:"reason"`
	HTTPStatus      *int              `json:"http_status,omitempty"`
	TicketLength    int               `json:"ticket_length"`
	Attempts        int               `json:"attempts"`
	TicketExpiresAt *time.Time        `json:"ticket_expires_at,omitempty"`
	EgressIP        string            `json:"egress_ip,omitempty"`
	RequestBody     string            `json:"request_body,omitempty"`
	ResponseBody    string            `json:"-"`
	ResponseHeaders map[string]string `json:"response_headers,omitempty"`
	TicketHash      string            `json:"ticket_hash,omitempty"`
	CreatedAt       time.Time         `json:"created_at"`
}

type OpenAICodexTicketAuditFilter struct {
	AccountID *int64
	Model     string
	Outcome   string
	From      *time.Time
	To        *time.Time
	Page      int
	PageSize  int
}

type OpenAICodexTicketAuditPage struct {
	Items    []*OpenAICodexTicketAudit `json:"items"`
	Total    int                       `json:"total"`
	Page     int                       `json:"page"`
	PageSize int                       `json:"page_size"`
}

type OpenAICodexTicketAuditRepository interface {
	Insert(context.Context, *OpenAICodexTicketAudit) error
	List(context.Context, *OpenAICodexTicketAuditFilter) (*OpenAICodexTicketAuditPage, error)
	GetByID(context.Context, int64) (*OpenAICodexTicketAudit, error)
	Clear(context.Context) (int64, error)
}
