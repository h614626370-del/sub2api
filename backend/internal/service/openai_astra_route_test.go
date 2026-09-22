package service

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

type astraRouteGroupReader struct{ group *Group }

type astraDiagnosisRepo struct {
	AccountRepository
	groupID int64
}

func (r *astraDiagnosisRepo) ListModelAvailabilityCandidates(_ context.Context, groupID *int64, platforms []string, includeGrouped bool) ([]Account, error) {
	if groupID != nil {
		r.groupID = *groupID
	}
	if includeGrouped {
		return nil, errors.New("must not widen explicit pool")
	}
	return []Account{{Credentials: map[string]any{"model_mapping": map[string]any{"gpt-6-astra": "gpt-6-astra"}}}}, nil
}

type astraRouteSettingsWriter struct{ *codexTicketSettingRepo }

func (r *astraRouteSettingsWriter) SetMultiple(_ context.Context, values map[string]string) error {
	for key, value := range values {
		r.values[key] = value
	}
	return nil
}
func (r *astraRouteSettingsWriter) GetAll(_ context.Context) (map[string]string, error) {
	return r.values, nil
}

func (r *astraRouteGroupReader) GetByID(_ context.Context, id int64) (*Group, error) {
	if r.group == nil || r.group.ID != id {
		return nil, errors.New("missing group")
	}
	return r.group, nil
}

func astraRouteFixture(t *testing.T, advanced bool) (*OpenAIGatewayService, *codexTicketSettingRepo, *astraRouteGroupReader) {
	t.Helper()
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
	repo := &codexTicketSettingRepo{codexPolicyMigrationRepoStub: &codexPolicyMigrationRepoStub{values: map[string]string{SettingKeyOpenAIAstraGroupID: "20", openAIAdvancedSchedulerSettingKey: strconv.FormatBool(advanced)}}}
	settings := NewSettingService(repo, &config.Config{})
	groups := &astraRouteGroupReader{group: &Group{ID: 20, Platform: PlatformOpenAI, Status: StatusActive, SubscriptionType: SubscriptionTypeSpecial, RateMultiplier: 100}}
	settings.SetDefaultSubscriptionGroupReader(groups)
	accounts := []Account{
		{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 2, GroupIDs: []int64{10}},
		{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 2, GroupIDs: []int64{20}},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, settingService: settings, rateLimitService: newOpenAIAdvancedSchedulerRateLimitService(strconv.FormatBool(advanced)), accountRepo: schedulerGroupAwareOpenAIAccountRepo{schedulerTestOpenAIAccountRepo{accounts: accounts}}, concurrencyService: NewConcurrencyService(stubConcurrencyCache{}), cache: &stubGatewayCache{}}
	return svc, repo, groups
}

func TestAstraRouteSelectsOnlyTargetPool(t *testing.T) {
	for _, advanced := range []bool{false, true} {
		t.Run(strconv.FormatBool(advanced), func(t *testing.T) {
			svc, _, _ := astraRouteFixture(t, advanced)
			require.Equal(t, advanced, svc.getOpenAIAccountScheduler(context.Background()) != nil)
			sourceID := int64(10)
			for _, tc := range []struct {
				model string
				id    int64
			}{{"gpt-6-astra", 2}, {"gpt-5.6-sol", 1}} {
				selection, _, err := svc.SelectAccountWithScheduler(context.Background(), &sourceID, "", "session", tc.model, nil, OpenAIUpstreamTransportAny, false)
				require.NoError(t, err)
				require.NotNil(t, selection)
				require.Equal(t, tc.id, selection.Account.ID)
				if selection.ReleaseFunc != nil {
					selection.ReleaseFunc()
				}
			}
			_, _, err := svc.SelectAccountWithScheduler(context.Background(), &sourceID, "", "session", "gpt-6-astra", map[int64]struct{}{2: {}}, OpenAIUpstreamTransportAny, false)
			require.ErrorIs(t, err, ErrNoAvailableAccounts, "must not fail over into source pool")
		})
	}
}

