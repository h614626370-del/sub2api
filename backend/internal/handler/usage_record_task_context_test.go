package handler

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestWrapUsageRecordTaskContextPreservesBalanceReservation(t *testing.T) {
	for _, mode := range []string{"complete", "abandon", "panic"} {
		t.Run(mode, func(t *testing.T) {
			cache := newHandlerInflightCache(1)
			cfg := &config.Config{}
			cfg.Billing.InflightReservation = config.InflightReservationConfig{Enabled: true, TTLSeconds: 60}
			billing := service.NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
			t.Cleanup(billing.Stop)
			reservation, err := billing.ReserveInflight(context.Background(), &service.User{ID: 66}, nil, nil, 0.9)
			require.NoError(t, err)
			require.NotNil(t, reservation)
			parent := service.WithInflightReservation(context.Background(), reservation)
			ran := false
			task, abandon := wrapUsageRecordTaskContext(parent, func(context.Context) {
				ran = true
				if mode == "panic" {
					panic("billing failed")
				}
			})
			reservation.HandlerDone()
			require.Equal(t, 1, cache.count(), "queued billing must retain the balance reservation")
			switch mode {
			case "complete":
				task(context.Background())
			case "abandon":
				abandon()
			case "panic":
				require.Panics(t, func() { task(context.Background()) })
			}
			abandon()
			require.Equal(t, mode != "abandon", ran)
			require.Zero(t, cache.count())
		})
	}
}
