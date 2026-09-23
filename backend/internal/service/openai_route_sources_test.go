package service

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOpenAIRouteSourcePoolIsolation(t *testing.T) {
	for _, advanced := range []bool{false, true} {
		t.Run(strconv.FormatBool(advanced), func(t *testing.T) {
			for _, tc := range []struct {
				name, sources, target, model string
				source, want                 int64
				fail                         bool
			}{
				{"selected", "[10]", "20", "gpt-6-astra", 10, 2, false},
				{"unselected", "[10]", "20", "gpt-6-astra", 30, 3, false},
				{"new source", "[10]", "20", "gpt-6-sol", 30, 3, false},
				{"empty", "[]", "20", "gpt-6-astra", 10, 1, false},
				{"disabled", "[10]", "0", "gpt-6-astra", 10, 1, false},
				{"invalid unrelated target", "[10]", "999", "gpt-6-astra", 30, 3, false},
				{"malformed unrelated target", "[10]", "bad", "gpt-6-astra", 30, 3, false},
				{"invalid selected target", "[10]", "999", "gpt-6-astra", 10, 0, true},
				{"malformed selected target", "[10]", "bad", "gpt-6-astra", 10, 0, true},
				{"alias", "[10]", "20", "gpt-6", 10, 2, false},
				{"dated model", "[10]", "20", "gpt-6-astra-2026-09-01", 10, 2, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					svc, repo, _ := astraRouteFixture(t, advanced)
					repo.values[SettingKeyOpenAIAstraSourceGroupIDs] = tc.sources
					repo.values[SettingKeyOpenAIAstraGroupID] = tc.target
					result, _, err := svc.SelectAccountWithScheduler(context.Background(), &tc.source, "", "same-session", tc.model, nil, OpenAIUpstreamTransportAny, false)
					if tc.fail {
						require.ErrorIs(t, err, ErrNoAvailableAccounts)
						return
					}
					require.NoError(t, err)
					require.Equal(t, tc.want, result.Account.ID)
					result.ReleaseFunc()
				})
			}
		})
	}
}

func TestOpenAIRouteIndependentSourcesAndOriginalGroup(t *testing.T) {
	svc, repo, _ := astraRouteFixture(t, false)
	repo.values[SettingKeyOpenAISolSourceGroupIDs] = "[30]"
	for _, tc := range []struct {
		source int64
		model  string
		want   int64
	}{{10, "gpt-6-astra", 20}, {10, "gpt-6-sol", 0}, {30, "gpt-6-astra", 0}, {30, "gpt-6-sol", 20}} {
		ctx := svc.WithOpenAIModelRoute(context.Background(), &tc.source, tc.model, PlatformOpenAI)
		require.Equal(t, tc.want, openAIModelRouteGroup(ctx))
		// A resolved composite member or channel mapping cannot replace the source.
		memberID := int64(99)
		ctx, err := svc.ensureOpenAIModelRoute(ctx, &memberID, "mapped-model", PlatformOpenAI)
		require.NoError(t, err)
		require.Equal(t, tc.want, openAIModelRouteGroup(ctx))
	}
	source := int64(10)
	for _, platform := range []string{PlatformAnthropic, PlatformGemini, PlatformGrok} {
		ctx := svc.WithOpenAIModelRoute(context.Background(), &source, "gpt-6-astra", platform)
		require.Zero(t, openAIModelRouteGroup(ctx))
		ctx = WithResolvedTargetPlatform(context.Background(), platform)
		ctx = svc.WithOpenAIModelRoute(ctx, &source, "gpt-6-astra", PlatformOpenAI)
		require.Zero(t, openAIModelRouteGroup(ctx), "composite resolution must precede platform normalization")
	}
	ctx := WithResolvedTargetPlatform(context.Background(), PlatformOpenAI)
	require.EqualValues(t, 20, openAIModelRouteGroup(svc.WithOpenAIModelRoute(ctx, &source, "gpt-6-astra", PlatformComposite)))
	require.Zero(t, openAIModelRouteGroup(svc.WithOpenAIModelRoute(context.Background(), nil, "gpt-6-astra", PlatformOpenAI)))
}

