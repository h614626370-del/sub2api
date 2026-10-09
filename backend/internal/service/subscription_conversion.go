package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentauditlog"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/shopspring/decimal"
)

var (
	ErrConversionDisabled    = infraerrors.Forbidden("CONVERSION_DISABLED", "订阅转余额暂未开放")
	ErrConversionUnavailable = infraerrors.Conflict("CONVERSION_UNAVAILABLE", "该订阅当前无法转换，请刷新查看原因")
	ErrConversionChanged     = infraerrors.Conflict("CONVERSION_CHANGED", "订阅或折算规则已变更，请刷新后重新确认")
	ErrSubscriptionConverted = infraerrors.Conflict("SUBSCRIPTION_CONVERTED", "订阅已转换为余额，不能恢复")
	ErrConversionPending     = infraerrors.Conflict("CONVERSION_PENDING", "该订阅仍有请求或用量待结算，请稍后刷新重试")
	ErrConversionUnsettled   = infraerrors.Conflict("CONVERSION_UNSETTLED", "该订阅有未成功结算的请求，请联系管理员核对")
)

type SubscriptionConversionPreview struct {
	SubscriptionID    int64     `json:"subscription_id"`
	GroupID           int64     `json:"group_id"`
	GroupName         string    `json:"group_name"`
	StartsAt          time.Time `json:"starts_at"`
	ExpiresAt         time.Time `json:"expires_at"`
	TotalSeconds      int64     `json:"total_seconds"`
	RemainingSeconds  int64     `json:"remaining_seconds"`
	PriceUSD          string    `json:"price_usd"`
	MonthlyQuotaUSD   string    `json:"monthly_quota_usd"`
	TotalQuotaUSD     string    `json:"total_quota_usd"`
	UsedQuotaUSD      string    `json:"used_quota_usd"`
	RemainingQuotaUSD string    `json:"remaining_quota_usd"`
	TimeRatio         string    `json:"time_ratio"`
	QuotaRatio        string    `json:"quota_ratio"`
	AmountUSD         string    `json:"amount_usd"`
	OrderIDs          []int64   `json:"order_ids"`
	Eligible          bool      `json:"eligible"`
	Reason            string    `json:"reason"`
	Quote             string    `json:"quote"`
}
type SubscriptionConversionReceipt struct {
	SubscriptionID int64                         `json:"subscription_id"`
	GroupID        int64                         `json:"group_id"`
	GroupName      string                        `json:"group_name"`
	AmountUSD      string                        `json:"amount_usd"`
	Calculation    SubscriptionConversionPreview `json:"calculation"`
	ConvertedAt    time.Time                     `json:"converted_at"`
}
type SubscriptionConversionOverview struct {
	Enabled       bool                            `json:"enabled"`
	Subscriptions []SubscriptionConversionPreview `json:"subscriptions"`
	History       []SubscriptionConversionReceipt `json:"history"`
}

// All amounts are decimal; round down only the final credit. The two weights
// are fixed at 50%, and each remaining proportion is clamped to [0,1].
func calculateSubscriptionConversion(price, monthly, used decimal.Decimal, total, remaining int64) (amount, quota, left, timeRatio, quotaRatio decimal.Decimal) {
	if total <= 0 || remaining <= 0 || !price.IsPositive() || !monthly.IsPositive() {
		return
	}
	if remaining > total {
		remaining = total
	}
	monthSeconds := decimal.NewFromInt(30 * 86400)
	scaledQuota := monthly.Mul(decimal.NewFromInt(total))
	scaledLeft := decimal.Max(decimal.Zero, scaledQuota.Sub(decimal.Max(decimal.Zero, used).Mul(monthSeconds)))
	quota = scaledQuota.Div(monthSeconds)
	left = scaledLeft.Div(monthSeconds)
	timeRatio = decimal.NewFromInt(remaining).Div(decimal.NewFromInt(total))
	quotaRatio = scaledLeft.Div(scaledQuota)
	// Keep both fractions over a common denominator. QuoRem truncates directly
	// to six decimals, without rounding an intermediate ratio across a boundary.
	numerator := price.Mul(monthly.Mul(decimal.NewFromInt(remaining)).Add(scaledLeft))
	amount, _ = numerator.QuoRem(scaledQuota.Mul(decimal.NewFromInt(2)), 6)
	return
}

var purchaseNotePattern = regexp.MustCompile(`^payment order ([1-9][0-9]*)$`)

