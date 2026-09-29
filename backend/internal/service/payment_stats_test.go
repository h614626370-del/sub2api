//go:build unit

package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

func TestPaymentDashboardRangeFiltersDatabaseAndKeepsTodayIndependent(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)
	user, err := client.User.Create().SetEmail("dashboard@example.com").SetUsername("dashboard").SetPasswordHash("test").Save(ctx)
	require.NoError(t, err)
	now := time.Now()
	period, err := ParsePaymentDashboardRange("2024-02-01", "2024-02-29", "", now)
	require.NoError(t, err)
	for i, item := range []struct {
		paidAt time.Time
		amount float64
		status string
	}{
		{period.Start.Add(-time.Second), 1000, OrderStatusCompleted},
		{period.Start, 10, OrderStatusCompleted},
		{period.End.Add(-time.Second), 20, OrderStatusPaid},
		{period.End, 2000, OrderStatusCompleted},
		{period.Start.Add(time.Hour), 500, OrderStatusPending},
		{now, 99, OrderStatusCompleted},
	} {
		_, err := client.PaymentOrder.Create().SetUserID(user.ID).SetUserEmail(user.Email).SetUserName(user.Username).
			SetAmount(item.amount).SetPayAmount(item.amount).SetRechargeCode(fmt.Sprintf("dashboard-%d", i)).
			SetOutTradeNo(fmt.Sprintf("dashboard-%d", i)).SetPaymentType("stripe").SetPaymentTradeNo("").
			SetProviderSnapshot(map[string]any{"currency": "USD"}).SetStatus(item.status).SetPaidAt(item.paidAt).
			SetExpiresAt(now.Add(time.Hour)).SetClientIP("127.0.0.1").SetSrcHost("test").Save(ctx)
		require.NoError(t, err)
	}
	s := &PaymentService{entClient: client}
	stats, err := s.GetDashboardStatsForRange(ctx, period)
	require.NoError(t, err)
	require.Equal(t, CurrencyAmounts{"USD": 30}, stats.TotalAmount)
	require.Equal(t, 2, stats.TotalCount)
	require.Equal(t, CurrencyAmounts{"USD": 15}, stats.AvgAmount)
	require.Equal(t, CurrencyAmounts{"USD": 99}, stats.TodayAmount)
	require.Equal(t, 1, stats.TodayCount)
	require.Equal(t, 1, stats.PendingOrders)
	require.Len(t, stats.DailySeries, 29)
	require.Equal(t, 1, stats.DailySeries[0].Count)
	require.Equal(t, 1, stats.DailySeries[28].Count)
	require.Equal(t, 30.0, stats.PaymentMethods[0].Amount["USD"])
	require.Equal(t, 30.0, stats.TopUsers["USD"][0].Amount)
	require.Equal(t, "2024-02-01", stats.StartDate)
	require.Equal(t, "2024-02-29", stats.EndDate)

	empty, err := ParsePaymentDashboardRange("2023-01-01", "2023-01-31", "", now)
	require.NoError(t, err)
	stats, err = s.GetDashboardStatsForRange(ctx, empty)
	require.NoError(t, err)
	require.Empty(t, stats.TotalAmount)
	require.Equal(t, 0, stats.TotalCount)
	require.Len(t, stats.DailySeries, 31)
	require.Equal(t, CurrencyAmounts{"USD": 99}, stats.TodayAmount)
}

func TestComputeBasicStatsGroupsAmountsByCurrency(t *testing.T) {
	t.Parallel()

	todayStart := time.Date(2026, time.July, 25, 0, 0, 0, 0, time.UTC)
	yesterday := todayStart.Add(-time.Hour)
	today := todayStart.Add(time.Hour)
	orders := []*dbent.PaymentOrder{
		paymentStatsTestOrder(1, "alice@example.com", "CNY", 10, &today),
		paymentStatsTestOrder(2, "bob@example.com", "USD", 10, &today),
		paymentStatsTestOrder(1, "alice@example.com", "CNY", 5, &yesterday),
	}

	stats := &DashboardStats{}
	computeBasicStats(stats, orders, todayStart)

	require.Equal(t, CurrencyAmounts{"CNY": 15, "USD": 10}, stats.TotalAmount)
	require.Equal(t, CurrencyAmounts{"CNY": 10, "USD": 10}, stats.TodayAmount)
	require.Equal(t, CurrencyAmounts{"CNY": 7.5, "USD": 10}, stats.AvgAmount)
	require.Equal(t, 3, stats.TotalCount)
	require.Equal(t, 2, stats.TodayCount)
}

