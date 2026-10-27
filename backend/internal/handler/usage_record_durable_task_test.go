//go:build unit

package handler

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestDurableBillingBypassesFullQueue(t *testing.T) {
	for _, kind := range []string{"gateway", "openai"} {
		for _, outcome := range []string{"success", "panic"} {
			t.Run(kind+"/"+outcome, func(t *testing.T) {
				pool := service.NewUsageRecordWorkerPoolWithOptions(service.UsageRecordWorkerPoolOptions{
					WorkerCount: 1, QueueSize: 1, TaskTimeout: time.Second,
					OverflowPolicy: config.UsageRecordOverflowPolicyDrop,
				})
				started, unblock := make(chan struct{}), make(chan struct{})
				t.Cleanup(func() { close(unblock); pool.Stop() })
				pool.Submit(func(context.Context) { close(started); <-unblock })
				<-started
				pool.Submit(func(context.Context) { <-unblock })

				cfg := &config.Config{}
				repo := &httpDeliveryBilling{}
				var submit func(context.Context, service.UsageRecordTask)
				if kind == "openai" {
					gw := service.NewOpenAIGatewayService(
						nil, nil, nil, repo, nil, nil, nil, nil, cfg,
						nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
					)
					require.True(t, gw.DurableUsageBillingEnabled())
					submit = (&OpenAIGatewayHandler{gatewayService: gw, usageRecordWorkerPool: pool}).submitUsageRecordTask
				} else {
					gw := service.NewGatewayService(
						nil, nil, nil, repo, nil, nil, nil, nil, cfg,
						nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
					)
					require.True(t, gw.DurableUsageBillingEnabled())
					submit = (&GatewayHandler{gatewayService: gw, usageRecordWorkerPool: pool}).submitUsageRecordTask
				}
				ran := false
				parent, cancel := context.WithCancel(context.Background())
				cancel()
				submit(parent, func(ctx context.Context) {
					ran = true
					require.NoError(t, ctx.Err(), "client cancellation must not cancel billing")
					if outcome == "panic" {
						panic("injected billing failure")
					}
				})
				require.True(t, ran, "durable billing must finish synchronously despite a full worker queue")
			})
		}
	}
}