func TestAstraRouteClientModelAndSettingsFailures(t *testing.T) {
	svc, repo, groups := astraRouteFixture(t, false)
	sourceID := int64(10)
	for _, tc := range []struct {
		client, upstream string
		id               int64
	}{{"gpt-6-astra", "custom-upstream", 2}, {"ordinary-alias", "gpt-6-astra", 1}} {
		ctx := svc.WithOpenAIModelRoute(context.Background(), tc.client, PlatformOpenAI)
		selection, _, err := svc.SelectAccountWithScheduler(ctx, &sourceID, "", "", tc.upstream, nil, OpenAIUpstreamTransportAny, false)
		require.NoError(t, err)
		require.Equal(t, tc.id, selection.Account.ID)
		selection.ReleaseFunc()
	}
	for _, status := range []string{StatusDisabled, StatusActive} {
		groups.group.Status = status
		if status == StatusActive {
			groups.group.SubscriptionType = SubscriptionTypeStandard
		}
		_, _, err := svc.SelectAccountWithScheduler(context.Background(), &sourceID, "", "", "gpt-6-astra", nil, OpenAIUpstreamTransportAny, false)
		require.ErrorIs(t, err, ErrNoAvailableAccounts)
	}
	repo.err = errors.New("settings unavailable")
	svc.settingService.invalidateOpenAIAstraGroupCache()
	_, _, err := svc.SelectAccountWithScheduler(context.Background(), &sourceID, "", "", "gpt-6-astra", nil, OpenAIUpstreamTransportAny, false)
	require.ErrorIs(t, err, ErrNoAvailableAccounts)
	selection, _, err := svc.SelectAccountWithScheduler(context.Background(), &sourceID, "", "", "gpt-5.6-sol", nil, OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.EqualValues(t, 1, selection.Account.ID)
	selection.ReleaseFunc()
	repo.err = nil
	repo.values[SettingKeyOpenAIAstraGroupID] = "0"
	svc.settingService.invalidateOpenAIAstraGroupCache()
	selection, _, err = svc.SelectAccountWithScheduler(context.Background(), &sourceID, "", "", "gpt-6-astra", nil, OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.EqualValues(t, 1, selection.Account.ID)
	selection.ReleaseFunc()
}

func TestAstraRoutePreservesSourceProfitGateAndStickyIsolation(t *testing.T) {
	svc, _, _ := astraRouteFixture(t, false)
	source := profitControlTestGroup(10, 0.5, 0)
	ctx := svc.WithOpenAIModelRoute(profitControlTestCtx(source), "gpt-6-astra", PlatformOpenAI)
	require.Same(t, source, ctx.Value(ctxkey.Group))
	gate := svc.resolveOpenAIProfitControlGate(ctx, &source.ID)
	require.NotNil(t, gate)
	require.Equal(t, source.ID, gate.groupID)
	require.Equal(t, 0.5, gate.threshold)
	normal := context.Background()
	require.NoError(t, svc.setStickySessionAccountID(normal, &source.ID, "same-session", 1, time.Hour))
	require.NoError(t, svc.setStickySessionAccountID(ctx, &source.ID, "same-session", 2, time.Hour))
	id, err := svc.getStickySessionAccountID(normal, &source.ID, "same-session")
	require.NoError(t, err)
	require.EqualValues(t, 1, id)
	id, err = svc.getStickySessionAccountID(ctx, &source.ID, "same-session")
	require.NoError(t, err)
	require.EqualValues(t, 2, id)
	require.NoError(t, svc.deleteStickySessionAccountID(ctx, &source.ID, "same-session"))
	id, err = svc.getStickySessionAccountID(normal, &source.ID, "same-session")
	require.NoError(t, err)
	require.EqualValues(t, 1, id)
}

func TestAstraRouteWebSocketModelSwitchAndUserBinding(t *testing.T) {
	svc, _, _ := astraRouteFixture(t, false)
	sourceID := int64(10)
	normal, _ := svc.accountRepo.GetByID(context.Background(), 1)
	special, _ := svc.accountRepo.GetByID(context.Background(), 2)
	require.False(t, svc.OpenAIModelRouteAllowsAccount(context.Background(), normal, &sourceID, "gpt-6-astra", PlatformOpenAI))
	require.True(t, svc.OpenAIModelRouteAllowsAccount(context.Background(), special, &sourceID, "gpt-6-astra", PlatformOpenAI))
	astraCtx := svc.WithOpenAIModelRoute(context.Background(), "gpt-6-astra", PlatformOpenAI)
	require.False(t, svc.OpenAIModelRouteAllowsAccount(astraCtx, special, &sourceID, "gpt-5.6-sol", PlatformOpenAI))
	user := &User{AllowedGroups: []int64{20}}
	group := &Group{ID: 20, SubscriptionType: SubscriptionTypeSpecial}
	keySvc := &APIKeyService{}
	require.False(t, keySvc.canUserBindGroup(context.Background(), user, group))
	require.False(t, keySvc.canUserBindGroupInternal(user, group, map[int64]bool{20: true}))
}

func TestAstraRouteUsageChargedToSourceGroup(t *testing.T) {
	routeSvc, _, _ := astraRouteFixture(t, false)
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	billingRepo := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true}}
	svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
	source := &Group{ID: 10, Platform: PlatformOpenAI, Status: StatusActive, RateMultiplier: 2.5}
	key := &APIKey{ID: 100, GroupID: &source.ID, Group: source, Quota: 100}
	account, _ := routeSvc.accountRepo.GetByID(context.Background(), 2)
	ctx := routeSvc.WithOpenAIModelRoute(context.Background(), "gpt-6-astra", PlatformOpenAI)
	usage := OpenAIUsage{InputTokens: 1200, OutputTokens: 300}
	err := svc.RecordUsage(ctx, &OpenAIRecordUsageInput{Result: &OpenAIForwardResult{RequestID: "astra-route-billing", Usage: usage, Model: "gpt-6-astra", Duration: time.Second}, APIKey: key, User: &User{ID: 200}, Account: account, APIKeyService: &openAIRecordUsageAPIKeyQuotaStub{}})
	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)
	require.NotNil(t, billingRepo.lastCmd)
	require.Equal(t, &source.ID, usageRepo.lastLog.GroupID)
	require.Equal(t, 2.5, usageRepo.lastLog.RateMultiplier)
	require.EqualValues(t, 2, usageRepo.lastLog.AccountID)
	expected := expectedOpenAICost(t, svc, "gpt-6-astra", usage, 2.5)
	require.InDelta(t, expected.ActualCost, billingRepo.lastCmd.BalanceCost, 1e-10)
	require.Greater(t, billingRepo.lastCmd.BalanceCost, 0.0)
	require.Same(t, source, key.Group)
	require.EqualValues(t, 10, *key.GroupID)
}

