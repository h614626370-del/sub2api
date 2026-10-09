//go:build integration

package repository

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionConversionSettlement(t *testing.T) {
	ctx := context.Background()
	client := integrationEntClient
	svc := service.NewSubscriptionService(NewGroupRepository(client, integrationDB), NewUserSubscriptionRepository(client), nil, client, &config.Config{})
	defer svc.Stop()
	settings := service.NewSettingService(NewSettingRepository(client), &config.Config{})
	require.NoError(t, settings.SetSubscriptionConversionSettings(ctx, &service.SubscriptionConversionSettings{Enabled: true}))
	t.Cleanup(func() {
		require.NoError(t, settings.SetSubscriptionConversionSettings(ctx, &service.SubscriptionConversionSettings{}))
	})
	type fixture struct {
		user  *dbent.User
		group *dbent.Group
		sub   *dbent.UserSubscription
		order *dbent.PaymentOrder
	}
	create := func(t *testing.T) fixture {
		t.Helper()
		now := time.Now().UTC().Truncate(time.Microsecond)
		start := now.Add(-15 * 24 * time.Hour)
		u, err := client.User.Create().SetEmail(fmt.Sprintf("conversion-%d@test.invalid", time.Now().UnixNano())).SetPasswordHash("test").SetBalance(10).Save(ctx)
		require.NoError(t, err)
		g, err := client.Group.Create().SetName(fmt.Sprintf("Purchased monthly %d", u.ID)).SetSubscriptionType("subscription").SetMonthlyLimitUsd(100).Save(ctx)
		require.NoError(t, err)
		o, err := client.PaymentOrder.Create().SetUserID(u.ID).SetUserEmail(u.Email).SetUserName("Test").SetAmount(30).SetPayAmount(210).SetRechargeCode(fmt.Sprintf("conv-%d", u.ID)).SetPaymentType("test").SetPaymentTradeNo("").SetOrderType("subscription").SetSubscriptionGroupID(g.ID).SetSubscriptionDays(30).SetStatus("COMPLETED").SetExpiresAt(start.Add(time.Hour)).SetCompletedAt(start.Add(time.Second)).SetClientIP("127.0.0.1").SetSrcHost("test").Save(ctx)
		require.NoError(t, err)
		_, err = client.PaymentAuditLog.Create().SetOrderID(fmt.Sprint(o.ID)).SetAction("SUBSCRIPTION_ASSIGNED").SetDetail("{}").SetOperator("system").Save(ctx)
		require.NoError(t, err)
		sub, err := client.UserSubscription.Create().SetUserID(u.ID).SetGroupID(g.ID).SetStartsAt(start).SetExpiresAt(start.Add(30 * 24 * time.Hour)).SetNotes(fmt.Sprintf("payment order %d", o.ID)).Save(ctx)
		require.NoError(t, err)
		_, err = client.ExecContext(ctx, `UPDATE user_subscriptions SET daily_usage_usd=20,monthly_usage_usd=20 WHERE id=$1`, sub.ID)
		require.NoError(t, err)
		return fixture{u, g, sub, o}
	}
	overview := func(t *testing.T, f fixture) service.SubscriptionConversionPreview {
		t.Helper()
		o, err := svc.ConversionOverview(ctx, f.user.ID, &service.SubscriptionConversionSettings{Enabled: true})
		require.NoError(t, err)
		require.Len(t, o.Subscriptions, 1)
		return o.Subscriptions[0]
	}
	t.Run("concurrent idempotent credits and receipt", func(t *testing.T) {
		f := create(t)
		p := overview(t, f)
		require.True(t, p.Eligible, p.Reason)
		require.Equal(t, "30.00", p.PriceUSD)
		require.Equal(t, "100.000000", p.TotalQuotaUSD)
		var wg sync.WaitGroup
		errs := make(chan error, 8)
		receipts := make(chan *service.SubscriptionConversionReceipt, 8)
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				r, e := svc.ConvertToBalance(ctx, f.user.ID, f.sub.ID, p.Quote)
				errs <- e
				receipts <- r
			}()
		}
		wg.Wait()
		close(errs)
		close(receipts)
		for err := range errs {
			require.NoError(t, err)
		}
		for r := range receipts {
			require.NotNil(t, r)
			require.Equal(t, f.sub.ID, r.SubscriptionID)
		}
		u, err := client.User.Get(ctx, f.user.ID)
		require.NoError(t, err)
		require.InDelta(t, 29.5, u.Balance, 0.001)
		o, err := client.PaymentOrder.Get(ctx, f.order.ID)
		require.NoError(t, err)
		require.Equal(t, service.OrderStatusConverted, o.Status)
		_, err = svc.RestoreSubscription(ctx, f.sub.ID)
		require.ErrorIs(t, err, service.ErrSubscriptionConverted)
		_, err = NewUserSubscriptionRepository(client).Restore(ctx, f.sub.ID, "active")
		require.Error(t, err)
		result, err := svc.ConversionOverview(ctx, f.user.ID, &service.SubscriptionConversionSettings{Enabled: true})
		require.NoError(t, err)
		require.Empty(t, result.Subscriptions)
		require.Len(t, result.History, 1)
	})
	t.Run("other user cannot convert", func(t *testing.T) {
		f := create(t)
		other := create(t)
		p := overview(t, f)
		_, err := svc.ConvertToBalance(ctx, other.user.ID, f.sub.ID, p.Quote)
		require.Error(t, err)
	})
	t.Run("stale confirmation and rollback", func(t *testing.T) {
		f := create(t)
		p := overview(t, f)
		_, err := svc.ConvertToBalance(ctx, f.user.ID, f.sub.ID, "stale")
		require.ErrorIs(t, err, service.ErrConversionChanged)
		u, err := client.User.Get(ctx, f.user.ID)
		require.NoError(t, err)
		require.Equal(t, 10.0, u.Balance)
		require.True(t, p.Eligible)
	})
	t.Run("history insert failure rolls back credit and revocation", func(t *testing.T) {
		f := create(t)
		p := overview(t, f)
		_, err := client.RedeemCode.Create().SetCode(fmt.Sprintf("subconv-%d", f.sub.ID)).Save(ctx)
		require.NoError(t, err)
		_, err = svc.ConvertToBalance(ctx, f.user.ID, f.sub.ID, p.Quote)
		require.Error(t, err)
		u, err := client.User.Get(ctx, f.user.ID)
		require.NoError(t, err)
		require.Equal(t, 10.0, u.Balance)
		require.True(t, overview(t, f).Eligible)
		o, err := client.PaymentOrder.Get(ctx, f.order.ID)
		require.NoError(t, err)
		require.Equal(t, "COMPLETED", o.Status)
	})
	for _, tc := range []struct{ name, sql, reason string }{
		{"admin grant", `UPDATE user_subscriptions SET assigned_by=user_id WHERE id=$1`, "not_purchased"},
		{"gift notes", `UPDATE user_subscriptions SET notes=notes || E'\ngift' WHERE id=$1`, "not_purchased"},
		{"manual extension", `UPDATE user_subscriptions SET expires_at=expires_at+interval '1 day' WHERE id=$1`, "mixed_term"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := create(t)
			_, err := client.ExecContext(ctx, tc.sql, f.sub.ID)
			require.NoError(t, err)
			p := overview(t, f)
			require.False(t, p.Eligible)
			require.Equal(t, tc.reason, p.Reason)
		})
	}
	t.Run("refunded order blocks", func(t *testing.T) {
		f := create(t)
		_, err := client.PaymentOrder.UpdateOneID(f.order.ID).SetStatus("REFUNDED").Save(ctx)
		require.NoError(t, err)
		p := overview(t, f)
		require.False(t, p.Eligible)
		require.Equal(t, "order_unavailable", p.Reason)
	})
	t.Run("daily quota unsupported", func(t *testing.T) {
		f := create(t)
		_, err := client.Group.UpdateOneID(f.group.ID).SetDailyLimitUsd(10).Save(ctx)
		require.NoError(t, err)
		require.Equal(t, "unsupported_quota", overview(t, f).Reason)
	})
	t.Run("quota edit invalidates quote", func(t *testing.T) {
		f := create(t)
		p := overview(t, f)
		_, err := client.Group.UpdateOneID(f.group.ID).SetMonthlyLimitUsd(200).Save(ctx)
		require.NoError(t, err)
		_, err = svc.ConvertToBalance(ctx, f.user.ID, f.sub.ID, p.Quote)
		require.ErrorIs(t, err, service.ErrConversionChanged)
	})
	t.Run("renewal includes both purchases", func(t *testing.T) {
		f := create(t)
		o, err := client.PaymentOrder.Create().SetUserID(f.user.ID).SetUserEmail(f.user.Email).SetUserName("Test").SetAmount(30).SetPayAmount(210).SetRechargeCode(fmt.Sprintf("renew-%d", f.user.ID)).SetPaymentType("test").SetPaymentTradeNo("").SetOrderType("subscription").SetSubscriptionGroupID(f.group.ID).SetSubscriptionDays(30).SetStatus("COMPLETED").SetExpiresAt(time.Now().Add(time.Hour)).SetCompletedAt(time.Now()).SetClientIP("127.0.0.1").SetSrcHost("test").Save(ctx)
		require.NoError(t, err)
		_, err = client.PaymentAuditLog.Create().SetOrderID(fmt.Sprint(o.ID)).SetAction("SUBSCRIPTION_ASSIGNED").SetDetail("{}").SetOperator("system").Save(ctx)
		require.NoError(t, err)
		_, err = client.UserSubscription.UpdateOneID(f.sub.ID).SetExpiresAt(f.sub.ExpiresAt.Add(30 * 24 * time.Hour)).SetNotes(*f.sub.Notes + fmt.Sprintf("\npayment order %d", o.ID)).Save(ctx)
		require.NoError(t, err)
		p := overview(t, f)
		require.True(t, p.Eligible, p.Reason)
		require.Equal(t, "60.00", p.PriceUSD)
		require.Equal(t, "200.000000", p.TotalQuotaUSD)
		require.Len(t, p.OrderIDs, 2)
	})
	t.Run("reset does not erase lifetime usage", func(t *testing.T) {
		f := create(t)
		_, err := client.ExecContext(ctx, `UPDATE user_subscriptions SET daily_usage_usd=0,monthly_usage_usd=0 WHERE id=$1`, f.sub.ID)
		require.NoError(t, err)
		_, err = client.ExecContext(ctx, `UPDATE user_subscriptions SET daily_usage_usd=10,monthly_usage_usd=10 WHERE id=$1`, f.sub.ID)
		require.NoError(t, err)
		require.Equal(t, "30.0000000000", overview(t, f).UsedQuotaUSD)
	})
	t.Run("expired previous purchase does not contribute to new term", func(t *testing.T) {
		f := create(t)
		old, err := client.PaymentOrder.Create().SetUserID(f.user.ID).SetUserEmail(f.user.Email).
			SetUserName("Test").SetAmount(90).SetPayAmount(630).
			SetRechargeCode(fmt.Sprintf("old-%d", f.user.ID)).SetPaymentType("test").
			SetPaymentTradeNo("").SetOrderType("subscription").
			SetSubscriptionGroupID(f.group.ID).SetSubscriptionDays(90).
			SetStatus("COMPLETED").SetExpiresAt(f.sub.StartsAt.Add(-89 * 24 * time.Hour)).
			SetCompletedAt(f.sub.StartsAt.Add(-90 * 24 * time.Hour)).
			SetClientIP("127.0.0.1").SetSrcHost("test").Save(ctx)
		require.NoError(t, err)
		_, err = client.UserSubscription.UpdateOneID(f.sub.ID).
			SetNotes(fmt.Sprintf("payment order %d\n%s", old.ID, *f.sub.Notes)).Save(ctx)
		require.NoError(t, err)
		p := overview(t, f)
		require.True(t, p.Eligible, p.Reason)
		require.Equal(t, "30.00", p.PriceUSD)
		require.Equal(t, []int64{f.order.ID}, p.OrderIDs)
		_, err = svc.ConvertToBalance(ctx, f.user.ID, f.sub.ID, p.Quote)
		require.NoError(t, err)
		old, err = client.PaymentOrder.Get(ctx, old.ID)
		require.NoError(t, err)
		require.Equal(t, "COMPLETED", old.Status)
	})
	t.Run("incomplete lifetime usage cannot be converted", func(t *testing.T) {
		f := create(t)
		// A larger existing monthly counter cannot be treated as unused quota,
		// even when the migration had previously marked the ledger complete.
		_, err := client.ExecContext(ctx, `UPDATE user_subscriptions SET monthly_usage_usd=40 WHERE id=$1`, f.sub.ID)
		require.NoError(t, err)
		p := overview(t, f)
		require.False(t, p.Eligible)
		require.Equal(t, "usage_incomplete", p.Reason)
		_, err = svc.ConvertToBalance(ctx, f.user.ID, f.sub.ID, "unused")
		require.ErrorIs(t, err, service.ErrConversionUnavailable)
	})
	t.Run("disabled blocks direct submit", func(t *testing.T) {
		f := create(t)
		p := overview(t, f)
		require.NoError(t, settings.SetSubscriptionConversionSettings(ctx, &service.SubscriptionConversionSettings{}))
		_, err := svc.ConvertToBalance(ctx, f.user.ID, f.sub.ID, p.Quote)
		require.ErrorIs(t, err, service.ErrConversionDisabled)
		require.NoError(t, settings.SetSubscriptionConversionSettings(ctx, &service.SubscriptionConversionSettings{Enabled: true}))
	})
	t.Run("request and queued billing must both finish before conversion", func(t *testing.T) {
		f := create(t)
		quote := overview(t, f).Quote
		lease, err := svc.AcquireSubscriptionConversionLease(ctx, f.sub.ID, f.user.ID)
		require.NoError(t, err)
		require.True(t, lease.Retain()) // usage task queued before the HTTP handler exits
		lease.Release()                 // client already received the response
		_, err = svc.ConvertToBalance(ctx, f.user.ID, f.sub.ID, quote)
		require.ErrorIs(t, err, service.ErrConversionPending)
		require.Equal(t, "pending_requests", overview(t, f).Reason)
		u, err := client.User.Get(ctx, f.user.ID)
		require.NoError(t, err)
		require.Equal(t, 10.0, u.Balance)
		// The queued charge commits before its retained lease is released.
		require.NoError(t, NewUserSubscriptionRepository(client).IncrementUsage(ctx, f.sub.ID, 10))
		lease.Release()
		p := overview(t, f)
		require.True(t, p.Eligible, p.Reason)
		require.Equal(t, "30.0000000000", p.UsedQuotaUSD)
		r, err := svc.ConvertToBalance(ctx, f.user.ID, f.sub.ID, quote)
		require.NoError(t, err)
		require.InDelta(t, 18, mustConversionAmount(t, r.AmountUSD), 0.001)
		// Stale auth/cache state cannot admit another request after conversion.
		_, err = svc.AcquireSubscriptionConversionLease(ctx, f.sub.ID, f.user.ID)
		require.ErrorIs(t, err, service.ErrConversionUnavailable)
	})
	t.Run("disconnect does not release pending settlement", func(t *testing.T) {
		f := create(t)
		quote := overview(t, f).Quote
		requestCtx, cancel := context.WithCancel(ctx)
		lease, err := svc.AcquireSubscriptionConversionLease(requestCtx, f.sub.ID, f.user.ID)
		require.NoError(t, err)
		cancel()
		_, err = svc.ConvertToBalance(ctx, f.user.ID, f.sub.ID, quote)
		require.ErrorIs(t, err, service.ErrConversionPending)
		lease.Release()
		_, err = svc.ConvertToBalance(ctx, f.user.ID, f.sub.ID, quote)
		require.NoError(t, err)
	})

	t.Run("video remains pending across HTTP requests until its charge commits", func(t *testing.T) {
		f := create(t)
		quote := overview(t, f).Quote
		createLease, err := svc.AcquireSubscriptionConversionLease(ctx, f.sub.ID, f.user.ID)
		require.NoError(t, err)
		require.NoError(t, createLease.HoldDeferredBilling(71, "seedance:task-1"))
		require.NoError(t, createLease.HoldDeferredBilling(71, "seedance:task-1")) // duplicate response cannot double-hold
		createLease.Release()                                                      // video-create HTTP response already returned

		other := service.NewSubscriptionService(NewGroupRepository(client, integrationDB), NewUserSubscriptionRepository(client), nil, client, &config.Config{})
		defer other.Stop()
		_, err = other.ConvertToBalance(ctx, f.user.ID, f.sub.ID, quote)
		require.ErrorIs(t, err, service.ErrConversionPending)
		require.Equal(t, "pending_requests", overview(t, f).Reason)

		pollLease, err := other.AcquireSubscriptionConversionLease(ctx, f.sub.ID, f.user.ID)
		require.NoError(t, err)
		require.True(t, pollLease.Retain()) // the status request enqueues its billing task
		pollLease.Release()                 // status HTTP response returned
		require.NoError(t, NewUserSubscriptionRepository(client).IncrementUsage(ctx, f.sub.ID, 10))
		require.NoError(t, pollLease.CompleteDeferredBilling(71, "seedance:task-1"))
		_, err = other.ConvertToBalance(ctx, f.user.ID, f.sub.ID, quote)
		require.ErrorIs(t, err, service.ErrConversionPending) // billing task still owns the poll lease
		pollLease.Release()
		p := overview(t, f)
		require.True(t, p.Eligible, p.Reason)
		require.Equal(t, "30.0000000000", p.UsedQuotaUSD)
		_, err = other.ConvertToBalance(ctx, f.user.ID, f.sub.ID, quote)
		require.NoError(t, err)
	})

	t.Run("failed video settlement stays blocked for reconciliation", func(t *testing.T) {
		f := create(t)
		quote := overview(t, f).Quote
		createLease, err := svc.AcquireSubscriptionConversionLease(ctx, f.sub.ID, f.user.ID)
		require.NoError(t, err)
		require.NoError(t, createLease.HoldDeferredBilling(72, "video-task-2"))
		createLease.Release()
		pollLease, err := svc.AcquireSubscriptionConversionLease(ctx, f.sub.ID, f.user.ID)
		require.NoError(t, err)
		pollLease.MarkUnsettled() // RecordUsage returned a billing error
		require.Error(t, pollLease.CompleteDeferredBilling(72, "video-task-2"))
		pollLease.Release()
		_, err = svc.ConvertToBalance(ctx, f.user.ID, f.sub.ID, quote)
		require.ErrorIs(t, err, service.ErrConversionUnsettled)
		require.Equal(t, "unsettled_usage", overview(t, f).Reason)
	})
	t.Run("failed settlement remains blocked across service instances", func(t *testing.T) {
		f := create(t)
		quote := overview(t, f).Quote
		lease, err := svc.AcquireSubscriptionConversionLease(ctx, f.sub.ID, f.user.ID)
		require.NoError(t, err)
		lease.MarkUnsettled()
		lease.Release()
		other := service.NewSubscriptionService(NewGroupRepository(client, integrationDB), NewUserSubscriptionRepository(client), nil, client, &config.Config{})
		defer other.Stop()
		_, err = other.ConvertToBalance(ctx, f.user.ID, f.sub.ID, quote)
		require.ErrorIs(t, err, service.ErrConversionUnsettled)
		require.Equal(t, "unsettled_usage", overview(t, f).Reason)
	})
	t.Run("admission and conversion race has only one winner", func(t *testing.T) {
		for i := 0; i < 12; i++ {
			f := create(t)
			quote := overview(t, f).Quote
			start := make(chan struct{})
			type admissionResult struct {
				lease *service.SubscriptionConversionLease
				err   error
			}
			admitted := make(chan admissionResult, 1)
			converted := make(chan error, 1)
			go func() {
				<-start
				lease, err := svc.AcquireSubscriptionConversionLease(ctx, f.sub.ID, f.user.ID)
				admitted <- admissionResult{lease, err}
			}()
			go func() {
				<-start
				_, err := svc.ConvertToBalance(ctx, f.user.ID, f.sub.ID, quote)
				converted <- err
			}()
			close(start)
			a, conversionErr := <-admitted, <-converted
			if a.err == nil {
				require.ErrorIs(t, conversionErr, service.ErrConversionPending)
				a.lease.Release()
			} else {
				require.ErrorIs(t, a.err, service.ErrConversionUnavailable)
				require.NoError(t, conversionErr)
			}
		}
	})
}