func purchaseOrderIDs(notes string) ([]int64, bool) {
	ids := []int64{}
	seen := map[int64]bool{}
	for _, line := range strings.Split(strings.TrimSpace(notes), "\n") {
		m := purchaseNotePattern.FindStringSubmatch(strings.TrimSpace(line))
		if len(m) != 2 {
			return nil, false
		}
		id, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil || seen[id] || len(ids) >= 1000 {
			return nil, false
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids, len(ids) > 0
}

func (s *SubscriptionService) loadConversionPreview(ctx context.Context, client *dbent.Client, id, userID int64, lock bool) (*SubscriptionConversionPreview, error) {
	p := &SubscriptionConversionPreview{SubscriptionID: id, PriceUSD: "0", AmountUSD: "0.000000", OrderIDs: []int64{}, Reason: "not_purchased"}
	query := `SELECT s.group_id,g.name,s.starts_at,s.expires_at,s.status,COALESCE(s.notes,''),s.assigned_by,
 s.conversion_used_usd::text,(s.conversion_usage_complete AND s.conversion_used_usd>=GREATEST(s.daily_usage_usd,s.weekly_usage_usd,s.monthly_usage_usd)),COALESCE(g.monthly_limit_usd,0)::text,
 COALESCE(g.daily_limit_usd,0)::text,COALESCE(g.weekly_limit_usd,0)::text
 FROM user_subscriptions s JOIN groups g ON g.id=s.group_id
 WHERE s.id=$1 AND s.user_id=$2 AND s.deleted_at IS NULL AND g.deleted_at IS NULL AND g.subscription_type='subscription'`
	if lock {
		query += ` FOR UPDATE OF s,g`
	}
	rows, err := client.QueryContext(ctx, query, id, userID)
	if err != nil {
		return nil, err
	}
	var status, notes, used, monthly, daily, weekly string
	var assigned sql.NullInt64
	var complete bool
	if !rows.Next() {
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return nil, err
		}
		return nil, ErrConversionUnavailable
	}
	err = rows.Scan(&p.GroupID, &p.GroupName, &p.StartsAt, &p.ExpiresAt, &status, &notes, &assigned, &used, &complete, &monthly, &daily, &weekly)
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	now := s.now()
	p.TotalSeconds = int64(p.ExpiresAt.Sub(p.StartsAt) / time.Second)
	p.RemainingSeconds = int64(p.ExpiresAt.Sub(now) / time.Second)
	if p.RemainingSeconds < 0 {
		p.RemainingSeconds = 0
	}
	p.MonthlyQuotaUSD = monthly
	p.UsedQuotaUSD = used
	if status != SubscriptionStatusActive || p.StartsAt.After(now) || p.RemainingSeconds <= 0 {
		p.Reason = "inactive"
		return p, nil
	}
	if assigned.Valid {
		return p, nil
	}
	ids, valid := purchaseOrderIDs(notes)
	if !valid {
		return p, nil
	}
	q := client.PaymentOrder.Query().Where(paymentorder.IDIn(ids...)).Order(dbent.Asc(paymentorder.FieldID))
	if lock {
		q = q.ForUpdate()
	}
	orders, err := q.All(ctx)
	if err != nil {
		return nil, err
	}
	if len(orders) != len(ids) {
		return p, nil
	}
	price := decimal.Zero
	var days int64
	for _, o := range orders {
		if o.UserID != userID || o.OrderType != "subscription" || o.SubscriptionGroupID == nil || *o.SubscriptionGroupID != p.GroupID || o.SubscriptionDays == nil || *o.SubscriptionDays <= 0 {
			return p, nil
		}
		// Notes can retain orders from an expired term; they must not contribute value again.
		if o.CompletedAt != nil && o.CompletedAt.Before(p.StartsAt.Add(-time.Second)) {
			continue
		}
		if o.Status != OrderStatusCompleted || o.CompletedAt == nil || o.RefundAmount != 0 || o.RefundRequestedAt != nil {
			p.Reason = "order_unavailable"
			return p, nil
		}
		audited, err := client.PaymentAuditLog.Query().Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(o.ID, 10)), paymentauditlog.ActionIn("SUBSCRIPTION_ASSIGNED", "SUBSCRIPTION_SUCCESS")).Exist(ctx)
		if err != nil {
			return nil, err
		}
		if !audited {
			return p, nil
		}
		price = price.Add(decimal.NewFromFloat(o.Amount))
		days += int64(*o.SubscriptionDays)
		p.OrderIDs = append(p.OrderIDs, o.ID)
	}
	// Reject mixed grants/manual term adjustments instead of refunding unpaid days.
	if days <= 0 || days > MaxValidityDays || absInt64(days*86400-p.TotalSeconds) > 2 {
		p.Reason = "mixed_term"
		return p, nil
	}
	if !complete {
		p.Reason = "usage_incomplete"
		return p, nil
	}
	month, err := decimal.NewFromString(monthly)
	if err != nil {
		return nil, err
	}
	d, _ := decimal.NewFromString(daily)
	w, _ := decimal.NewFromString(weekly)
	if !month.IsPositive() || d.IsPositive() || w.IsPositive() {
		p.Reason = "unsupported_quota"
		return p, nil
	}
	consumption, err := decimal.NewFromString(used)
	if err != nil {
		return nil, err
	}
	// Row locks may have waited behind an order/refund operation. Use settlement
	// time after acquiring them, never the earlier time when the request arrived.
	p.RemainingSeconds = int64(p.ExpiresAt.Sub(s.now()) / time.Second)
	if p.RemainingSeconds <= 0 {
		p.RemainingSeconds = 0
		p.Reason = "inactive"
		return p, nil
	}
	amount, totalQuota, left, tr, qr := calculateSubscriptionConversion(price, month, consumption, days*86400, p.RemainingSeconds)
	p.PriceUSD = price.StringFixed(2)
	p.TotalQuotaUSD = totalQuota.StringFixed(6)
	p.RemainingQuotaUSD = left.StringFixed(6)
	p.TimeRatio = tr.StringFixed(10)
	p.QuotaRatio = qr.StringFixed(10)
	p.AmountUSD = amount.StringFixed(6)
	if !amount.IsPositive() {
		p.Reason = "too_small"
		return p, nil
	}
	state, err := s.conversionLeaseState(ctx, client, id)
	if err != nil {
		return nil, err
	}
	if state == "active" {
		p.Reason = "pending_requests"
		return p, nil
	}
	if state == "unsettled" {
		p.Reason = "unsettled_usage"
		return p, nil
	}
	raw, _ := json.Marshal(struct {
		IDs            []int64
		Price, Monthly string
		Start, End     time.Time
	}{p.OrderIDs, p.PriceUSD, monthly, p.StartsAt, p.ExpiresAt})
	digest := sha256.Sum256(raw)
	p.Quote = hex.EncodeToString(digest[:])
	p.Eligible = true
	p.Reason = ""
	return p, nil
}
func absInt64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