func TestAstraRoutePreviousResponseCannotEscapePool(t *testing.T) {
	svc, _, _ := astraRouteFixture(t, true)
	sourceID := int64(10)
	ctx := svc.WithOpenAIModelRoute(context.Background(), "gpt-6-astra", PlatformOpenAI)
	store := svc.getOpenAIWSStateStore()
	require.NoError(t, store.BindResponseAccount(ctx, sourceID, "resp_old_normal", 1, time.Hour))
	selection, _, err := svc.SelectAccountWithScheduler(ctx, &sourceID, "resp_old_normal", "", "gpt-6-astra", nil, OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.EqualValues(t, 2, selection.Account.ID)
	selection.ReleaseFunc()
}

func TestAstraRouteTargetUnavailableAndTicketGate(t *testing.T) {
	for _, advanced := range []bool{false, true} {
		t.Run(strconv.FormatBool(advanced), func(t *testing.T) {
			svc, _, _ := astraRouteFixture(t, advanced)
			sourceID := int64(10)
			repo, ok := svc.accountRepo.(schedulerGroupAwareOpenAIAccountRepo)
			require.True(t, ok)
			target := &repo.accounts[1]
			target.Schedulable = false
			_, _, err := svc.SelectAccountWithScheduler(context.Background(), &sourceID, "", "", "gpt-6-astra", nil, OpenAIUpstreamTransportAny, false)
			require.ErrorIs(t, err, ErrNoAvailableAccounts)
			target.Schedulable = true
			target.Type = AccountTypeOAuth
			svc.cfg.Gateway.OpenAICodexTicket = config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}
			// Production services share the startup config; this fixture creates two.
			svc.settingService.cfg = svc.cfg
			svc.settingService.invalidateCodexTicketPolicy()
			_, _, err = svc.SelectAccountWithScheduler(context.Background(), &sourceID, "", "", "gpt-6-astra", nil, OpenAIUpstreamTransportAny, false)
			require.ErrorIs(t, err, ErrNoAvailableAccounts, "routing must not bypass ticket gating")
		})
	}
}

