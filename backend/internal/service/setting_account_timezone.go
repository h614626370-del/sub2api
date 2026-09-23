package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

type oauthTimezoneSettingCache struct {
	mu      sync.Mutex
	zone    string
	expires time.Time
}

func (s *SettingService) GetOpenAIOAuthDefaultTimezone(ctx context.Context) string {
	if s == nil || s.settingRepo == nil {
		return ""
	}
	c := &s.openAIOAuthTimezoneCache
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Now().Before(c.expires) {
		return c.zone
	}
	dbCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	raw, err := s.settingRepo.GetValue(dbCtx, SettingKeyOpenAIOAuthDefaultTimezone)
	if err != nil && !errors.Is(err, ErrSettingNotFound) {
		return c.zone
	}
	zone := strings.TrimSpace(raw)
	if !validAccountTimezone(zone) {
		zone = ""
	}
	c.zone, c.expires = zone, time.Now().Add(5*time.Second)
	return zone
}

func (s *SettingService) invalidateOpenAIOAuthTimezoneCache() {
	s.openAIOAuthTimezoneCache.mu.Lock()
	s.openAIOAuthTimezoneCache.expires = time.Time{}
	s.openAIOAuthTimezoneCache.mu.Unlock()
}
