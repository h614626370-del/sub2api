package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type timezoneSettingsRepo struct {
	*codexPolicyMigrationRepoStub
}

func (r *timezoneSettingsRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	result := make(map[string]string)
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			result[key] = value
		}
	}
	return result, nil
}

func TestOAuthTimezoneSettingCache(t *testing.T) {
	ctx := context.Background()
	repo := &codexPolicyMigrationRepoStub{values: map[string]string{}}
	s := NewSettingService(repo, nil)
	require.Empty(t, s.GetOpenAIOAuthDefaultTimezone(ctx))
	repo.values[SettingKeyOpenAIOAuthDefaultTimezone] = "Asia/Tokyo"
	require.Empty(t, s.GetOpenAIOAuthDefaultTimezone(ctx), "setting reads use the short-lived cache")
	s.openAIOAuthTimezoneCache.expires = time.Now().Add(-time.Second)
	require.Equal(t, "Asia/Tokyo", s.GetOpenAIOAuthDefaultTimezone(ctx))
	repo.values[SettingKeyOpenAIOAuthDefaultTimezone] = "Not/AZone"
	s.invalidateOpenAIOAuthTimezoneCache()
	require.Empty(t, s.GetOpenAIOAuthDefaultTimezone(ctx))
}
