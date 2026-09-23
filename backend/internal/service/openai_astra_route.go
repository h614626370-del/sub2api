package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

type astraGroupSettingCache struct {
	mu      sync.Mutex
	id      int64
	expires time.Time
}

// Missing/zero disables routing. A settings read failure must never silently
// send an Astra request back to an ordinary account pool.
func (s *SettingService) GetOpenAIAstraGroupID(ctx context.Context) (int64, error) {
	if s == nil || s.settingRepo == nil {
		return 0, nil
	}
	c := &s.openAIAstraGroupCache
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Now().Before(c.expires) {
		return c.id, nil
	}
	dbCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	raw, err := s.settingRepo.GetValue(dbCtx, SettingKeyOpenAIAstraGroupID)
	if errors.Is(err, ErrSettingNotFound) {
		raw, err = "0", nil
	}
	if err != nil {
		return 0, err
	}
	if strings.TrimSpace(raw) == "" {
		raw = "0"
	}
	id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || id < 0 {
		return 0, errors.New("invalid Astra account group setting")
	}
	c.id, c.expires = id, time.Now().Add(5*time.Second)
	return id, nil
}

func (s *SettingService) invalidateOpenAIAstraGroupCache() {
	s.openAIAstraGroupCache.mu.Lock()
	s.openAIAstraGroupCache.expires = time.Time{}
	s.openAIAstraGroupCache.mu.Unlock()
}

func (s *SettingService) GetOpenAISolGroupID(ctx context.Context) (int64, error) {
	if s == nil || s.settingRepo == nil {
		return 0, nil
	}
	c := &s.openAISolGroupCache
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Now().Before(c.expires) {
		return c.id, nil
	}
	dbCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	raw, err := s.settingRepo.GetValue(dbCtx, SettingKeyOpenAISolGroupID)
	if errors.Is(err, ErrSettingNotFound) {
		raw, err = "0", nil
	}
	if err != nil {
		return 0, err
	}
	if strings.TrimSpace(raw) == "" {
		raw = "0"
	}
	id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || id < 0 {
		return 0, errors.New("invalid Sol account group setting")
	}
	c.id, c.expires = id, time.Now().Add(5*time.Second)
	return id, nil
}

func (s *SettingService) invalidateOpenAISolGroupCache() {
	s.openAISolGroupCache.mu.Lock()
	s.openAISolGroupCache.expires = time.Time{}
	s.openAISolGroupCache.mu.Unlock()
}

func (s *SettingService) validateOpenAISolGroup(ctx context.Context, id int64) error {
	if id == 0 {
		return nil
	}
	if id > 0 && s.defaultSubGroupReader != nil {
		dbCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		group, err := s.defaultSubGroupReader.GetByID(dbCtx, id)
		if err == nil && group != nil && group.IsSpecialType() && group.Platform == PlatformOpenAI && group.Status == StatusActive {
			return nil
		}
	}
	return infraerrors.BadRequest("INVALID_SOL_GROUP", "Sol 路由目标必须是启用的 OpenAI 特殊分组")
}

func (s *SettingService) validateOpenAIAstraGroup(ctx context.Context, id int64) error {
	if id == 0 {
		return nil
	}
	if id > 0 && s.defaultSubGroupReader != nil {
		dbCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		group, err := s.defaultSubGroupReader.GetByID(dbCtx, id)
		if err == nil && group != nil && group.IsSpecialType() && group.Platform == PlatformOpenAI && group.Status == StatusActive {
			return nil
		}
	}
	return infraerrors.BadRequest("INVALID_ASTRA_GROUP", "Astra 路由目标必须是启用的 OpenAI 特殊分组")
}

type openAIAstraRouteKey struct{}
type openAIAstraRoute struct {
	model   string
	groupID int64
	err     error
}

// WithOpenAIModelRoute changes only account-pool membership. The original API
// key/group remain intact for authorization, channel mapping, pricing and usage.
// Call with the client model before channel mapping, once per request/WS turn.
func (s *OpenAIGatewayService) WithOpenAIModelRoute(ctx context.Context, model, platform string) context.Context {
	canonical := canonicalizeOpenAIModelAliasSpelling(model)
	route := openAIAstraRoute{model: canonical}
	if NormalizeOpenAICompatiblePlatform(platform) == PlatformOpenAI && s != nil && s.settingService != nil {
		var validate func(context.Context, int64) error
		switch {
		case canonical == "gpt-6-astra" || canonical == "gpt-6" || strings.HasPrefix(canonical, "gpt-6-astra-"):
			route.groupID, route.err = s.settingService.GetOpenAIAstraGroupID(ctx)
			validate = s.settingService.validateOpenAIAstraGroup
		case isOpenAIGPT6SolModel(canonical):
			route.groupID, route.err = s.settingService.GetOpenAISolGroupID(ctx)
			validate = s.settingService.validateOpenAISolGroup
		}
		if route.err == nil && route.groupID > 0 {
			route.err = validate(ctx, route.groupID)
		}
	}
	return context.WithValue(ctx, openAIAstraRouteKey{}, route)
}

func (s *OpenAIGatewayService) ensureOpenAIModelRoute(ctx context.Context, model, platform string) (context.Context, error) {
	if _, ok := ctx.Value(openAIAstraRouteKey{}).(openAIAstraRoute); !ok {
		ctx = s.WithOpenAIModelRoute(ctx, model, platform)
	}
	route, _ := ctx.Value(openAIAstraRouteKey{}).(openAIAstraRoute)
	if route.err != nil {
		return ctx, fmt.Errorf("%w: Astra account pool unavailable", ErrNoAvailableAccounts)
	}
	return ctx, nil
}

func openAIModelRouteGroup(ctx context.Context) int64 {
	route, _ := ctx.Value(openAIAstraRouteKey{}).(openAIAstraRoute)
	return route.groupID
}

func openAIAstraSessionHash(ctx context.Context, hash string) string {
	if id := openAIModelRouteGroup(ctx); id > 0 && hash != "" {
		route, _ := ctx.Value(openAIAstraRouteKey{}).(openAIAstraRoute)
		return fmt.Sprintf("%s-group:%d:%s", route.model, id, hash)
	}
	return hash
}

func (s *OpenAIGatewayService) openAIAccountMatchesRequestGroup(ctx context.Context, account *Account, groupID *int64) bool {
	if id := openAIModelRouteGroup(ctx); id > 0 {
		return openAIStickyAccountMatchesGroup(account, &id)
	}
	return s.openAIAccountMatchesSchedulingGroup(account, groupID)
}

// WebSocket connections own a single upstream account. A model switch across
// pools must reconnect before sending the next turn, never leak to the old pool.
func (s *OpenAIGatewayService) OpenAIModelRouteAllowsAccount(ctx context.Context, account *Account, groupID *int64, model, platform string) bool {
	previousPool := openAIModelRouteGroup(ctx)
	ctx = s.WithOpenAIModelRoute(ctx, model, platform)
	if _, err := s.ensureOpenAIModelRoute(ctx, model, platform); err != nil {
		return false
	}
	if previousPool == 0 && openAIModelRouteGroup(ctx) == 0 {
		return true
	}
	return s.openAIAccountMatchesRequestGroup(ctx, account, groupID)
}
