//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionConversionPublicSettings(t *testing.T) {
	for _, raw := range []string{"", `{"enabled":false}`, `{"enabled":true}`, `invalid`} {
		s := NewSettingService(&settingPublicRepoStub{values: map[string]string{SettingKeySubscriptionConversion: raw}}, &config.Config{})
		settings, err := s.GetPublicSettings(context.Background())
		require.NoError(t, err)
		require.Equal(t, raw == `{"enabled":true}`, settings.SubscriptionConversionEnabled)
		injected, err := s.GetPublicSettingsForInjection(context.Background())
		require.NoError(t, err)
		require.Equal(t, settings.SubscriptionConversionEnabled, injected.(*PublicSettingsInjectionPayload).SubscriptionConversionEnabled)
	}
}

func TestSubscriptionConversionOrderCannotRefundOrFulfillAgain(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createPaymentFulfillmentSubscriptionOrder(t, ctx, client, OrderStatusConverted, time.Now())
	svc := &PaymentService{entClient: client}
	_, _, err := svc.PrepareRefund(ctx, order.ID, 0, "test", true, true)
	require.Error(t, err)
	_, err = svc.ExecuteRefund(ctx, &RefundPlan{OrderID: order.ID})
	require.Error(t, err)
	require.NoError(t, svc.ExecuteSubscriptionFulfillment(ctx, order.ID))
	require.NoError(t, svc.alreadyProcessed(ctx, order))
	updated, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusConverted, updated.Status)
}
