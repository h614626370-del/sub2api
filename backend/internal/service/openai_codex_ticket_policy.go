package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const SettingKeyOpenAICodexTicketPolicy = "openai_codex_ticket_policy"

// Policy contains only non-secret runtime controls. Enabled and proxy remain separate settings.
type OpenAICodexTicketPolicy struct {
	ConnectionMaxAgeSeconds int      `json:"connection_max_age_seconds,omitempty"`
	ReuseConnection         bool     `json:"reuse_connection,omitempty"`
	TTLSeconds              int      `json:"ttl_seconds"`
	RefreshBeforeSeconds    int      `json:"refresh_before_seconds"`
	ProbeIntervalSeconds    int      `json:"probe_interval_seconds"`
	AttemptTimeoutSeconds   int      `json:"attempt_timeout_seconds"`
	TargetLength            int      `json:"target_length"`
	Models                  []string `json:"models"`
	FailClosed              bool     `json:"fail_closed"`
	CookieEnabled           bool     `json:"cookie_enabled"`
	CookieRequired          bool     `json:"cookie_required"`
	CookieTTLSeconds        int      `json:"cookie_ttl_seconds"`
}

func codexTicketPolicyFromConfig(c config.OpenAICodexTicketConfig) OpenAICodexTicketPolicy {
	p := OpenAICodexTicketPolicy{
		ConnectionMaxAgeSeconds: c.ConnectionMaxAgeSeconds,
		ReuseConnection:         c.ReuseConnection, TTLSeconds: c.TTLSeconds,
		RefreshBeforeSeconds: c.RefreshBeforeSeconds, ProbeIntervalSeconds: c.HarvestProbeIntervalSeconds,
		AttemptTimeoutSeconds: c.HarvestAttemptTimeoutSeconds, TargetLength: c.TargetLength,
		Models: append([]string(nil), c.Models...), FailClosed: c.FailClosed,
		CookieEnabled: c.CookieEnabled, CookieRequired: c.CookieRequired, CookieTTLSeconds: c.CookieTTLSeconds,
	}
	if p.TTLSeconds <= 0 {
		p.TTLSeconds = 3600
	}
	if p.RefreshBeforeSeconds <= 0 {
		p.RefreshBeforeSeconds = 600
	}
	if p.RefreshBeforeSeconds >= p.TTLSeconds {
		p.RefreshBeforeSeconds = p.TTLSeconds / 4
	}
	if p.ProbeIntervalSeconds <= 0 {
		p.ProbeIntervalSeconds = 6
	}
	if p.AttemptTimeoutSeconds <= 0 {
		p.AttemptTimeoutSeconds = 25
	}
	if p.TargetLength <= 0 {
		p.TargetLength = 292
	}
	if len(p.Models) == 0 {
		p.Models = []string{openAICodexTicketDefaultModel, openAICodexTicketDefaultSolModel}
	}
	if p.CookieTTLSeconds <= 0 {
		p.CookieTTLSeconds = 240
	}
	return p
}

func (p OpenAICodexTicketPolicy) Apply(c config.OpenAICodexTicketConfig) config.OpenAICodexTicketConfig {
	c.ReuseConnection = p.ReuseConnection
	c.ConnectionMaxAgeSeconds = p.ConnectionMaxAgeSeconds
	if c.ConnectionMaxAgeSeconds == 0 {
		c.ConnectionMaxAgeSeconds = 300
	}
	c.TTLSeconds, c.RefreshBeforeSeconds = p.TTLSeconds, p.RefreshBeforeSeconds
	c.HarvestProbeIntervalSeconds, c.HarvestAttemptTimeoutSeconds = p.ProbeIntervalSeconds, p.AttemptTimeoutSeconds
	c.TargetLength, c.Models, c.FailClosed = p.TargetLength, append([]string(nil), p.Models...), p.FailClosed
	c.CookieEnabled, c.CookieRequired, c.CookieTTLSeconds = p.CookieEnabled, p.CookieRequired, p.CookieTTLSeconds
	return c
}

