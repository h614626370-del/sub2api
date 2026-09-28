package service

import (
	"context"
	"fmt"
	"time"
)

// Use the normal refresh lock and DB re-read, refreshing only the rejected
// credential. A concurrent successful refresh must never be rotated again.
type bpsRejectedTokenExecutor struct {
	OAuthRefreshExecutor
	rejected string
}

func (e bpsRejectedTokenExecutor) NeedsRefresh(account *Account, _ time.Duration) bool {
	return account.GetCredential("access_token") == e.rejected
}

func (s *OpenAIGatewayService) bpsOAuthCredentials(ctx context.Context, account *Account, rejected string) (*Account, error) {
	fresh := account
	if s.accountRepo != nil {
		var err error
		fresh, err = s.accountRepo.GetByID(ctx, account.ID)
		if err != nil {
			return nil, err
		}
	}
	if fresh == nil || !fresh.OpenAIBPSEnabled() {
		return nil, bpsExpired()
	}
	p := s.openAITokenProvider
	if rejected != "" && fresh.GetCredential("access_token") == rejected {
		if fresh.GetOpenAIRefreshToken() == "" {
			return nil, &bpsError{401, "bps_reauthorization_required", "BPS authentication failed; reauthorize the OpenAI OAuth account"}
		}
		if p == nil || p.refreshAPI == nil || p.executor == nil {
			return nil, fmt.Errorf("OAuth refresh provider is unavailable")
		}
		result, err := p.refreshAPI.RefreshIfNeeded(ctx, fresh, bpsRejectedTokenExecutor{p.executor, rejected}, 0)
		if err != nil {
			return nil, err
		}
		if result.Account != nil {
			fresh = result.Account
		}
		if result.LockHeld && p.tokenCache != nil {
			_, err := p.waitForTokenAfterLockRace(ctx, OpenAITokenCacheKey(fresh))
			if err != nil {
				return nil, err
			}
			if s.accountRepo != nil {
				fresh, err = s.accountRepo.GetByID(ctx, account.ID)
				if err != nil {
					return nil, err
				}
			}
		}
	}
	if fresh == nil || !fresh.OpenAIBPSEnabled() {
		return nil, bpsExpired()
	}
	if rejected != "" && p != nil && p.tokenCache != nil {
		if err := p.tokenCache.DeleteAccessToken(ctx, OpenAITokenCacheKey(fresh)); err != nil {
			return nil, err
		}
	}
	token := fresh.GetCredential("access_token")
	if p != nil {
		var err error
		token, err = p.GetAccessToken(ctx, fresh)
		if err != nil {
			return nil, err
		}
	}
	clone := *fresh
	clone.Credentials = shallowCopyMap(fresh.Credentials)
	clone.Credentials["access_token"] = token
	// Provider may return a just-refreshed cached token while this snapshot has
	// the preceding expiry. The provider, not that snapshot, validates expiry.
	if p != nil {
		delete(clone.Credentials, "expires_at")
	}
	return &clone, nil
}

func (s *OpenAIGatewayService) recordOpenAIBPSCredentialSuccess(ctx context.Context, account *Account) {
	if repo, ok := s.accountRepo.(OpenAIBPSCredentialStateRepository); ok {
		_, _ = repo.SetOpenAIBPSCredentialErrorIfMatch(ctx, account.ID,
			bpsRequestCredentialSnapshot(ctx, account),
			OpenAIBPSCredentialState{Status: "valid"})
	}
}
