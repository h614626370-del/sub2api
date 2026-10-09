package handler

import (
	"context"
	"sync"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func conversionTaskLease(t *testing.T) (*service.SubscriptionConversionLease, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	svc := service.NewSubscriptionService(nil, nil, nil, client, &config.Config{})
	t.Cleanup(func() {
		svc.Stop()
		require.NoError(t, mock.ExpectationsWereMet())
		_ = db.Close()
	})
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id FROM user_subscriptions").
		WithArgs(int64(55), int64(66), service.SubscriptionStatusActive).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(55))
	mock.ExpectExec("INSERT INTO subscription_conversion_leases").
		WithArgs(sqlmock.AnyArg(), int64(55), int64(66)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	lease, err := svc.AcquireSubscriptionConversionLease(context.Background(), 55, 66)
	require.NoError(t, err)
	return lease, mock
}

func conversionTaskSubmitter(kind string, pool *service.UsageRecordWorkerPool) func(context.Context, service.UsageRecordTask) {
	if kind == "openai" {
		return (&OpenAIGatewayHandler{usageRecordWorkerPool: pool}).submitUsageRecordTask
	}
	return (&GatewayHandler{usageRecordWorkerPool: pool}).submitUsageRecordTask
}

func TestSubscriptionConversionTaskRetainsLeaseAfterHTTPReturns(t *testing.T) {
	for _, kind := range []string{"gateway", "openai"} {
		t.Run(kind, func(t *testing.T) {
			lease, mock := conversionTaskLease(t)
			pool := service.NewUsageRecordWorkerPoolWithOptions(service.UsageRecordWorkerPoolOptions{
				WorkerCount: 1, QueueSize: 1, TaskTimeout: time.Second,
			})
			started, unblock := make(chan struct{}), make(chan struct{})
			unblockWorker := sync.OnceFunc(func() { close(unblock) })
			t.Cleanup(func() { unblockWorker(); pool.Stop() })
			require.Equal(t, service.UsageRecordSubmitModeEnqueued, pool.Submit(func(context.Context) {
				close(started)
				<-unblock
			}))
			<-started
			parent, cancel := context.WithCancel(service.WithSubscriptionConversionLease(context.Background(), lease))
			gotLease := make(chan *service.SubscriptionConversionLease, 1)
			conversionTaskSubmitter(kind, pool)(parent, func(ctx context.Context) {
				gotLease <- service.SubscriptionConversionLeaseFromContext(ctx)
			})
			cancel()
			lease.Release() // response returned before the queued task runs
			require.True(t, lease.Retain(), "queued billing must keep the request active")
			lease.Release()
			mock.ExpectExec("DELETE FROM subscription_conversion_leases WHERE id=").WillReturnResult(sqlmock.NewResult(0, 1))
			unblockWorker()
			pool.Stop()
			require.Same(t, lease, <-gotLease)
		})
	}
}

func TestSubscriptionConversionTaskNeverDropsOnOverflow(t *testing.T) {
	for _, kind := range []string{"gateway", "openai"} {
		t.Run(kind, func(t *testing.T) {
			lease, mock := conversionTaskLease(t)
			pool := service.NewUsageRecordWorkerPoolWithOptions(service.UsageRecordWorkerPoolOptions{
				WorkerCount: 1, QueueSize: 1, TaskTimeout: time.Second,
				OverflowPolicy: config.UsageRecordOverflowPolicyDrop,
			})
			started, unblock := make(chan struct{}), make(chan struct{})
			unblockWorker := sync.OnceFunc(func() { close(unblock) })
			t.Cleanup(func() { unblockWorker(); pool.Stop() })
			require.Equal(t, service.UsageRecordSubmitModeEnqueued, pool.Submit(func(context.Context) {
				close(started)
				<-unblock
			}))
			<-started
			require.Equal(t, service.UsageRecordSubmitModeEnqueued, pool.Submit(func(context.Context) { <-unblock }))
			executed := false
			conversionTaskSubmitter(kind, pool)(service.WithSubscriptionConversionLease(context.Background(), lease), func(ctx context.Context) {
				executed = true
				require.Same(t, lease, service.SubscriptionConversionLeaseFromContext(ctx))
			})
			require.True(t, executed, "a subscription bill must use synchronous fallback")
			mock.ExpectExec("DELETE FROM subscription_conversion_leases WHERE id=").WillReturnResult(sqlmock.NewResult(0, 1))
			lease.Release()
		})
	}
}

func TestSubscriptionConversionTaskFailureKeepsLease(t *testing.T) {
	for _, mode := range []string{"billing_error", "panic"} {
		t.Run(mode, func(t *testing.T) {
			lease, mock := conversionTaskLease(t)
			conversionTaskSubmitter("openai", nil)(service.WithSubscriptionConversionLease(context.Background(), lease), func(ctx context.Context) {
				if mode == "panic" {
					panic("billing task failed")
				}
				err := (&service.OpenAIGatewayService{}).RecordUsage(ctx, &service.OpenAIRecordUsageInput{
					Subscription: &service.UserSubscription{ID: 55},
					Result:       &service.OpenAIForwardResult{UsageUnavailable: true},
				})
				require.Error(t, err)
			})
			mock.ExpectExec("UPDATE subscription_conversion_leases SET state='unsettled'").WillReturnResult(sqlmock.NewResult(0, 1))
			lease.Release()
		})
	}
}
