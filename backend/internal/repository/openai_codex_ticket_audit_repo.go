package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type openAICodexTicketAuditRepository struct{ db *sql.DB }

func (r *openAICodexTicketAuditRepository) Statistics(ctx context.Context) ([]service.OpenAICodexTicketStatistics, error) {
	rows, err := r.db.QueryContext(ctx, `WITH last_success AS (
	 SELECT DISTINCT ON (account_id, model) account_id, model, started_at, finished_at, duration_ms, egress_ip, response_headers
	 FROM openai_codex_ticket_audits WHERE outcome IN ('success','validated')
	 ORDER BY account_id, model, started_at DESC, id DESC
	)
	SELECT a.account_id, a.model, count(*), count(*) FILTER (WHERE a.outcome IN ('success','validated')), coalesce(sum(a.duration_ms),0),
	 count(*) FILTER (WHERE s.started_at IS NULL OR a.started_at > s.started_at),
	 min(a.started_at) FILTER (WHERE s.started_at IS NULL OR a.started_at > s.started_at),
	 s.finished_at, coalesce(s.duration_ms,0), coalesce(s.egress_ip,''),
	 coalesce(s.response_headers->>'x-sub2api-egress-ip-source','unknown')
	FROM openai_codex_ticket_audits a LEFT JOIN last_success s USING(account_id,model)
	GROUP BY a.account_id,a.model,s.started_at,s.finished_at,s.duration_ms,s.egress_ip,s.response_headers
	ORDER BY a.account_id,a.model`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]service.OpenAICodexTicketStatistics, 0)
	for rows.Next() {
		var item service.OpenAICodexTicketStatistics
		if err := rows.Scan(&item.AccountID, &item.Model, &item.TotalAttempts, &item.Successes, &item.TotalDurationMS, &item.PendingAttempts, &item.PendingSince, &item.LastSuccessAt, &item.LastSuccessDurationMS, &item.LastSuccessIP, &item.LastSuccessIPSource); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func NewOpenAICodexTicketAuditRepository(db *sql.DB) service.OpenAICodexTicketAuditRepository {
	return &openAICodexTicketAuditRepository{db: db}
}

func (r *openAICodexTicketAuditRepository) Insert(ctx context.Context, item *service.OpenAICodexTicketAudit) error {
	if r == nil || r.db == nil || item == nil {
		return fmt.Errorf("nil codex ticket audit repository or item")
	}
	headers, err := json.Marshal(item.ResponseHeaders)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO openai_codex_ticket_audits
  (account_id, model, started_at, finished_at, duration_ms, outcome, reason, http_status,
  ticket_length, attempts, ticket_expires_at, egress_ip, request_body, response_body, response_headers, ticket_hash)
  VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
		item.AccountID, item.Model, item.StartedAt, item.FinishedAt, item.DurationMS,
		item.Outcome, item.Reason, item.HTTPStatus, item.TicketLength, item.Attempts, item.TicketExpiresAt,
		strings.TrimSpace(item.EgressIP), item.RequestBody, "", headers, item.TicketHash)
	return err
}

func (r *openAICodexTicketAuditRepository) List(ctx context.Context, filter *service.OpenAICodexTicketAuditFilter) (*service.OpenAICodexTicketAuditPage, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("nil codex ticket audit repository")
	}
	if filter == nil {
		filter = &service.OpenAICodexTicketAuditFilter{}
	}
	page, pageSize := filter.Page, filter.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 50
	}
	if pageSize > 200 {
		pageSize = 200
	}
	where := []string{"1=1"}
	args := []any{}
	add := func(sql string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(sql, len(args)))
	}
	if filter.AccountID != nil {
		add("account_id = $%d", *filter.AccountID)
	}
	if v := strings.TrimSpace(filter.Model); v != "" {
		add("model = $%d", v)
	}
	if v := strings.TrimSpace(filter.Outcome); v != "" {
		add("outcome = $%d", v)
	}
	if filter.From != nil {
		add("created_at >= $%d", filter.From.UTC())
	}
	if filter.To != nil {
		add("created_at <= $%d", filter.To.UTC())
	}
	whereSQL := strings.Join(where, " AND ")
	var total int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM openai_codex_ticket_audits WHERE "+whereSQL, args...).Scan(&total); err != nil {
		return nil, err
	}
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := r.db.QueryContext(ctx, `SELECT id, account_id, model, started_at, finished_at, duration_ms,
  outcome, reason, http_status, ticket_length, attempts, ticket_expires_at, egress_ip, request_body,
  response_body, response_headers, ticket_hash, created_at
  FROM openai_codex_ticket_audits WHERE `+whereSQL+` ORDER BY created_at DESC, id DESC LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]*service.OpenAICodexTicketAudit, 0, pageSize)
	for rows.Next() {
		item, err := scanCodexTicketAudit(rows.Scan)
		if err != nil {
			return nil, err
		}
		item.RequestBody = ""
		item.ResponseBody = ""
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &service.OpenAICodexTicketAuditPage{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

func (r *openAICodexTicketAuditRepository) GetByID(ctx context.Context, id int64) (*service.OpenAICodexTicketAudit, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("nil codex ticket audit repository")
	}
	row := r.db.QueryRowContext(ctx, `SELECT id, account_id, model, started_at, finished_at, duration_ms,
	  outcome, reason, http_status, ticket_length, attempts, ticket_expires_at, egress_ip, request_body,
  response_body, response_headers, ticket_hash, created_at FROM openai_codex_ticket_audits WHERE id=$1`, id)
	item, err := scanCodexTicketAudit(row.Scan)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("codex ticket audit not found")
	}
	if item != nil {
		item.ResponseBody = ""
	}
	return item, err
}

func (r *openAICodexTicketAuditRepository) Clear(ctx context.Context) (int64, error) {
	if r == nil || r.db == nil {
		return 0, fmt.Errorf("nil codex ticket audit repository")
	}
	result, err := r.db.ExecContext(ctx, `DELETE FROM openai_codex_ticket_audits`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func scanCodexTicketAudit(scan func(...any) error) (*service.OpenAICodexTicketAudit, error) {
	item := &service.OpenAICodexTicketAudit{}
	var headers []byte
	var status sql.NullInt64
	var expires sql.NullTime
	err := scan(&item.ID, &item.AccountID, &item.Model, &item.StartedAt, &item.FinishedAt, &item.DurationMS, &item.Outcome, &item.Reason, &status, &item.TicketLength, &item.Attempts, &expires, &item.EgressIP, &item.RequestBody, &item.ResponseBody, &headers, &item.TicketHash, &item.CreatedAt)
	if err != nil {
		return nil, err
	}
	if status.Valid {
		v := int(status.Int64)
		item.HTTPStatus = &v
	}
	if expires.Valid {
		v := expires.Time
		item.TicketExpiresAt = &v
	}
	if len(headers) > 0 {
		_ = json.Unmarshal(headers, &item.ResponseHeaders)
	}
	return item, nil
}
