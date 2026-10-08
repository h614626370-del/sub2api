package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAccount_IsOpenAICacheCreationAsInputEnabled(t *testing.T) {
	require.False(t, (*Account)(nil).IsOpenAICacheCreationAsInputEnabled())
	for _, platform := range []string{PlatformOpenAI, PlatformAnthropic, PlatformGemini} {
		for _, accountType := range []string{AccountTypeOAuth, AccountTypeAPIKey, AccountTypeSetupToken} {
			for _, value := range []any{nil, false, true, "true", 1} {
				t.Run(fmt.Sprintf("%s/%s/%v", platform, accountType, value), func(t *testing.T) {
					account := &Account{Platform: platform, Type: accountType}
					require.False(t, account.IsOpenAICacheCreationAsInputEnabled())
					account.Extra = map[string]any{"openai_cache_creation_as_input": value}
					require.Equal(t, platform == PlatformOpenAI && value == true, account.IsOpenAICacheCreationAsInputEnabled())
					require.False(t, account.IsExcelBPSEnabled())
				})
			}
		}
	}
}

func TestUpdateAccountOpenAICacheCreationAsInput(t *testing.T) {
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeAPIKey, AccountTypeSetupToken} {
		t.Run(accountType, func(t *testing.T) {
			account := &Account{ID: 51, Platform: PlatformOpenAI, Type: accountType,
				Extra: map[string]any{"unrelated": "keep"}}
			repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{account.ID: account}}
			svc := &adminServiceImpl{accountRepo: repo}
			for _, enabled := range []bool{true, false} {
				extra := map[string]any{"unrelated": "keep"}
				if enabled {
					extra["openai_cache_creation_as_input"] = true
				}
				updated, err := svc.UpdateAccount(context.Background(), account.ID, &UpdateAccountInput{Extra: extra})
				require.NoError(t, err)
				require.Equal(t, enabled, updated.IsOpenAICacheCreationAsInputEnabled())
				require.Equal(t, enabled, repo.accounts[account.ID].IsOpenAICacheCreationAsInputEnabled())
				require.False(t, updated.IsExcelBPSEnabled())
				require.Equal(t, "keep", updated.Extra["unrelated"])
			}
		})
	}
}

