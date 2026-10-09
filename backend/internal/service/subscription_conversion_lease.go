package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/google/uuid"
)

// SubscriptionConversionLease owns one admitted request. The request keeps its
// first reference until its handler returns; queued usage tasks hold additional
// references until billing finishes. An uncertain bill leaves a durable row so
// conversion cannot credit quota that may have been consumed.
type SubscriptionConversionLease struct {
	client         *dbent.Client
	id             string
	subscriptionID int64
	userID         int64
	mu             sync.Mutex
	refs           int
	unsettled      bool
}

type subscriptionConversionLeaseContextKey struct{}

func WithSubscriptionConversionLease(ctx context.Context, lease *SubscriptionConversionLease) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if lease == nil {
		return ctx
	}
	return context.WithValue(ctx, subscriptionConversionLeaseContextKey{}, lease)
}

func SubscriptionConversionLeaseFromContext(ctx context.Context) *SubscriptionConversionLease {
	if ctx == nil {
		return nil
	}
	lease, _ := ctx.Value(subscriptionConversionLeaseContextKey{}).(*SubscriptionConversionLease)
	return lease
}

// AcquireSubscriptionConversionLease serializes admission with conversion on
// the same subscription row. A cached pre-conversion subscription cannot pass
// this fresh database check after the conversion commits.
func (s *SubscriptionService) AcquireSubscriptionConversionLease(ctx context.Context, subscriptionID, userID int64) (*SubscriptionConversionLease, error) {
	if s == nil || s.entClient == nil || subscriptionID <= 0 || userID <= 0 {
		return nil, ErrConversionUnavailable
	}
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	client := tx.Client()
	rows, err := client.QueryContext(ctx, `SELECT id FROM user_subscriptions WHERE id=$1 AND user_id=$2 AND status=$3 AND deleted_at IS NULL AND starts_at<=NOW() AND expires_at>NOW() FOR UPDATE`, subscriptionID, userID, SubscriptionStatusActive)
	if err != nil {
		return nil, err
	}
	exists := rows.Next()
	rowErr := rows.Err()
	_ = rows.Close()
	if rowErr != nil {
		return nil, rowErr
	}
	if !exists {
		return nil, ErrConversionUnavailable
	}
	id := uuid.NewString()
	if _, err := client.ExecContext(ctx, `INSERT INTO subscription_conversion_leases(id,subscription_id,user_id) VALUES($1,$2,$3)`, id, subscriptionID, userID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &SubscriptionConversionLease{client: s.entClient, id: id, subscriptionID: subscriptionID, userID: userID, refs: 1}, nil
}

func deferredSubscriptionTaskKey(apiKeyID int64, taskID string) (string, error) {
	taskID = strings.TrimSpace(taskID)
	if apiKeyID <= 0 || taskID == "" {
		return "", errors.New("deferred subscription task identity is missing")
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%s", apiKeyID, taskID)))
	return hex.EncodeToString(sum[:]), nil
}

// HoldDeferredBilling persists a second lease for work that is billed by a
// later HTTP request. The creating request's lease remains intact and can be
// released normally. No TTL is used: an abandoned or ambiguous task needs
// operator reconciliation before its subscription can be converted.
func (l *SubscriptionConversionLease) HoldDeferredBilling(apiKeyID int64, taskID string) error {
	if l == nil {
		return nil
	}
	key, err := deferredSubscriptionTaskKey(apiKeyID, taskID)
	if err != nil {
		l.MarkUnsettled()
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.refs == 0 {
		l.unsettled = true
		return errors.New("subscription request lease was already released")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = l.client.ExecContext(ctx, `INSERT INTO subscription_conversion_leases(id,subscription_id,user_id,deferred_task_key)
		VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, uuid.NewString(), l.subscriptionID, l.userID, key)
	if err != nil {
		l.unsettled = true
	}
	return err
}

// CompleteDeferredBilling is called only after a successful charge. The
// current poll request still owns its regular lease while the old hold clears.
func (l *SubscriptionConversionLease) CompleteDeferredBilling(apiKeyID int64, taskID string) error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	requestUnsettled := l.unsettled
	l.mu.Unlock()
	if requestUnsettled {
		return errors.New("deferred subscription usage was not fully settled")
	}
	key, err := deferredSubscriptionTaskKey(apiKeyID, taskID)
	if err != nil {
		l.MarkUnsettled()
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = l.client.ExecContext(ctx, `DELETE FROM subscription_conversion_leases
		WHERE subscription_id=$1 AND user_id=$2 AND deferred_task_key=$3`, l.subscriptionID, l.userID, key)
	if err != nil {
		l.MarkUnsettled()
	}
	return err
}

// Retain extends the lease over an asynchronous billing task. Call it before
// enqueueing the task so the request middleware cannot release the final ref.
func (l *SubscriptionConversionLease) Retain() bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.refs == 0 {
		return false
	}
	l.refs++
	return true
}

func (l *SubscriptionConversionLease) MarkUnsettled() {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.unsettled = true
	l.mu.Unlock()
}

// Release removes the durable row only after every retained task has finished.
// A failed write is deliberately fail-closed: the remaining row blocks a
// conversion until an operator has reconciled it.
func (l *SubscriptionConversionLease) Release() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.refs == 0 {
		return
	}
	l.refs--
	if l.refs != 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var err error
	if l.unsettled {
		_, err = l.client.ExecContext(ctx, `UPDATE subscription_conversion_leases SET state='unsettled' WHERE id=$1`, l.id)
	} else {
		_, err = l.client.ExecContext(ctx, `DELETE FROM subscription_conversion_leases WHERE id=$1`, l.id)
	}
	if err != nil {
		slog.Error("subscription conversion lease release failed", "subscription_id", l.subscriptionID, "lease_id", l.id, "error", err)
	}
}

func (s *SubscriptionService) conversionLeaseState(ctx context.Context, client *dbent.Client, subscriptionID int64) (string, error) {
	rows, err := client.QueryContext(ctx, `SELECT state FROM subscription_conversion_leases WHERE subscription_id=$1 ORDER BY CASE state WHEN 'unsettled' THEN 0 ELSE 1 END LIMIT 1`, subscriptionID)
	if err != nil {
		return "", err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return "", rows.Err()
	}
	var state string
	if err := rows.Scan(&state); err != nil {
		return "", err
	}
	return state, rows.Err()
}