func (p *OpenAICodexTicketPolicy) Validate() error {
	bad := func() error {
		return infraerrors.BadRequest("INVALID_CODEX_TICKET_POLICY", "Invalid ticket settings: TTL 10–86400s; refresh 0 <= refresh < TTL; interval 1–3600s; timeout 1–120s; length 1–8192; cookie TTL 10–86400s; 1–20 unique model names; required cookies need cookie capture enabled")
	}
	if p.ConnectionMaxAgeSeconds != 0 && (p.ConnectionMaxAgeSeconds < 30 || p.ConnectionMaxAgeSeconds > 3600) {
		return infraerrors.BadRequest("INVALID_CODEX_TICKET_POLICY", "Connection lifetime must be 30-3600 seconds")
	}
	if p.TTLSeconds < 10 || p.TTLSeconds > 86400 || p.RefreshBeforeSeconds < 0 || p.RefreshBeforeSeconds >= p.TTLSeconds || p.ProbeIntervalSeconds < 1 || p.ProbeIntervalSeconds > 3600 || p.AttemptTimeoutSeconds < 1 || p.AttemptTimeoutSeconds > 120 || p.TargetLength < 1 || p.TargetLength > 8192 || p.CookieTTLSeconds < 10 || p.CookieTTLSeconds > 86400 || ((p.CookieRequired || p.ReuseConnection) && !p.CookieEnabled) || len(p.Models) == 0 || len(p.Models) > 20 {
		return bad()
	}
	seen := map[string]bool{}
	for _, model := range p.Models {
		if model == "" || len(model) > 128 || strings.ContainsAny(model, " \t\r\n") || seen[model] {
			return bad()
		}
		seen[model] = true
	}
	return nil
}

type codexTicketPolicyCache struct {
	mu      sync.Mutex
	policy  *OpenAICodexTicketPolicy
	expires time.Time
}

func (s *SettingService) defaultCodexTicketPolicy() OpenAICodexTicketPolicy {
	c := config.OpenAICodexTicketConfig{FailClosed: true}
	if s != nil && s.cfg != nil {
		c = s.cfg.Gateway.OpenAICodexTicket
	}
	return codexTicketPolicyFromConfig(c)
}

func (s *SettingService) parseCodexTicketPolicy(raw string) OpenAICodexTicketPolicy {
	p := s.defaultCodexTicketPolicy()
	if strings.TrimSpace(raw) != "" {
		var candidate OpenAICodexTicketPolicy
		if json.Unmarshal([]byte(raw), &candidate) == nil && candidate.Validate() == nil {
			p = candidate
		}
	}
	return p
}

func (s *SettingService) GetOpenAICodexTicketPolicy(ctx context.Context) OpenAICodexTicketPolicy {
	c := &s.openAICodexTicketPolicyCache
	c.mu.Lock()
	defer c.mu.Unlock()
	clone := func(p OpenAICodexTicketPolicy) OpenAICodexTicketPolicy {
		p.Models = append([]string(nil), p.Models...)
		return p
	}
	if c.policy != nil && time.Now().Before(c.expires) {
		return clone(*c.policy)
	}
	if s.settingRepo == nil {
		return s.defaultCodexTicketPolicy()
	}
	dbCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	raw, err := s.settingRepo.GetValue(dbCtx, SettingKeyOpenAICodexTicketPolicy)
	if err != nil && !errors.Is(err, ErrSettingNotFound) {
		if c.policy != nil {
			return clone(*c.policy)
		}
		return s.defaultCodexTicketPolicy()
	}
	p := s.parseCodexTicketPolicy(raw)
	c.policy, c.expires = &p, time.Now().Add(5*time.Second)
	return clone(p)
}

func (s *SettingService) invalidateCodexTicketPolicy() {
	c := &s.openAICodexTicketPolicyCache
	c.mu.Lock()
	c.expires = time.Time{}
	c.mu.Unlock()
}