func mustConversionAmount(t *testing.T, amount string) float64 {
	t.Helper()
	value, err := strconv.ParseFloat(amount, 64)
	require.NoError(t, err)
	return value
}

func TestSubscriptionConversionMigration(t *testing.T) {
	ctx := context.Background()
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	schema := fmt.Sprintf("conversion_migration_%d", time.Now().UnixNano())
	_, err = tx.ExecContext(ctx, `CREATE SCHEMA `+schema+`; SET LOCAL search_path TO `+schema)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `CREATE TABLE users(id BIGINT PRIMARY KEY);
	CREATE TABLE user_subscriptions(id BIGINT PRIMARY KEY,starts_at TIMESTAMPTZ,daily_usage_usd NUMERIC,weekly_usage_usd NUMERIC,monthly_usage_usd NUMERIC);
	CREATE TABLE usage_logs(subscription_id BIGINT,billing_type INTEGER,created_at TIMESTAMPTZ,actual_cost NUMERIC);
	CREATE TABLE usage_cleanup_tasks(deleted_rows BIGINT,created_at TIMESTAMPTZ);
	INSERT INTO user_subscriptions VALUES(1,'2026-10-01',5,10,20),(2,'2026-10-01',5,10,20);
	INSERT INTO usage_logs VALUES(1,1,'2026-10-02',20),(1,1,'2026-09-01',99),(2,1,'2026-10-02',5);`)
	require.NoError(t, err)
	script, err := migrations.FS.ReadFile("269_subscription_conversions.sql")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(script))
	require.NoError(t, err)
	var used string
	var complete bool
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT conversion_used_usd::text,conversion_usage_complete FROM user_subscriptions WHERE id=1`).Scan(&used, &complete))
	require.Equal(t, "20.0000000000", used)
	require.True(t, complete)
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT conversion_usage_complete FROM user_subscriptions WHERE id=2`).Scan(&complete))
	require.False(t, complete)
	_, err = tx.ExecContext(ctx, `UPDATE user_subscriptions SET daily_usage_usd=daily_usage_usd+10 WHERE id=1; DELETE FROM usage_logs;`)
	require.NoError(t, err)
	// Reapplying must not replace a persisted lifetime ledger with cleaned logs.
	_, err = tx.ExecContext(ctx, string(script))
	require.NoError(t, err)
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT conversion_used_usd::text,conversion_usage_complete FROM user_subscriptions WHERE id=1`).Scan(&used, &complete))
	require.Equal(t, "30.0000000000", used)
	require.True(t, complete)
	_, err = tx.ExecContext(ctx, `UPDATE user_subscriptions SET starts_at='2026-11-01',daily_usage_usd=0,weekly_usage_usd=0,monthly_usage_usd=0 WHERE id=2`)
	require.NoError(t, err)
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT conversion_used_usd::text,conversion_usage_complete FROM user_subscriptions WHERE id=2`).Scan(&used, &complete))
	require.Equal(t, "0.0000000000", used)
	require.True(t, complete)
}
