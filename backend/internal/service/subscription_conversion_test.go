package service

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionConversionFormula(t *testing.T) {
	for _, tc := range []struct {
		name, price, monthly, used string
		total, left                int64
		want                       string
	}{
		{"half time 80 percent quota", "30", "100", "20", 30 * 86400, 15 * 86400, "19.500000"},
		{"untouched", "30", "100", "0", 30 * 86400, 30 * 86400, "30.000000"},
		{"quota exhausted still half time component", "30", "100", "150", 30 * 86400, 15 * 86400, "7.500000"},
		{"two month renewal", "60", "100", "50", 60 * 86400, 30 * 86400, "37.500000"},
		{"partial month", "15", "100", "25", 15 * 86400, 10 * 86400, "8.750000"},
		{"expired", "30", "100", "0", 30 * 86400, 0, "0.000000"},
		{"unlimited unsupported", "30", "0", "0", 30 * 86400, 15 * 86400, "0.000000"},
		{"time clamped", "30", "100", "0", 30 * 86400, 60 * 86400, "30.000000"},
		{"round down", "1", "100", "100", 30 * 86400, 1, "0.000000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			amount, _, _, _, _ := calculateSubscriptionConversion(decimal.RequireFromString(tc.price), decimal.RequireFromString(tc.monthly), decimal.RequireFromString(tc.used), tc.total, tc.left)
			require.Equal(t, tc.want, amount.StringFixed(6))
		})
	}
}
func TestSubscriptionConversionPurchaseNotes(t *testing.T) {
	ids, ok := purchaseOrderIDs("payment order 12\r\npayment order 34")
	require.True(t, ok)
	require.Equal(t, []int64{12, 34}, ids)
	for _, notes := range []string{"", "admin grant", "payment order 12\ngift", "payment order 12\npayment order 12", "payment order 9223372036854775808", "payment order -1"} {
		_, ok := purchaseOrderIDs(notes)
		require.False(t, ok, notes)
	}
}
func TestSubscriptionConversionDisabledByDefault(t *testing.T) {
	for _, raw := range []string{"", `{}`, `{"enabled":false}`, `broken`} {
		require.False(t, conversionEnabled(raw))
	}
	require.True(t, conversionEnabled(`{"enabled":true}`))
}