func TestOpenAICacheCreationAsInputBilling(t *testing.T) {
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeAPIKey, AccountTypeSetupToken} {
		for _, endpoint := range []string{"/v1/responses", "/v1/chat/completions", "/basispoints/api/responses"} {
			for _, transport := range []string{"non-stream", "sse", "websocket"} {
				for _, subscription := range []bool{false, true} {
					for _, tt := range []struct {
						name                    string
						option                  any
						input, creation, read   int
						wantInput, wantCreation int
					}{
						{"default", nil, 1000, 200, 100, 700, 200},
						{"disabled", false, 1000, 200, 100, 700, 200},
						{"enabled", true, 1000, 200, 100, 900, 0},
						{"invalid", "true", 1000, 200, 100, 700, 200},
						{"no creation", true, 1000, 0, 100, 900, 0},
						{"all cached", true, 1000, 800, 200, 800, 0},
						{"empty input", true, 0, 0, 0, 0, 0},
					} {
						t.Run(fmt.Sprintf("%s/%s/%s/subscription=%t/%s", accountType, endpoint, transport, subscription, tt.name), func(t *testing.T) {
							usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
							billingRepo := &openAIRecordUsageBillingRepoStub{}
							svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo,
								&openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
							svc.billingService = NewBillingService(svc.cfg, &PricingService{pricingData: map[string]*LiteLLMModelPricing{
								"gpt-6-sol": {
									InputCostPerToken: 5e-6, OutputCostPerToken: 30e-6,
									CacheCreationInputTokenCost: 6.25e-6, CacheCreationInputTokenCostExplicit: true,
									CacheReadInputTokenCost: 0.5e-6,
								},
							}})
							account := &Account{ID: 3001, Platform: PlatformOpenAI, Type: accountType,
								Extra: map[string]any{"openai_cache_creation_as_input": tt.option}}
							require.False(t, account.IsExcelBPSEnabled())
							original := OpenAIUsage{InputTokens: tt.input, CacheCreationInputTokens: tt.creation,
								CacheReadInputTokens: tt.read, OutputTokens: 50}
							result := &OpenAIForwardResult{RequestID: "resp_cache_input", Model: "gpt-6-sol",
								UpstreamEndpoint: endpoint, Usage: original, Stream: transport != "non-stream",
								OpenAIWSMode: transport == "websocket", Duration: time.Second}
							input := &OpenAIRecordUsageInput{Result: result, Account: account,
								APIKey: &APIKey{ID: 1001}, User: &User{ID: 2001}}
							if subscription {
								input.Subscription = &UserSubscription{ID: 4001}
								input.APIKey.Group = &Group{SubscriptionType: "subscription"}
							}
							require.NoError(t, svc.RecordUsage(context.Background(), input))
							require.Equal(t, original, result.Usage, "retain the upstream usage")
							log := usageRepo.lastLog
							require.NotNil(t, log)
							require.Equal(t, tt.wantInput, log.InputTokens)
							require.Equal(t, tt.wantCreation, log.CacheCreationTokens)
							require.Equal(t, tt.read, log.CacheReadTokens)
							require.Equal(t, 50, log.OutputTokens)
							require.Equal(t, tt.input+50, log.TotalTokens())
							wantInputCost := float64(tt.wantInput) * 5e-6
							wantCreationCost := float64(tt.wantCreation) * 6.25e-6
							wantReadCost := float64(tt.read) * 0.5e-6
							wantTotal := wantInputCost + wantCreationCost + wantReadCost + 50*30e-6
							require.InDelta(t, wantInputCost, log.InputCost, 1e-12)
							require.InDelta(t, wantCreationCost, log.CacheCreationCost, 1e-12)
							require.InDelta(t, wantReadCost, log.CacheReadCost, 1e-12)
							require.InDelta(t, wantTotal, log.TotalCost, 1e-12)
							require.InDelta(t, wantTotal*1.1, log.ActualCost, 1e-12)
							require.Equal(t, 1, billingRepo.calls)
							cmd := billingRepo.lastCmd
							require.Equal(t, tt.wantInput, cmd.InputTokens)
							require.Equal(t, tt.wantCreation, cmd.CacheCreationTokens)
							require.Equal(t, tt.read, cmd.CacheReadTokens)
							if subscription {
								require.Zero(t, cmd.BalanceCost)
								require.InDelta(t, wantTotal*1.1, cmd.SubscriptionCost, 1e-12)
							} else {
								require.Zero(t, cmd.SubscriptionCost)
								require.InDelta(t, wantTotal*1.1, cmd.BalanceCost, 1e-12)
							}
						})
					}
				}
			}
		}
	}
}

func TestOpenAICacheCreationAsInputBillingAccountIsolation(t *testing.T) {
	for _, tt := range []struct {
		name          string
		platform      string
		parentEnabled bool
		enabled       bool
		wantCreation  int
	}{
		{"shadow opts in", PlatformOpenAI, false, true, 0},
		{"shadow does not inherit parent billing", PlatformOpenAI, true, false, 200},
		{"other platform ignores stale setting", PlatformGemini, false, true, 200},
	} {
		t.Run(tt.name, func(t *testing.T) {
			usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
			svc := newOpenAIRecordUsageServiceForTest(usageRepo,
				&openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
			parent := &Account{ID: 9001, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
				Extra: map[string]any{"openai_cache_creation_as_input": tt.parentEnabled}}
			svc.accountRepo = &openAIRecordUsageAccountRepoStub{account: parent}
			account := &Account{ID: 3001, Platform: tt.platform, Type: AccountTypeOAuth,
				Extra: map[string]any{"openai_cache_creation_as_input": tt.enabled}}
			if tt.platform == PlatformOpenAI {
				account.ParentAccountID = &parent.ID
			}
			require.NoError(t, svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
				Result: &OpenAIForwardResult{RequestID: "resp_cache_isolation", Model: "gpt-5.6-sol",
					Usage: OpenAIUsage{InputTokens: 1000, CacheCreationInputTokens: 200, CacheReadInputTokens: 100, OutputTokens: 50}},
				Account: account, APIKey: &APIKey{ID: 1001}, User: &User{ID: 2001},
			}))
			require.NotNil(t, usageRepo.lastLog)
			require.Equal(t, tt.wantCreation, usageRepo.lastLog.CacheCreationTokens)
			require.Equal(t, 900-tt.wantCreation, usageRepo.lastLog.InputTokens)
			require.Equal(t, 1050, usageRepo.lastLog.TotalTokens())
		})
	}
}