const conversionReceiptColumns = `subscription_id,group_id,group_name,amount_usd::text,calculation,converted_at`

func scanConversionReceipt(rows *sql.Rows) (*SubscriptionConversionReceipt, error) {
	r := &SubscriptionConversionReceipt{}
	var raw []byte
	if err := rows.Scan(&r.SubscriptionID, &r.GroupID, &r.GroupName, &r.AmountUSD, &raw, &r.ConvertedAt); err != nil {
		return nil, err
	}
	err := json.Unmarshal(raw, &r.Calculation)
	return r, err
}
func (s *SubscriptionService) ConversionOverview(ctx context.Context, userID int64, cfg *SubscriptionConversionSettings) (*SubscriptionConversionOverview, error) {
	out := &SubscriptionConversionOverview{Enabled: cfg.Enabled, Subscriptions: []SubscriptionConversionPreview{}, History: []SubscriptionConversionReceipt{}}
	if !cfg.Enabled {
		return out, nil
	}
	subs, err := s.userSubRepo.ListActiveByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	for _, sub := range subs {
		p, err := s.loadConversionPreview(ctx, s.entClient, sub.ID, userID, false)
		if err != nil {
			return nil, err
		}
		out.Subscriptions = append(out.Subscriptions, *p)
	}
	rows, err := s.entClient.QueryContext(ctx, `SELECT `+conversionReceiptColumns+` FROM subscription_conversions WHERE user_id=$1 ORDER BY converted_at DESC LIMIT 100`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		r, err := scanConversionReceipt(rows)
		if err != nil {
			return nil, err
		}
		out.History = append(out.History, *r)
	}
	return out, rows.Err()
}
func (s *SubscriptionService) ConvertToBalance(ctx context.Context, userID, subscriptionID int64, quote string) (*SubscriptionConversionReceipt, error) {
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	client := tx.Client()
	rows, err := client.QueryContext(ctx, `SELECT value FROM settings WHERE key=$1 FOR SHARE`, SettingKeySubscriptionConversion)
	if err != nil {
		return nil, err
	}
	raw := ""
	if rows.Next() {
		err = rows.Scan(&raw)
	}
	rowErr := rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	if rowErr != nil {
		return nil, rowErr
	}
	cfg, err := parseConversionSettings(raw)
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, ErrConversionDisabled
	}
	rows, err = client.QueryContext(ctx, `SELECT id FROM users WHERE id=$1 AND deleted_at IS NULL AND status='active' FOR UPDATE`, userID)
	if err != nil {
		return nil, err
	}
	exists := rows.Next()
	rowErr = rows.Err()
	_ = rows.Close()
	if rowErr != nil {
		return nil, rowErr
	}
	if !exists {
		return nil, ErrConversionUnavailable
	}
	rows, err = client.QueryContext(ctx, `SELECT `+conversionReceiptColumns+` FROM subscription_conversions WHERE subscription_id=$1 AND user_id=$2`, subscriptionID, userID)
	if err != nil {
		return nil, err
	}
	if rows.Next() {
		receipt, scanErr := scanConversionReceipt(rows)
		_ = rows.Close()
		_ = tx.Rollback()
		if scanErr == nil {
			s.invalidateConversionCaches(userID, receipt.GroupID)
		}
		return receipt, scanErr
	}
	rowErr = rows.Err()
	_ = rows.Close()
	if rowErr != nil {
		return nil, rowErr
	}
	p, err := s.loadConversionPreview(ctx, client, subscriptionID, userID, true)
	if err != nil {
		return nil, err
	}
	if !p.Eligible {
		switch p.Reason {
		case "pending_requests":
			return nil, ErrConversionPending
		case "unsettled_usage":
			return nil, ErrConversionUnsettled
		}
		return nil, ErrConversionUnavailable
	}
	if quote == "" || quote != p.Quote {
		return nil, ErrConversionChanged
	}
	now := s.now()
	receipt := &SubscriptionConversionReceipt{SubscriptionID: p.SubscriptionID, GroupID: p.GroupID, GroupName: p.GroupName, AmountUSD: p.AmountUSD, Calculation: *p, ConvertedAt: now}
	calculation, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	_, err = client.ExecContext(ctx, `INSERT INTO subscription_conversions(subscription_id,user_id,group_id,group_name,amount_usd,calculation,converted_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, p.SubscriptionID, userID, p.GroupID, p.GroupName, p.AmountUSD, string(calculation), now)
	if err != nil {
		return nil, err
	}
	_, err = client.ExecContext(ctx, `UPDATE users SET balance=balance+$1::numeric,updated_at=$2 WHERE id=$3`, p.AmountUSD, now, userID)
	if err != nil {
		return nil, err
	}
	_, err = client.ExecContext(ctx, `UPDATE user_subscriptions SET status=$1,deleted_at=$2,updated_at=$2 WHERE id=$3`, SubscriptionStatusConverted, now, p.SubscriptionID)
	if err != nil {
		return nil, err
	}
	// Locking and changing the original order status prevents a concurrent/refuture refund
	// from returning money for benefits already converted to balance.
	count, err := client.PaymentOrder.Update().Where(paymentorder.IDIn(p.OrderIDs...), paymentorder.StatusEQ(OrderStatusCompleted)).SetStatus(OrderStatusConverted).Save(ctx)
	if err != nil {
		return nil, err
	}
	if count != len(p.OrderIDs) {
		return nil, ErrConversionChanged
	}
	for _, orderID := range p.OrderIDs {
		if _, err := client.PaymentAuditLog.Create().SetOrderID(strconv.FormatInt(orderID, 10)).
			SetAction("SUBSCRIPTION_CONVERTED").SetOperator(fmt.Sprintf("user:%d", userID)).
			SetDetail(string(calculation)).Save(ctx); err != nil {
			return nil, err
		}
	}
	_, err = client.ExecContext(ctx, `INSERT INTO redeem_codes(code,type,value,status,used_by,used_at,notes,created_at,validity_days) VALUES($1,'balance',$2::numeric,'used',$3,$4,$5,$4,0)`, fmt.Sprintf("subconv-%d", p.SubscriptionID), p.AmountUSD, userID, now, fmt.Sprintf("订阅转余额 #%d · %s · 售价 %s × (时间比例 %s × 50%% + 额度比例 %s × 50%%)", p.SubscriptionID, p.GroupName, p.PriceUSD, p.TimeRatio, p.QuotaRatio))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	s.invalidateConversionCaches(userID, p.GroupID)
	return receipt, nil
}
func (s *SubscriptionService) invalidateConversionCaches(userID, groupID int64) {
	if s.conversionAuthCacheInvalidator != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		s.conversionAuthCacheInvalidator.InvalidateAuthCacheByUserID(ctx, userID)
		cancel()
	}
	if err := s.invalidateSubscriptionCaches(userID, groupID); err != nil {
		slog.Error("subscription conversion cache invalidation failed", "user_id", userID, "group_id", groupID, "error", err)
	}
	if s.billingCacheService != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.billingCacheService.InvalidateUserBalance(ctx, userID); err != nil {
			slog.Error("subscription conversion balance cache invalidation failed", "user_id", userID, "error", err)
		}
	}
}