func TestPaymentDashboardBreakdownsGroupAmountsAndRankingsByCurrency(t *testing.T) {
	t.Parallel()

	firstDay := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)
	secondDay := firstDay.AddDate(0, 0, 1)
	orders := []*dbent.PaymentOrder{
		paymentStatsTestOrder(1, "alice@example.com", "CNY", 5.555, &firstDay),
		paymentStatsTestOrder(2, "bob@example.com", "CNY", 10, &firstDay),
		paymentStatsTestOrder(1, "alice@example.com", "USD", 20, &secondDay),
		paymentStatsTestOrder(2, "bob@example.com", "USD", 10, &secondDay),
	}
	orders[0].PaymentType = "stripe"
	orders[1].PaymentType = "stripe"
	orders[2].PaymentType = "stripe"
	orders[3].PaymentType = "alipay"

	daily := buildDailySeries(orders, firstDay, 2)
	require.Equal(t, []DailyStats{
		{Date: "2026-07-24", Amount: CurrencyAmounts{"CNY": 15.56}, Count: 2},
		{Date: "2026-07-25", Amount: CurrencyAmounts{"USD": 30}, Count: 2},
	}, daily)

	methods := buildMethodDistribution(orders)
	require.Equal(t, []PaymentMethodStat{
		{Type: "alipay", Amount: CurrencyAmounts{"USD": 10}, Count: 1},
		{Type: "stripe", Amount: CurrencyAmounts{"CNY": 15.56, "USD": 20}, Count: 3},
	}, methods)

	users := buildTopUsers(orders)
	require.Equal(t, TopUsersByCurrency{
		"CNY": {
			{UserID: 2, Email: "bob@example.com", Amount: 10},
			{UserID: 1, Email: "alice@example.com", Amount: 5.56},
		},
		"USD": {
			{UserID: 1, Email: "alice@example.com", Amount: 20},
			{UserID: 2, Email: "bob@example.com", Amount: 10},
		},
	}, users)
}

func paymentStatsTestOrder(userID int64, email, currency string, amount float64, paidAt *time.Time) *dbent.PaymentOrder {
	return &dbent.PaymentOrder{
		UserID:           userID,
		UserEmail:        email,
		PayAmount:        amount,
		PaidAt:           paidAt,
		ProviderSnapshot: map[string]any{"currency": currency},
	}
}

func TestPaymentDailySeriesIncludesFirstAndLastDayInReportingTimezone(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	require.NoError(t, err)
	start := time.Date(2026, 3, 7, 0, 0, 0, 0, loc)
	first := start.UTC()
	last := start.AddDate(0, 0, 3).Add(-time.Nanosecond).UTC()
	orders := []*dbent.PaymentOrder{
		paymentStatsTestOrder(1, "a@example.com", "USD", 10, &first),
		paymentStatsTestOrder(1, "a@example.com", "USD", 20, &last),
	}
	series := buildDailySeries(orders, start, 3)
	require.Len(t, series, 3)
	require.Equal(t, DailyStats{Date: "2026-03-07", Amount: CurrencyAmounts{"USD": 10}, Count: 1}, series[0])
	require.Equal(t, DailyStats{Date: "2026-03-08", Amount: CurrencyAmounts{}, Count: 0}, series[1])
	require.Equal(t, DailyStats{Date: "2026-03-09", Amount: CurrencyAmounts{"USD": 20}, Count: 1}, series[2])
}
