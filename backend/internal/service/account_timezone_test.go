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

func TestAccountTimezoneDetectionDefaultAndProxyChange(t *testing.T) {
	ctx := context.Background()
	id := int64(1)
	repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{1: {ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ProxyID: &id, Extra: map[string]any{accountTimezoneOverrideKey: "Asia/Tokyo"}}}}
	proxy := &timezoneProxyRepo{proxy: &Proxy{ID: 1, Host: "proxy.example", Port: 8080, Protocol: "http", Status: StatusActive}}
	probe := &timezoneProber{}
	settingsRepo := &codexPolicyMigrationRepoStub{values: map[string]string{SettingKeyOpenAIOAuthDefaultTimezone: "Europe/London"}}
	s := &adminServiceImpl{accountRepo: repo, proxyRepo: proxy, proxyProber: probe, settingService: NewSettingService(settingsRepo, nil)}
	state, err := s.GetAccountTimezone(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, "global", state.Source)
	require.Equal(t, "Europe/London", state.Timezone)
	state, err = s.DetectAccountTimezone(ctx, 1, false)
	require.NoError(t, err)
	require.Equal(t, "proxy", state.Source)
	_, err = s.DetectAccountTimezone(ctx, 1, false)
	require.NoError(t, err)
	require.Equal(t, 1, probe.calls)
	state, err = s.SetAccountTimezone(ctx, 1, "Asia/Tokyo")
	require.Error(t, err)
	require.Nil(t, state)
	state, err = s.DetectAccountTimezone(ctx, 1, true)
	require.NoError(t, err)
	require.Equal(t, "America/Los_Angeles", state.Timezone)
	probe.fail = true
	_, err = s.DetectAccountTimezone(ctx, 1, true)
	require.Error(t, err)
	state, err = s.GetAccountTimezone(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, "America/Los_Angeles", state.Timezone)
	proxy.proxy.Host = "changed.example"
	state, err = s.GetAccountTimezone(ctx, 1)
	require.NoError(t, err)
	require.True(t, state.Stale)
	require.Equal(t, "Europe/London", state.Timezone)
	require.Equal(t, "global", state.Source)
	probe.fail = false
	state, err = s.DetectAccountTimezone(ctx, 1, false)
	require.NoError(t, err)
	require.False(t, state.Stale)
	for _, invalid := range []string{"Local", "Not/AZone", "+08:00"} {
		_, err = s.SetAccountTimezone(ctx, 1, invalid)
		require.Error(t, err)
	}
	require.Equal(t, "Asia/Tokyo", repo.accounts[1].Extra[accountTimezoneOverrideKey])
	probe.during = func() { proxy.proxy.Host = "another.example" }
	_, err = s.DetectAccountTimezone(ctx, 1, true)
	require.Error(t, err)
}

func TestAccountTimezoneEligibilityAndNoProxy(t *testing.T) {
	ctx := context.Background()
	repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{1: {ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth}}}
	settingsRepo := &codexPolicyMigrationRepoStub{values: map[string]string{SettingKeyOpenAIOAuthDefaultTimezone: "Asia/Shanghai"}}
	s := &adminServiceImpl{accountRepo: repo, settingService: NewSettingService(settingsRepo, nil)}
	state, err := s.GetAccountTimezone(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, "Asia/Shanghai", state.Timezone)
	require.Equal(t, "global", state.Source)
	require.False(t, state.HasProxy)
	_, err = s.DetectAccountTimezone(ctx, 1, false)
	require.Error(t, err)
	settingsRepo.values[SettingKeyOpenAIOAuthDefaultTimezone] = ""
	s.settingService.invalidateOpenAIOAuthTimezoneCache()
	state, err = s.GetAccountTimezone(ctx, 1)
	require.NoError(t, err)
	require.Empty(t, state.Timezone)
	require.Equal(t, "none", state.Source)
	for _, tc := range []struct{ platform, kind string }{
		{PlatformOpenAI, AccountTypeAPIKey}, {PlatformOpenAI, AccountTypeSetupToken},
		{PlatformAnthropic, AccountTypeOAuth},
	} {
		repo.accounts[1].Platform, repo.accounts[1].Type = tc.platform, tc.kind
		_, err = s.GetAccountTimezone(ctx, 1)
		require.Error(t, err)
		_, err = s.DetectAccountTimezone(ctx, 1, true)
		require.Error(t, err)
	}
}
func TestAccountTimezoneManagedStateSurvivesGeneralEdit(t *testing.T) {
	current := map[string]any{accountTimezoneOverrideKey: "Asia/Tokyo", accountTimezoneDetectedKey: "saved"}
	got := MergeAccountTimezoneExtra(map[string]any{accountTimezoneOverrideKey: "fake", "other": true}, current)
	require.Equal(t, "Asia/Tokyo", got[accountTimezoneOverrideKey])
	require.Equal(t, "saved", got[accountTimezoneDetectedKey])
	require.Equal(t, true, got["other"])
	require.NotContains(t, MergeAccountTimezoneExtra(current, nil), accountTimezoneOverrideKey)
}
