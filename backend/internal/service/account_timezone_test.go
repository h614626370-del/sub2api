package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type timezoneProxyRepo struct {
	ProxyRepository
	proxy *Proxy
}

func (r *timezoneProxyRepo) GetByID(context.Context, int64) (*Proxy, error) {
	p := *r.proxy
	return &p, nil
}

type timezoneProber struct {
	ProxyExitInfoProber
	calls  int
	fail   bool
	during func()
}

func (p *timezoneProber) ProbeProxyTimezone(context.Context, string) (*ProxyExitInfo, error) {
	p.calls++
	if p.during != nil {
		p.during()
	}
	if p.fail {
		return nil, errors.New("unavailable")
	}
	return &ProxyExitInfo{IP: "203.0.113.10", Timezone: "America/Los_Angeles"}, nil
}

func TestAccountTimezoneDetectionOverrideAndProxyChange(t *testing.T) {
	ctx := context.Background()
	id := int64(1)
	repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{1: {ID: 1, ProxyID: &id}}}
	proxy := &timezoneProxyRepo{proxy: &Proxy{ID: 1, Host: "proxy.example", Port: 8080, Protocol: "http", Status: StatusActive}}
	probe := &timezoneProber{}
	s := &adminServiceImpl{accountRepo: repo, proxyRepo: proxy, proxyProber: probe}
	state, err := s.DetectAccountTimezone(ctx, 1, false)
	require.NoError(t, err)
	require.Equal(t, "proxy", state.Source)
	_, err = s.DetectAccountTimezone(ctx, 1, false)
	require.NoError(t, err)
	require.Equal(t, 1, probe.calls)
	state, err = s.SetAccountTimezone(ctx, 1, "Asia/Tokyo")
	require.NoError(t, err)
	require.Equal(t, "manual", state.Source)
	state, err = s.DetectAccountTimezone(ctx, 1, true)
	require.NoError(t, err)
	require.Equal(t, "Asia/Tokyo", state.Timezone)
	probe.fail = true
	_, err = s.DetectAccountTimezone(ctx, 1, true)
	require.Error(t, err)
	state, err = s.GetAccountTimezone(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, "Asia/Tokyo", state.Timezone)
	proxy.proxy.Host = "changed.example"
	state, err = s.SetAccountTimezone(ctx, 1, "")
	require.NoError(t, err)
	require.True(t, state.Stale)
	require.Empty(t, state.Timezone)
	probe.fail = false
	state, err = s.DetectAccountTimezone(ctx, 1, false)
	require.NoError(t, err)
	require.False(t, state.Stale)
	for _, invalid := range []string{"Local", "Not/AZone", "+08:00"} {
		_, err = s.SetAccountTimezone(ctx, 1, invalid)
		require.Error(t, err)
	}
	probe.during = func() { _, e := s.SetAccountTimezone(ctx, 1, "Europe/London"); require.NoError(t, e) }
	state, err = s.DetectAccountTimezone(ctx, 1, true)
	require.NoError(t, err)
	require.Equal(t, "Europe/London", state.Timezone)
	probe.during = func() { proxy.proxy.Host = "another.example" }
	_, err = s.DetectAccountTimezone(ctx, 1, true)
	require.Error(t, err)
}

func TestAccountTimezoneManagedStateSurvivesGeneralEdit(t *testing.T) {
	current := map[string]any{accountTimezoneOverrideKey: "Asia/Tokyo", accountTimezoneDetectedKey: "saved"}
	got := MergeAccountTimezoneExtra(map[string]any{accountTimezoneOverrideKey: "fake", "other": true}, current)
	require.Equal(t, "Asia/Tokyo", got[accountTimezoneOverrideKey])
	require.Equal(t, "saved", got[accountTimezoneDetectedKey])
	require.Equal(t, true, got["other"])
	require.NotContains(t, MergeAccountTimezoneExtra(current, nil), accountTimezoneOverrideKey)
}
