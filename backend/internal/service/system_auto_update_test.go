//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type autoUpdateSettingsTestRepo struct{ *mockSettingRepo }

func (r autoUpdateSettingsTestRepo) GetValue(ctx context.Context, key string) (string, error) {
	setting, err := r.Get(ctx, key)
	if err != nil {
		return "", err
	}
	return setting.Value, nil
}

func newTestAutoUpdate() *SystemAutoUpdate {
	return &SystemAutoUpdate{
		settings:  autoUpdateSettingsTestRepo{newMockSettingRepo()},
		lock:      NewSystemOperationLockService(newInMemoryIdempotencyRepo(), IdempotencyConfig{}),
		supported: true,
	}
}

func TestSystemAutoUpdateSchedule(t *testing.T) {
	ctx := context.Background()
	svc := newTestAutoUpdate()
	settings, err := svc.Get(ctx)
	require.NoError(t, err)
	require.False(t, settings.Enabled)
	require.Equal(t, "04:00", settings.Time)

	for _, value := range []string{"", "4:00", "24:00", "09:30:00", "09:60"} {
		_, err := svc.Save(ctx, SystemAutoUpdateSchedule{Enabled: true, Time: value})
		require.ErrorContains(t, err, "HH:MM")
	}
	schedule := SystemAutoUpdateSchedule{Enabled: true, Time: "03:15"}
	_, err = svc.Save(ctx, schedule)
	require.NoError(t, err)
	settings, err = svc.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, schedule, settings.SystemAutoUpdateSchedule)

	svc.supported = false
	_, err = svc.Save(ctx, schedule)
	require.ErrorContains(t, err, "Linux release")
	schedule.Enabled = false
	_, err = svc.Save(ctx, schedule)
	require.NoError(t, err)
}

func TestSystemAutoUpdateRunsOncePerLocalDay(t *testing.T) {
	ctx := context.Background()
	svc := newTestAutoUpdate()
	var calls int
	svc.install = func(context.Context) error {
		calls++
		return nil
	}
	local := time.FixedZone("CST", 8*60*60)
	current := time.Date(2026, 11, 23, 3, 14, 59, 0, local)
	now := func() time.Time { return current }
	_, err := svc.Save(ctx, SystemAutoUpdateSchedule{Enabled: true, Time: "03:15"})
	require.NoError(t, err)
	installed, err := svc.check(ctx, now)
	require.NoError(t, err)
	require.False(t, installed)

	current = current.Add(time.Second)
	manual, err := svc.lock.Acquire(ctx, "manual-rollback")
	require.NoError(t, err)
	installed, err = svc.check(ctx, now)
	require.NoError(t, err)
	require.False(t, installed)
	require.Zero(t, calls)
	require.NoError(t, svc.lock.Release(ctx, manual, true, ""))

	installed, err = svc.check(ctx, now)
	require.NoError(t, err)
	require.True(t, installed)
	restarted := *svc
	installed, err = restarted.check(ctx, now)
	require.NoError(t, err)
	require.False(t, installed)
	require.Equal(t, 1, calls)

	current = current.AddDate(0, 0, 1)
	installed, err = restarted.check(ctx, now)
	require.NoError(t, err)
	require.True(t, installed)
	require.Equal(t, 2, calls)
}

func TestSystemAutoUpdateDoesNotRestartAfterSkippedOrFailedInstall(t *testing.T) {
	for name, installErr := range map[string]error{
		"current":                ErrNoUpdateAvailable,
		"pending manual restart": ErrSystemRestartRequired,
		"download failed":        errors.New("download failed"),
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			now := func() time.Time { return time.Date(2026, 11, 23, 4, 0, 0, 0, time.UTC) }
			svc := newTestAutoUpdate()
			var calls int
			svc.install = func(context.Context) error {
				calls++
				return installErr
			}
			_, err := svc.Save(ctx, SystemAutoUpdateSchedule{Enabled: true, Time: "04:00"})
			require.NoError(t, err)
			installed, err := svc.check(ctx, now)
			require.False(t, installed)
			if name == "download failed" {
				require.ErrorIs(t, err, installErr)
			} else {
				require.NoError(t, err)
			}
			installed, err = svc.check(ctx, now)
			require.NoError(t, err)
			require.False(t, installed)
			require.Equal(t, 1, calls)
		})
	}
}
