package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type codexTicketDiscardRepo struct {
	AccountRepository
	account *Account
	err     error
	writes  int
}

func (r *codexTicketDiscardRepo) GetByID(context.Context, int64) (*Account, error) {
	return r.account, nil
}
func (r *codexTicketDiscardRepo) RemoveExtraKeys(context.Context, int64, []string) error {
	return r.err
}
func (r *codexTicketDiscardRepo) UpdateExtra(context.Context, int64, map[string]any) error {
	r.writes++
	return nil
}

func TestCodexTicketDiscardRejectsStaleSnapshotAndInflightResult(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, nil)
	account := ticketTestAccount(41)
	old := &openAICodexTicket{Model: "gpt-6-astra", State: fakeCodexTicketState(292), Length: 292,
		CapturedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour)}
	account.Extra = map[string]any{openAICodexTicketExtraKey(old.Model): old}
	repo := &codexTicketDiscardRepo{account: account}
	svc.accountRepo = repo
	require.NotNil(t, svc.lookupOpenAICodexTicket(account, old.Model))
	_, err := svc.DiscardOpenAICodexTickets(context.Background(), &account.ID)
	require.NoError(t, err)
	require.Nil(t, svc.lookupOpenAICodexTicket(account, old.Model))
	require.False(t, svc.storeOpenAICodexTicket(context.Background(), account, old))
	require.Zero(t, repo.writes)
	fresh := *old
	fresh.CapturedAt = svc.codexTicketAccountState(account.ID).revokedBefore.Add(time.Millisecond)
	require.True(t, svc.storeOpenAICodexTicket(context.Background(), account, &fresh))
	require.Equal(t, &fresh, svc.lookupOpenAICodexTicket(account, old.Model))
}

func TestCodexTicketDiscardWithoutExistingTicketRejectsInflightResult(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, nil)
	account := ticketTestAccount(41)
	svc.accountRepo = &codexTicketDiscardRepo{account: account}
	started := time.Now().Add(-time.Second)
	_, err := svc.DiscardOpenAICodexTickets(context.Background(), &account.ID)
	require.NoError(t, err)
	require.False(t, svc.storeOpenAICodexTicket(context.Background(), account, &openAICodexTicket{
		Model: "gpt-6-astra", CapturedAt: started,
	}))
}

func TestCodexTicketFailedDiscardPreservesTicket(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, nil)
	account := ticketTestAccount(41)
	ticket := &openAICodexTicket{Model: "gpt-6-astra", State: fakeCodexTicketState(292), Length: 292,
		CapturedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}
	require.True(t, svc.storeOpenAICodexTicket(context.Background(), account, ticket))
	svc.accountRepo = &codexTicketDiscardRepo{account: account, err: errors.New("database unavailable")}
	_, err := svc.DiscardOpenAICodexTickets(context.Background(), &account.ID)
	require.Error(t, err)
	require.NotNil(t, svc.lookupOpenAICodexTicket(account, ticket.Model))
}
