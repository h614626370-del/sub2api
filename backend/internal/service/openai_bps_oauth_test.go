//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type bps401Upstream struct {
	HTTPUpstream
	tokens []string
}

type bpsConcurrentCredentialRepo struct {
	AccountRepository
	mu      sync.Mutex
	account *Account
}

func (r *bpsConcurrentCredentialRepo) GetByID(context.Context, int64) (*Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	copy := *r.account
	copy.Credentials = shallowCopyMap(r.account.Credentials)
	return &copy, nil
}

func (r *bpsConcurrentCredentialRepo) UpdateCredentials(_ context.Context, _ int64, credentials map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.account.Credentials = shallowCopyMap(credentials)
	return nil
}

func TestOpenAIBPSConcurrentRejectionsShareRefreshLock(t *testing.T) {
	a := bpsModeAccount()
	require.NoError(t, NormalizeOpenAIBPSMode(a, nil))
	a.Credentials["expires_at"] = time.Now().Add(time.Hour).Format(time.RFC3339)
	repo := &bpsConcurrentCredentialRepo{account: a}
	credentials := shallowCopyMap(a.Credentials)
	credentials["access_token"] = "concurrent-refreshed-token"
	executor := &refreshAPIExecutorStub{credentials: credentials, delay: 10 * time.Millisecond}
	provider := NewOpenAITokenProvider(repo, nil, nil)
	provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, nil), executor)
	s := &OpenAIGatewayService{accountRepo: repo, openAITokenProvider: provider}
	var wg sync.WaitGroup
	errors := make([]error, 8)
	tokens := make([]string, 8)
	for i := range errors {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			fresh, err := s.bpsOAuthCredentials(context.Background(), &Account{ID: a.ID}, "test-token")
			errors[i] = err
			if fresh != nil {
				tokens[i] = fresh.GetCredential("access_token")
			}
		}(i)
	}
	wg.Wait()
	for i, err := range errors {
		require.NoError(t, err)
		require.Equal(t, "concurrent-refreshed-token", tokens[i])
	}
	require.Equal(t, 1, executor.refreshCalls)
}

func (u *bps401Upstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.tokens = append(u.tokens, req.Header.Get("Authorization"))
	return &http.Response{StatusCode: 401, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"invalid_token"}}`))}, nil
}

func TestOpenAIBPSOAuth401RetriesOnceWithSharedProvider(t *testing.T) {
	s, _ := bpsFixture()
	a := bpsModeAccount()
	require.NoError(t, NormalizeOpenAIBPSMode(a, nil))
	a.Credentials["expires_at"] = time.Now().Add(time.Hour).Format(time.RFC3339)
	repo := &refreshAPIAccountRepo{account: a}
	creds := shallowCopyMap(a.Credentials)
	creds["access_token"] = "refreshed-test-token"
	executor := &refreshAPIExecutorStub{credentials: creds}
	provider := NewOpenAITokenProvider(repo, nil, nil)
	provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, nil), executor)
	s.accountRepo, s.openAITokenProvider = repo, provider
	upstream := &bps401Upstream{}
	s.httpUpstream = upstream
	c, rec := bpsContext(1, "/v1/responses")
	_, err := s.forwardOpenAIBPS(c.Request.Context(), c, a, bpsRequestBody("hi", false))
	require.Error(t, err)
	require.Equal(t, 401, rec.Code)
	require.Equal(t, []string{"Bearer test-token", "Bearer refreshed-test-token"}, upstream.tokens)
	require.Equal(t, 1, executor.refreshCalls)
	require.Equal(t, StatusActive, repo.account.Status)
	// A second rejection of the old token must reuse the rotated credential.
	fresh, err := s.bpsOAuthCredentials(context.Background(), a, "test-token")
	require.NoError(t, err)
	require.Equal(t, "refreshed-test-token", fresh.GetCredential("access_token"))
	require.Equal(t, 1, executor.refreshCalls)
}

func TestOpenAIBPSOAuth401WithoutRefreshRequiresReauthorization(t *testing.T) {
	s, _ := bpsFixture()
	a := bpsModeAccount()
	delete(a.Credentials, "refresh_token")
	require.NoError(t, NormalizeOpenAIBPSMode(a, nil))
	s.accountRepo = &refreshAPIAccountRepo{account: a}
	upstream := &bps401Upstream{}
	s.httpUpstream = upstream
	c, rec := bpsContext(1, "/v1/responses")
	_, err := s.forwardOpenAIBPS(c.Request.Context(), c, a, bpsRequestBody("hi", false))
	require.Error(t, err)
	require.Contains(t, rec.Body.String(), "bps_reauthorization_required")
	require.Len(t, upstream.tokens, 1)
	require.Equal(t, StatusActive, a.Status)
}
