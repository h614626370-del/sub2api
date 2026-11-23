package service

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

const (
	systemAutoUpdateKey     = "system_auto_update"
	systemAutoUpdateLastRun = "system_auto_update_last_run"
	systemAutoUpdateTime    = "04:00"
	systemAutoUpdatePoll    = 15 * time.Second
)

type SystemAutoUpdateSchedule struct {
	Enabled bool   `json:"enabled"`
	Time    string `json:"time"`
}

type SystemAutoUpdateSettings struct {
	SystemAutoUpdateSchedule
	Timezone  string `json:"timezone"`
	Supported bool   `json:"supported"`
}

type SystemAutoUpdate struct {
	settings  SettingRepository
	lock      *SystemOperationLockService
	install   func(context.Context) error
	supported bool
}

func ProvideSystemAutoUpdate(settings SettingRepository, lock *SystemOperationLockService, update *UpdateService, cfg *config.Config) *SystemAutoUpdate {
	return &SystemAutoUpdate{
		settings:  settings,
		lock:      lock,
		install:   update.performVerifiedUpdate,
		supported: cfg.RunsBackgroundJobs() && runtime.GOOS == "linux" && update.buildType == "release",
	}
}

func (s *SystemAutoUpdate) Get(ctx context.Context) (SystemAutoUpdateSettings, error) {
	result := SystemAutoUpdateSettings{
		SystemAutoUpdateSchedule: SystemAutoUpdateSchedule{Time: systemAutoUpdateTime},
		Timezone:                 timezone.Location().String(),
		Supported:                s.supported,
	}
	value, err := s.settings.GetValue(ctx, systemAutoUpdateKey)
	if errors.Is(err, ErrSettingNotFound) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	err = json.Unmarshal([]byte(value), &result.SystemAutoUpdateSchedule)
	return result, err
}

func (s *SystemAutoUpdate) Save(ctx context.Context, schedule SystemAutoUpdateSchedule) (SystemAutoUpdateSettings, error) {
	result := SystemAutoUpdateSettings{
		SystemAutoUpdateSchedule: schedule,
		Timezone:                 timezone.Location().String(),
		Supported:                s.supported,
	}
	parsed, err := time.Parse("15:04", schedule.Time)
	if err != nil || parsed.Format("15:04") != schedule.Time {
		return result, infraerrors.BadRequest("SYSTEM_AUTO_UPDATE_INVALID_TIME", "Enter a daily time in HH:MM format.")
	}
	if schedule.Enabled && !s.supported {
		return result, infraerrors.BadRequest("SYSTEM_AUTO_UPDATE_UNSUPPORTED", "Automatic updates require a Linux release running on the primary node.")
	}
	value, err := json.Marshal(schedule)
	if err != nil {
		return result, err
	}
	lock, err := s.lock.Acquire(ctx, "system-auto-update-settings")
	if err != nil {
		return result, err
	}
	err = s.settings.Set(ctx, systemAutoUpdateKey, string(value))
	s.release(lock, err)
	return result, err
}

func (s *SystemAutoUpdate) Run(ctx context.Context, restart func()) {
	if !s.supported {
		return
	}
	ticker := time.NewTicker(systemAutoUpdatePoll)
	defer ticker.Stop()
	for ctx.Err() == nil {
		installed, err := s.check(ctx, timezone.Now)
		if installed {
			logger.LegacyPrintf("service.system_auto_update", "Automatic update installed. Restarting service.")
			restart()
			return
		}
		if err != nil && !errors.Is(err, context.Canceled) {
			logger.LegacyPrintf("service.system_auto_update", "Automatic update check failed: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *SystemAutoUpdate) check(ctx context.Context, now func() time.Time) (installed bool, err error) {
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	settings, err := s.Get(checkCtx)
	if err != nil || !settings.Enabled || settings.Time != now().Format("15:04") {
		return false, err
	}
	lock, err := s.lock.Acquire(checkCtx, "system-auto-update")
	if errors.Is(err, ErrSystemOperationBusy) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer func() { s.release(lock, err) }()

	// Recheck the saved schedule after acquiring the shared system operation lock.
	settings, err = s.Get(checkCtx)
	current := now()
	if err != nil || !settings.Enabled || settings.Time != current.Format("15:04") {
		return false, err
	}
	day := current.Format(time.DateOnly)
	lastRun, err := s.settings.GetValue(checkCtx, systemAutoUpdateLastRun)
	if err != nil && !errors.Is(err, ErrSettingNotFound) {
		return false, err
	}
	if lastRun == day {
		return false, nil
	}
	// Record the attempt before installation so a restart or another process
	// cannot repeat the same daily update.
	if err := s.settings.Set(checkCtx, systemAutoUpdateLastRun, day); err != nil {
		return false, err
	}
	updateCtx, finish := context.WithTimeout(ctx, 15*time.Minute)
	defer finish()
	if err := s.install(updateCtx); err != nil {
		if errors.Is(err, ErrNoUpdateAvailable) || errors.Is(err, ErrSystemRestartRequired) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *SystemAutoUpdate) release(lock *SystemOperationLock, operationErr error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.lock.Release(ctx, lock, operationErr == nil, ""); err != nil {
		logger.LegacyPrintf("service.system_auto_update", "Could not release system operation lock: %v", err)
	}
}
