package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

type astraGroupSettingCache struct {
	mu      sync.Mutex
	config  openAIModelRouteConfig
	expires time.Time
}

type openAIModelRouteConfig struct {
	targetID  int64
	sourceIDs []int64
	targetErr error
}

func parseOpenAIRouteSourceIDs(raw string) ([]int64, error) {
	ids := []int64{}
	if strings.TrimSpace(raw) == "" {
		return ids, nil
	}
	if err := json.Unmarshal([]byte(raw), &ids); err != nil || ids == nil {
		return nil, errors.New("invalid OpenAI route source groups")
	}
	for _, id := range ids {
		if id <= 0 {
			return nil, errors.New("OpenAI route source group IDs must be positive")
		}
	}
	slices.Sort(ids)
	return slices.Compact(ids), nil
}

// Read the source list and target in one snapshot. Target errors only apply to
// selected sources; unrelated requests must not depend on the dedicated pool.
func (s *SettingService) getOpenAIModelRouteConfig(ctx context.Context, sol bool) (openAIModelRouteConfig, error) {
	if s == nil || s.settingRepo == nil {
		return openAIModelRouteConfig{}, nil
	}
	c := &s.openAIAstraGroupCache
	targetKey, sourceKey := SettingKeyOpenAIAstraGroupID, SettingKeyOpenAIAstraSourceGroupIDs
	if sol {
		c = &s.openAISolGroupCache
		targetKey, sourceKey = SettingKeyOpenAISolGroupID, SettingKeyOpenAISolSourceGroupIDs
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Now().Before(c.expires) {
		return c.config, nil
	}
	dbCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	values, err := s.settingRepo.GetMultiple(dbCtx, []string{targetKey, sourceKey})
	if err != nil {
		return openAIModelRouteConfig{}, err
	}
	ids, err := parseOpenAIRouteSourceIDs(values[sourceKey])
	if err != nil {
		return openAIModelRouteConfig{}, err
	}
	raw := strings.TrimSpace(values[targetKey])
	if raw == "" {
		raw = "0"
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	cfg := openAIModelRouteConfig{targetID: id, sourceIDs: ids}
	if err != nil || id < 0 {
		cfg.targetErr = errors.New("invalid OpenAI route target group")
	}
	c.config, c.expires = cfg, time.Now().Add(5*time.Second)
	return cfg, nil
}

func (s *SettingService) GetOpenAIAstraGroupID(ctx context.Context) (int64, error) {
	cfg, err := s.getOpenAIModelRouteConfig(ctx, false)
	if err != nil {
		return 0, err
	}
	return cfg.targetID, cfg.targetErr
}

func (s *SettingService) invalidateOpenAIAstraGroupCache() {
	s.openAIAstraGroupCache.mu.Lock()
	s.openAIAstraGroupCache.expires = time.Time{}
	s.openAIAstraGroupCache.mu.Unlock()
}

func (s *SettingService) GetOpenAISolGroupID(ctx context.Context) (int64, error) {
	cfg, err := s.getOpenAIModelRouteConfig(ctx, true)
	if err != nil {
		return 0, err
	}
	return cfg.targetID, cfg.targetErr
}

func (s *SettingService) normalizeOpenAIRouteSources(ctx context.Context, ids []int64) ([]int64, error) {
	normalized := append([]int64{}, ids...)
	slices.Sort(normalized)
	normalized = slices.Compact(normalized)
	for _, id := range normalized {
		if id > 0 && s.defaultSubGroupReader != nil {
			group, err := s.defaultSubGroupReader.GetByID(ctx, id)
			if err == nil && group != nil && group.Status == StatusActive && !group.IsSpecialType() &&
				(group.Platform == PlatformOpenAI || group.Platform == PlatformComposite) {
				continue
			}
		}
		return nil, infraerrors.BadRequest("INVALID_OPENAI_ROUTE_SOURCE", fmt.Sprintf("路由适用分组 #%d 必须是启用的 OpenAI 或合成分组，且不能是专用分组", id))
	}
	return normalized, nil
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
func (s *OpenAIGatewayService) WithOpenAIModelRoute(ctx context.Context, sourceGroupID *int64, model, platform string) context.Context {
	canonical := canonicalizeOpenAIModelAliasSpelling(model)
	route := openAIAstraRoute{model: canonical}
	if resolved, ok := ResolvedTargetPlatformFromContext(ctx); ok {
		platform = resolved
	}
	if sourceGroupID != nil && *sourceGroupID > 0 && platform == PlatformOpenAI && s != nil && s.settingService != nil {
		var validate func(context.Context, int64) error
		var cfg openAIModelRouteConfig
		switch {
		case canonical == "gpt-6-astra" || canonical == "gpt-6" || strings.HasPrefix(canonical, "gpt-6-astra-"):
			cfg, route.err = s.settingService.getOpenAIModelRouteConfig(ctx, false)
			validate = s.settingService.validateOpenAIAstraGroup
		case isOpenAIGPT6SolModel(canonical):
			cfg, route.err = s.settingService.getOpenAIModelRouteConfig(ctx, true)
			validate = s.settingService.validateOpenAISolGroup
		}
		if route.err == nil && slices.Contains(cfg.sourceIDs, *sourceGroupID) {
			route.groupID, route.err = cfg.targetID, cfg.targetErr
		}
		if route.err == nil && route.groupID > 0 {
			route.err = validate(ctx, route.groupID)
		}
	}
	return context.WithValue(ctx, openAIAstraRouteKey{}, route)
}

func (s *OpenAIGatewayService) ensureOpenAIModelRoute(ctx context.Context, sourceGroupID *int64, model, platform string) (context.Context, error) {
	if _, ok := ctx.Value(openAIAstraRouteKey{}).(openAIAstraRoute); !ok {
		ctx = s.WithOpenAIModelRoute(ctx, sourceGroupID, model, platform)
	}
	route, _ := ctx.Value(openAIAstraRouteKey{}).(openAIAstraRoute)
	if route.err != nil {
		return ctx, fmt.Errorf("%w: OpenAI route account pool unavailable", ErrNoAvailableAccounts)
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
	ctx = s.WithOpenAIModelRoute(ctx, groupID, model, platform)
	if _, err := s.ensureOpenAIModelRoute(ctx, groupID, model, platform); err != nil {
		return false
	}
	if previousPool == 0 && openAIModelRouteGroup(ctx) == 0 {
		return true
	}
	if account == nil || s.accountRepo == nil {
		return false
	}
	latest, err := s.accountRepo.GetByID(ctx, account.ID)
	if err != nil || latest == nil {
		return false
	}
	return s.openAIAccountMatchesRequestGroup(ctx, latest, groupID)
}