func TestOpenAIRouteWebSocketSourceChanges(t *testing.T) {
	svc, repo, _ := astraRouteFixture(t, true)
	source := int64(10)
	ctx := svc.WithOpenAIModelRoute(context.Background(), &source, "gpt-6-astra", PlatformOpenAI)
	special, err := svc.accountRepo.GetByID(ctx, 2)
	require.NoError(t, err)
	require.True(t, svc.OpenAIModelRouteAllowsAccount(ctx, special, &source, "gpt-6-astra", PlatformOpenAI))
	repo.values[SettingKeyOpenAIAstraSourceGroupIDs] = "[]"
	svc.settingService.invalidateOpenAIAstraGroupCache()
	require.False(t, svc.OpenAIModelRouteAllowsAccount(ctx, special, &source, "gpt-6-astra", PlatformOpenAI))
	normalCtx := svc.WithOpenAIModelRoute(context.Background(), &source, "gpt-6-astra", PlatformOpenAI)
	normal, err := svc.accountRepo.GetByID(ctx, 1)
	require.NoError(t, err)
	require.True(t, svc.OpenAIModelRouteAllowsAccount(normalCtx, normal, &source, "gpt-6-astra", PlatformOpenAI))
	repo.values[SettingKeyOpenAIAstraSourceGroupIDs] = "[10]"
	svc.settingService.invalidateOpenAIAstraGroupCache()
	require.False(t, svc.OpenAIModelRouteAllowsAccount(normalCtx, normal, &source, "gpt-6-astra", PlatformOpenAI))
	accountRepo, ok := svc.accountRepo.(schedulerGroupAwareOpenAIAccountRepo)
	require.True(t, ok)
	accountRepo.accounts[1].GroupIDs = []int64{30}
	require.False(t, svc.OpenAIModelRouteAllowsAccount(ctx, special, &source, "gpt-6-astra", PlatformOpenAI), "must reload membership rather than trust the connection snapshot")
}

func TestOpenAIRouteSourcesValidationAndAtomicSettings(t *testing.T) {
	svc, repo, _ := astraRouteFixture(t, false)
	settings := svc.settingService
	settings.settingRepo = &astraRouteSettingsWriter{repo}
	settings.defaultSubGroupReader = solAndAstraGroupReader{groups: map[int64]*Group{
		10: {ID: 10, Platform: PlatformOpenAI, Status: StatusActive},
		11: {ID: 11, Platform: PlatformComposite, Status: StatusActive},
		12: {ID: 12, Platform: PlatformAnthropic, Status: StatusActive},
		13: {ID: 13, Platform: PlatformOpenAI, Status: StatusDisabled},
		20: {ID: 20, Platform: PlatformOpenAI, Status: StatusActive, SubscriptionType: SubscriptionTypeSpecial},
	}}
	ctx := context.Background()
	require.NoError(t, settings.UpdateSettings(ctx, &SystemSettings{OpenAIAstraGroupID: 20, OpenAIAstraSourceGroupIDs: []int64{11, 10, 11}}))
	require.Equal(t, "[10,11]", repo.values[SettingKeyOpenAIAstraSourceGroupIDs])
	cfg, err := settings.getOpenAIModelRouteConfig(ctx, false)
	require.NoError(t, err)
	require.Equal(t, []int64{10, 11}, cfg.sourceIDs)
	for _, invalid := range []int64{0, -1, 12, 13, 20, 999} {
		require.Error(t, settings.UpdateSettings(ctx, &SystemSettings{OpenAIAstraGroupID: 0, OpenAIAstraSourceGroupIDs: []int64{10, invalid}}))
		require.Equal(t, "20", repo.values[SettingKeyOpenAIAstraGroupID])
		require.Equal(t, "[10,11]", repo.values[SettingKeyOpenAIAstraSourceGroupIDs])
	}
	require.NoError(t, settings.UpdateSettingsOmitting(ctx, &SystemSettings{}, OmittedSettingKeys{
		SettingKeyOpenAIAstraGroupID: {}, SettingKeyOpenAIAstraSourceGroupIDs: {},
	}))
	require.Equal(t, "[10,11]", repo.values[SettingKeyOpenAIAstraSourceGroupIDs])
	require.NoError(t, settings.UpdateSettings(ctx, &SystemSettings{}))
	cfg, err = settings.getOpenAIModelRouteConfig(ctx, false)
	require.NoError(t, err)
	require.Empty(t, cfg.sourceIDs)
	require.Zero(t, cfg.targetID)
	require.Equal(t, "[]", repo.values[SettingKeyOpenAIAstraSourceGroupIDs])
}

