package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const accountTimezoneDetectedKey = "account_timezone_detected"
const accountTimezoneOverrideKey = "account_timezone_override"

// Detection stays separate from legacy overrides retained for rollback.
// The proxy fingerprint prevents stale cache reuse.
type accountTimezoneDetection struct {
	Timezone         string    `json:"timezone"`
	IP               string    `json:"ip"`
	ProxyFingerprint string    `json:"proxy_fingerprint"`
	DetectedAt       time.Time `json:"detected_at"`
}

type AccountTimezoneState struct {
	ProxyFingerprint string     `json:"-"`
	Timezone         string     `json:"timezone"`
	Override         string     `json:"override"`
	DetectedTimezone string     `json:"detected_timezone"`
	IP               string     `json:"ip"`
	DetectedAt       *time.Time `json:"detected_at,omitempty"`
	Source           string     `json:"source"`
	Stale            bool       `json:"stale"`
	HasProxy         bool       `json:"has_proxy"`
}

type AccountTimezoneManager interface {
	GetAccountTimezone(context.Context, int64) (*AccountTimezoneState, error)
	DetectAccountTimezone(context.Context, int64, bool) (*AccountTimezoneState, error)
	SetAccountTimezone(context.Context, int64, string) (*AccountTimezoneState, error)
}

type ProxyTimezoneProber interface {
	ProbeProxyTimezone(context.Context, string) (*ProxyExitInfo, error)
}

func validAccountTimezone(name string) bool {
	if name == "" || name == "Local" || len(name) > 128 {
		return false
	}
	_, err := time.LoadLocation(name)
	return err == nil
}

func timezoneProxyFingerprint(proxy *Proxy) string {
	if proxy == nil {
		return ""
	}
	sum := sha256.Sum256([]byte(proxy.URL()))
	return hex.EncodeToString(sum[:])
}

func accountTimezoneState(account *Account, proxy *Proxy) *AccountTimezoneState {
	state := &AccountTimezoneState{Source: "none", HasProxy: proxy != nil, ProxyFingerprint: timezoneProxyFingerprint(proxy)}
	var detected accountTimezoneDetection
	if raw, err := json.Marshal(account.Extra[accountTimezoneDetectedKey]); err == nil {
		_ = json.Unmarshal(raw, &detected)
	}
	if validAccountTimezone(detected.Timezone) {
		state.DetectedTimezone, state.IP = detected.Timezone, detected.IP
		state.DetectedAt = &detected.DetectedAt
		state.Stale = proxy == nil || detected.ProxyFingerprint != timezoneProxyFingerprint(proxy)
		if !state.Stale {
			state.Timezone, state.Source = detected.Timezone, "proxy"
		}
	}
	return state
}

// Only a result bound to the request's proxy may override the global default.
func resolveAccountTimezone(state *AccountTimezoneState, fingerprint, defaultZone string) (string, string) {
	if state != nil && state.Source == "proxy" && !state.Stale &&
		state.ProxyFingerprint == fingerprint && fingerprint != "" && validAccountTimezone(state.Timezone) {
		return state.Timezone, "proxy"
	}
	if validAccountTimezone(defaultZone) {
		return defaultZone, "global"
	}
	return "", "none"
}

func isAccountTimezoneEligible(account *Account) bool {
	return account != nil && account.IsOpenAI() && account.Type == AccountTypeOAuth
}

func (s *adminServiceImpl) timezoneAccount(ctx context.Context, id int64) (*Account, *Proxy, error) {
	account, err := s.accountRepo.GetByID(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if !isAccountTimezoneEligible(account) {
		return nil, nil, infraerrors.BadRequest("ACCOUNT_TIMEZONE_UNSUPPORTED", "Timezone settings are only available for OpenAI OAuth accounts")
	}
	if account.ProxyID == nil {
		return account, nil, nil
	}
	proxy, err := s.proxyRepo.GetByID(ctx, *account.ProxyID)
	return account, proxy, err
}

func (s *adminServiceImpl) GetAccountTimezone(ctx context.Context, id int64) (*AccountTimezoneState, error) {
	account, proxy, err := s.timezoneAccount(ctx, id)
	if err != nil {
		return nil, err
	}
	state := accountTimezoneState(account, proxy)
	state.Timezone, state.Source = resolveAccountTimezone(state, state.ProxyFingerprint, s.settingService.GetOpenAIOAuthDefaultTimezone(ctx))
	return state, nil
}

func (s *adminServiceImpl) SetAccountTimezone(_ context.Context, _ int64, _ string) (*AccountTimezoneState, error) {
	return nil, infraerrors.BadRequest("ACCOUNT_TIMEZONE_OVERRIDE_UNSUPPORTED", "Account timezone overrides are no longer supported; configure the global OAuth timezone instead")
}

func (s *adminServiceImpl) DetectAccountTimezone(ctx context.Context, id int64, refresh bool) (*AccountTimezoneState, error) {
	account, proxy, err := s.timezoneAccount(ctx, id)
	if err != nil {
		return nil, err
	}
	state := accountTimezoneState(account, proxy)
	if !refresh && state.Source == "proxy" {
		return state, nil
	}
	if proxy == nil {
		return nil, infraerrors.BadRequest("ACCOUNT_TIMEZONE_NO_PROXY", "No proxy is assigned; the global OAuth timezone applies when configured")
	}
	if !proxy.IsActive() || proxy.IsExpired(time.Now()) {
		return nil, infraerrors.BadRequest("ACCOUNT_TIMEZONE_PROXY_UNAVAILABLE", "The assigned proxy is inactive or expired")
	}
	prober, ok := s.proxyProber.(ProxyTimezoneProber)
	if !ok {
		return nil, infraerrors.BadRequest("ACCOUNT_TIMEZONE_UNAVAILABLE", "Timezone detection is unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	info, err := prober.ProbeProxyTimezone(ctx, proxy.URL())
	if err != nil || info == nil || !validAccountTimezone(info.Timezone) {
		return nil, infraerrors.BadRequest("ACCOUNT_TIMEZONE_DETECTION_FAILED", "Could not determine the proxy timezone; previous settings were retained")
	}
	// Recheck after network I/O. Never label a previous proxy's result as current.
	_, currentProxy, err := s.timezoneAccount(ctx, id)
	if err != nil {
		return nil, err
	}
	if timezoneProxyFingerprint(currentProxy) != timezoneProxyFingerprint(proxy) {
		return nil, infraerrors.Conflict("ACCOUNT_TIMEZONE_PROXY_CHANGED", "Proxy changed during detection; retry")
	}
	detected := accountTimezoneDetection{Timezone: info.Timezone, IP: info.IP, ProxyFingerprint: timezoneProxyFingerprint(proxy), DetectedAt: time.Now().UTC()}
	if err := s.accountRepo.UpdateExtra(ctx, id, map[string]any{accountTimezoneDetectedKey: detected}); err != nil {
		return nil, err
	}
	return s.GetAccountTimezone(ctx, id)
}

// Preserve managed timezone keys when a general account form saves an old
// snapshot. Only the dedicated timezone endpoints may modify these keys.
func MergeAccountTimezoneExtra(incoming, current map[string]any) map[string]any {
	if incoming == nil && current == nil {
		return nil
	}
	result := make(map[string]any, len(incoming)+2)
	for k, v := range incoming {
		result[k] = v
	}
	for _, key := range []string{accountTimezoneDetectedKey, accountTimezoneOverrideKey} {
		delete(result, key)
		if v, ok := current[key]; ok {
			result[key] = v
		}
	}
	return result
}