func TestAstraRouteSettingsValidationAndOmittedUpdates(t *testing.T) {
	svc, repo, groups := astraRouteFixture(t, false)
	settings := svc.settingService
	settings.settingRepo = &astraRouteSettingsWriter{repo}
	require.NoError(t, settings.UpdateSettings(context.Background(), &SystemSettings{OpenAIAstraGroupID: 20}))
	id, err := settings.GetOpenAIAstraGroupID(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 20, id)
	loaded, err := settings.GetAllSettings(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 20, loaded.OpenAIAstraGroupID)
	groups.group.Status = StatusDisabled
	require.Error(t, settings.UpdateSettings(context.Background(), &SystemSettings{OpenAIAstraGroupID: 20}))
	// A disabled/deleted target must not lock the entire settings screen.
	require.NoError(t, settings.UpdateSettingsOmitting(context.Background(), &SystemSettings{OpenAIAstraGroupID: 20}, OmittedSettingKeys{SettingKeyOpenAIAstraGroupID: {}}))
	require.Equal(t, "20", repo.values[SettingKeyOpenAIAstraGroupID])
	require.NoError(t, settings.UpdateSettings(context.Background(), &SystemSettings{}))
	id, err = settings.GetOpenAIAstraGroupID(context.Background())
	require.NoError(t, err)
	require.Zero(t, id)
	require.Error(t, settings.UpdateSettings(context.Background(), &SystemSettings{OpenAIAstraGroupID: -1}))
}

func TestAstraRouteFailureDiagnosisUsesSpecialPool(t *testing.T) {
	svc, repo, _ := astraRouteFixture(t, false)
	diagnosticRepo := &astraDiagnosisRepo{}
	svc.accountRepo = diagnosticRepo
	sourceID := int64(10)
	ctx := svc.WithOpenAIModelRoute(context.Background(), "gpt-6-astra", PlatformOpenAI)
	diagnosis := svc.DiagnoseModelAvailabilityForPlatform(ctx, &sourceID, "gpt-6-astra", PlatformOpenAI)
	require.EqualValues(t, 20, diagnosticRepo.groupID)
	require.True(t, diagnosis.HasModelSupport)
	repo.err = errors.New("unavailable")
	svc.settingService.invalidateOpenAIAstraGroupCache()
	diagnosticRepo.groupID = 0
	ctx = svc.WithOpenAIModelRoute(context.Background(), "gpt-6-astra", PlatformOpenAI)
	diagnosis = svc.DiagnoseModelAvailabilityForPlatform(ctx, &sourceID, "gpt-6-astra", PlatformOpenAI)
	require.Zero(t, diagnosticRepo.groupID)
	require.True(t, diagnosis.HasModelSupport, "settings outage must stay a temporary 503")
}