func TestParseOpenAIRouteSourceIDs(t *testing.T) {
	for _, raw := range []string{"null", "{}", "[0]", "[-1]", "[1.5]", `["10"]`, "bad"} {
		_, err := parseOpenAIRouteSourceIDs(raw)
		require.Error(t, err, raw)
	}
	ids, err := parseOpenAIRouteSourceIDs("[30,10,30]")
	require.NoError(t, err)
	require.Equal(t, []int64{10, 30}, ids)
}

func TestOpenAIRouteSourcesLegacyStickyAndPreviousResponse(t *testing.T) {
	svc, repo, _ := astraRouteFixture(t, false)
	ctx := context.Background()
	source := int64(10)
	require.NoError(t, svc.setStickySessionAccountID(ctx, &source, "source-session", 1, time.Hour))
	selected, err := svc.SelectAccountForModel(ctx, &source, "source-session", "gpt-6-astra")
	require.NoError(t, err)
	require.EqualValues(t, 2, selected.ID)
	counted, err := svc.SelectAccountForTokenCount(ctx, &source, "", "gpt-6-astra", "", PlatformOpenAI)
	require.NoError(t, err)
	require.EqualValues(t, 2, counted.ID)
	store := svc.getOpenAIWSStateStore()
	require.NoError(t, store.BindResponseAccount(ctx, source, "resp_normal", 1, time.Hour))
	result, err := svc.SelectAccountByPreviousResponseID(ctx, &source, "resp_normal", "gpt-6-astra", nil, false)
	require.NoError(t, err)
	require.Nil(t, result)
	require.NoError(t, store.BindResponseAccount(ctx, source, "resp_special", 2, time.Hour))
	result, err = svc.SelectAccountByPreviousResponseID(ctx, &source, "resp_special", "gpt-6-astra", nil, false)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.EqualValues(t, 2, result.Account.ID)
	result.ReleaseFunc()
	repo.values[SettingKeyOpenAIAstraSourceGroupIDs] = "[]"
	svc.settingService.invalidateOpenAIAstraGroupCache()
	result, err = svc.SelectAccountByPreviousResponseID(ctx, &source, "resp_special", "gpt-6-astra", nil, false)
	require.NoError(t, err)
	require.Nil(t, result, "previous-response binding must not retain the special pool after deselection")
	selected, err = svc.SelectAccountForModel(ctx, &source, "source-session", "gpt-6-astra")
	require.NoError(t, err)
	require.EqualValues(t, 1, selected.ID)
}

func TestOpenAIRouteConfigCachesPairTogether(t *testing.T) {
	svc, repo, _ := astraRouteFixture(t, false)
	ctx := context.Background()
	first, err := svc.settingService.getOpenAIModelRouteConfig(ctx, false)
	require.NoError(t, err)
	require.EqualValues(t, 20, first.targetID)
	require.Equal(t, []int64{10}, first.sourceIDs)
	repo.values[SettingKeyOpenAIAstraGroupID] = "30"
	repo.values[SettingKeyOpenAIAstraSourceGroupIDs] = "[11]"
	cached, err := svc.settingService.getOpenAIModelRouteConfig(ctx, false)
	require.NoError(t, err)
	require.Equal(t, first, cached)
	svc.settingService.invalidateOpenAIAstraGroupCache()
	latest, err := svc.settingService.getOpenAIModelRouteConfig(ctx, false)
	require.NoError(t, err)
	require.EqualValues(t, 30, latest.targetID)
	require.Equal(t, []int64{11}, latest.sourceIDs)
}
