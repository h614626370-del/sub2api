package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

type openAICodexTicketProbeResult struct {
	ConnectionClosed bool
	State            string
	Cookies          []openAICodexTicketCookie
	Status           int
	RequestBody      string
	ResponseHeaders  map[string]string
	StartedAt        time.Time
	FinishedAt       time.Time
	Reason           string
	EgressIP         string
}

type codexTicketAccountState struct {
	mu            sync.Mutex
	revokedBefore time.Time
}

func (s *OpenAIGatewayService) codexTicketAccountState(id int64) *codexTicketAccountState {
	state, _ := s.openaiCodexTicketAccounts.LoadOrStore(id, &codexTicketAccountState{})
	return state.(*codexTicketAccountState)
}

const (
	openAICodexTicketExtraKeyPrefix  = "codex_turn_ticket:"
	openAICodexAstraMinVersion       = "0.153.4"
	openAICodexTicketStatePrefix     = "gAAAAA"
	openAICodexTicketDefaultModel    = "gpt-6-astra"
	openAICodexTicketDefaultSolModel = "gpt-5.6-sol"
)

// ErrOpenAICodexTicketUnavailable 表示该号该模型没有可用的 292 门票，
// 且 fail_closed 禁止裸打业务请求。
var ErrOpenAICodexTicketUnavailable = errors.New("codex turn-state ticket unavailable")

type openAICodexTicket struct {
	EgressIP       string                    `json:"egress_ip,omitempty"`
	EgressIPSource string                    `json:"egress_ip_source,omitempty"`
	DurationMS     int                       `json:"duration_ms,omitempty"`
	Cookies        []openAICodexTicketCookie `json:"cookies,omitempty"`
	AccountID      int64                     `json:"account_id"`
	Model          string                    `json:"model"`
	State          string                    `json:"state"`
	Length         int                       `json:"length"`
	CapturedAt     time.Time                 `json:"captured_at"`
	ExpiresAt      time.Time                 `json:"expires_at"`
	Attempts       int                       `json:"attempts"`
}

// OpenAICodexTicketDiscardResult summarizes an administrative ticket reset.
type OpenAICodexTicketDiscardResult struct {
	ClearedAccounts int `json:"cleared_accounts"`
	ClearedTickets  int `json:"cleared_tickets"`
}

// openAICodexTicketExtraRemover is implemented by the production account
// repository. It is kept optional so lightweight service test repositories do
// not need to grow a persistence-only method.
type openAICodexTicketExtraRemover interface {
	RemoveExtraKeys(ctx context.Context, id int64, keys []string) error
}

func openAICodexTicketKey(accountID int64, model string) string {
	return fmt.Sprintf("%d\x00%s", accountID, strings.TrimSpace(model))
}

func openAICodexTicketExtraKey(model string) string {
	return openAICodexTicketExtraKeyPrefix + strings.TrimSpace(model)
}

func normalizeOpenAICodexTicketModel(model string) string {
	return strings.TrimSpace(model)
}

func extractOpenAICodexTicketModel(body []byte) string {
	return normalizeOpenAICodexTicketModel(gjson.GetBytes(body, "model").String())
}

func (s *OpenAIGatewayService) openAICodexTicketConfig() config.OpenAICodexTicketConfig {
	cfg := config.OpenAICodexTicketConfig{}
	if s != nil && s.cfg != nil {
		cfg = s.cfg.Gateway.OpenAICodexTicket
	}
	if cfg.TargetLength <= 0 {
		cfg.TargetLength = 292
	}
	if cfg.TTLSeconds <= 0 {
		cfg.TTLSeconds = 3600
	}
	if cfg.RefreshBeforeSeconds <= 0 {
		cfg.RefreshBeforeSeconds = 600
	}
	if cfg.HarvestProbeIntervalSeconds <= 0 {
		cfg.HarvestProbeIntervalSeconds = 6
	}
	if cfg.HarvestAttemptTimeoutSeconds <= 0 {
		cfg.HarvestAttemptTimeoutSeconds = 25
	}
	if len(cfg.Models) == 0 {
		cfg.Models = []string{openAICodexTicketDefaultModel, openAICodexTicketDefaultSolModel}
	}
	if s != nil && s.settingService != nil {
		return s.settingService.GetOpenAICodexTicketPolicy(context.Background()).Apply(cfg)
	}
	return codexTicketPolicyFromConfig(cfg).Apply(cfg)
}

func (s *OpenAIGatewayService) openAICodexTicketGatedModel(model string) bool {
	model = normalizeOpenAICodexTicketModel(model)
	if model == "" || !s.openAICodexTicketEnabled() {
		return false
	}
	for _, item := range s.openAICodexTicketConfig().Models {
		if normalizeOpenAICodexTicketModel(item) == model {
			return true
		}
	}
	return false
}

// OpenAICodexTicketStatus 是给管理端看的门票摘要，不含 state blob。
type OpenAICodexTicketStatus struct {
	LastSuccessAt          *time.Time `json:"last_success_at,omitempty"`
	LastSuccessIP          string     `json:"last_success_ip,omitempty"`
	LastSuccessIPSource    string     `json:"last_success_ip_source,omitempty"`
	LastSuccessDurationMS  int        `json:"last_success_duration_ms,omitempty"`
	CookieEnabled          bool       `json:"cookie_enabled,omitempty"`
	CookieCount            int        `json:"cookie_count,omitempty"`
	CookieRemainingSeconds int64      `json:"cookie_remaining_seconds,omitempty"`
	Model                  string     `json:"model"`
	Length                 int        `json:"length,omitempty"`
	Attempts               int        `json:"attempts,omitempty"`
	Ready                  bool       `json:"ready"`
	RemainingSeconds       int64      `json:"remaining_seconds"`
	Blocked                bool       `json:"blocked"`
	ExpiresAt              *time.Time `json:"expires_at,omitempty"`
}

func OpenAICodexTicketStatuses(account *Account, cfg config.OpenAICodexTicketConfig, now time.Time) []OpenAICodexTicketStatus {
	if !cfg.Enabled || !isOpenAICodexTicketAccount(account) {
		return nil
	}
	models, targetLen := cfg.Models, cfg.TargetLength
	if len(models) == 0 {
		models = []string{openAICodexTicketDefaultModel, openAICodexTicketDefaultSolModel}
	}
	if targetLen <= 0 {
		targetLen = 292
	}
	cfg = codexTicketPolicyFromConfig(cfg).Apply(cfg)
	out := make([]OpenAICodexTicketStatus, 0, len(models))
	for _, model := range models {
		model = normalizeOpenAICodexTicketModel(model)
		if model == "" {
			continue
		}
		status := OpenAICodexTicketStatus{Model: model}
		ticket := parseOpenAICodexTicketFromAny(0, model, nil)
		if account != nil && account.Extra != nil {
			ticket = parseOpenAICodexTicketFromAny(account.ID, model, account.Extra[openAICodexTicketExtraKey(model)])
		}
		status.CookieEnabled = cfg.CookieEnabled
		if ticket != nil && !ticket.CapturedAt.IsZero() {
			captured := ticket.CapturedAt
			status.LastSuccessAt = &captured
			status.LastSuccessIP = ticket.EgressIP
			status.LastSuccessIPSource = ticket.EgressIPSource
			status.LastSuccessDurationMS = ticket.DurationMS
		}
		cookies := ticket.liveCookies(now, cfg)
		status.CookieCount = len(cookies)
		for _, cookie := range cookies {
			remaining := int64(cookie.ExpiresAt.Sub(now) / time.Second)
			if status.CookieRemainingSeconds == 0 || remaining < status.CookieRemainingSeconds {
				status.CookieRemainingSeconds = remaining
			}
		}
		if ticket.usable(now, cfg) {
			status.Ready = true
			status.Length = ticket.Length
			status.Attempts = ticket.Attempts
			expires := ticket.ExpiresAt
			if !ticket.CapturedAt.IsZero() {
				if limit := ticket.CapturedAt.Add(time.Duration(cfg.TTLSeconds) * time.Second); limit.Before(expires) {
					expires = limit
				}
			}
			if cfg.CookieRequired {
				for _, c := range ticket.liveCookies(now, cfg) {
					if c.ExpiresAt.Before(expires) {
						expires = c.ExpiresAt
					}
				}
			}
			remaining := int64(expires.Sub(now) / time.Second)
			if remaining < 0 {
				remaining = 0
			}
			status.RemainingSeconds = remaining
			exp := expires
			status.ExpiresAt = &exp
		}
		status.Blocked = (cfg.FailClosed || cfg.CookieRequired) && !status.Ready
		out = append(out, status)
	}
	return out
}

func (s *OpenAIGatewayService) openAICodexTicketEnabled() bool {
	return s.openAICodexTicketEnabledContext(context.Background())
}

func (s *OpenAIGatewayService) openAICodexTicketEnabledContext(ctx context.Context) bool {
	if s == nil {
		return false
	}
	fallback := s.cfg != nil && s.cfg.Gateway.OpenAICodexTicket.Enabled
	if s.settingService != nil {
		return s.settingService.GetOpenAICodexTicketEnabled(ctx, fallback)
	}
	return fallback
}

func (s *OpenAIGatewayService) openAICodexTicketHarvestProxyURL() string {
	return s.openAICodexTicketHarvestProxyURLContext(context.Background())
}

func (s *OpenAIGatewayService) openAICodexTicketHarvestProxyURLContext(ctx context.Context) string {
	if s.settingService != nil {
		if proxy := s.settingService.GetOpenAICodexTicketHarvestProxyURL(ctx); proxy != "" {
			return proxy
		}
	}
	return strings.TrimSpace(s.openAICodexTicketConfig().HarvestProxyURL)
}

func (t *openAICodexTicket) valid(now time.Time, targetLen int) bool {
	if t == nil {
		return false
	}
	state := strings.TrimSpace(t.State)
	if len(state) != targetLen || t.Length != targetLen || !strings.HasPrefix(state, openAICodexTicketStatePrefix) {
		return false
	}
	if t.ExpiresAt.IsZero() || !now.Before(t.ExpiresAt) {
		return false
	}
	return true
}

func (t *openAICodexTicket) needsRefresh(now time.Time, refreshBefore time.Duration) bool {
	if t == nil || t.ExpiresAt.IsZero() {
		return true
	}
	return !t.ExpiresAt.After(now.Add(refreshBefore))
}

func (s *OpenAIGatewayService) lookupOpenAICodexTicket(account *Account, model string) *openAICodexTicket {
	if s == nil || account == nil || account.ID <= 0 {
		return nil
	}
	model = normalizeOpenAICodexTicketModel(model)
	if model == "" {
		return nil
	}
	key := openAICodexTicketKey(account.ID, model)
	targetLen := 292
	if s != nil {
		targetLen = s.openAICodexTicketConfig().TargetLength
	}
	now := time.Now()
	accountState := s.codexTicketAccountState(account.ID)
	accountState.mu.Lock()
	defer accountState.mu.Unlock()
	var mem *openAICodexTicket
	if raw, ok := s.openaiCodexTickets.Load(key); ok {
		mem, _ = raw.(*openAICodexTicket)
	}
	var extra *openAICodexTicket
	if account.Extra != nil {
		extra = parseOpenAICodexTicketFromAny(account.ID, model, account.Extra[openAICodexTicketExtraKey(model)])
	}
	if !accountState.revokedBefore.IsZero() {
		if mem != nil && !mem.CapturedAt.After(accountState.revokedBefore) {
			s.openaiCodexTickets.Delete(key)
			mem = nil
		}
		if extra != nil && !extra.CapturedAt.After(accountState.revokedBefore) {
			extra = nil
		}
	}
	if extra.valid(now, targetLen) && (mem == nil || extra.CapturedAt.After(mem.CapturedAt)) {
		s.openaiCodexTickets.Store(key, extra)
		return extra
	}
	if mem.valid(now, targetLen) {
		return mem
	}
	if extra != nil {
		s.openaiCodexTickets.Store(key, extra)
		return extra
	}
	if mem != nil {
		s.openaiCodexTickets.Delete(key)
	}
	return nil
}

func parseOpenAICodexTicketFromAny(accountID int64, model string, raw any) *openAICodexTicket {
	if raw == nil {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var ticket openAICodexTicket
	if err := json.Unmarshal(b, &ticket); err != nil {
		return nil
	}
	ticket.AccountID = accountID
	if strings.TrimSpace(model) != "" {
		ticket.Model = model
	}
	ticket.State = strings.TrimSpace(ticket.State)
	if ticket.Length == 0 {
		ticket.Length = len(ticket.State)
	}
	if ticket.State == "" {
		return nil
	}
	return &ticket
}

func (s *OpenAIGatewayService) storeOpenAICodexTicket(ctx context.Context, account *Account, ticket *openAICodexTicket) bool {
	if s == nil || account == nil || ticket == nil || account.ID <= 0 {
		return false
	}
	accountState := s.codexTicketAccountState(account.ID)
	accountState.mu.Lock()
	defer accountState.mu.Unlock()
	if !accountState.revokedBefore.IsZero() && !ticket.CapturedAt.After(accountState.revokedBefore) {
		return false
	}
	model := normalizeOpenAICodexTicketModel(ticket.Model)
	ticket.Model = model
	ticket.AccountID = account.ID
	s.openaiCodexTickets.Store(openAICodexTicketKey(account.ID, model), ticket)
	if s.accountRepo == nil {
		return true
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.accountRepo.UpdateExtra(ctx, account.ID, map[string]any{
		openAICodexTicketExtraKey(model): ticket,
	}); err != nil {
		logger.L().Warn("openai_codex_ticket persist failed",
			zap.Int64("account_id", account.ID),
			zap.String("model", model),
			zap.Error(err),
		)
	}
	return true
}

// applyOpenAICodexTicket 在出站请求上覆盖 x-codex-turn-state。
// 请求路径只注入已捕获的有效门票，不现场打票；无票则返回
// ErrOpenAICodexTicketUnavailable。打票由后台 harvester 完成。
func (s *OpenAIGatewayService) applyOpenAICodexTicket(ctx context.Context, account *Account, model string, h http.Header) error {
	if s == nil || h == nil || !isOpenAICodexTicketAccount(account) || !s.openAICodexTicketEnabledContext(ctx) {
		return nil
	}
	model = normalizeOpenAICodexTicketModel(model)
	if model == "" || !s.openAICodexTicketGatedModel(model) {
		return nil
	}
	cfg := s.openAICodexTicketConfig()
	ticket := s.lookupOpenAICodexTicket(account, model)
	if ticket.usable(time.Now(), cfg) {
		if cfg.CookieEnabled {
			applyCodexRoutingCookies(h, ticket.liveCookies(time.Now(), cfg))
		}
		h.Set(openAICodexTurnStateHeader, ticket.State)
		cookieNames := make([]string, 0, 2)
		for _, cookie := range (&http.Request{Header: h}).Cookies() {
			if isCodexRoutingCookie(cookie.Name) {
				cookieNames = append(cookieNames, cookie.Name)
			}
		}
		logger.FromContext(ctx).Info("openai_codex_ticket applied",
			zap.Int64("account_id", account.ID), zap.String("model", model),
			zap.Int("ticket_length", len(h.Get(openAICodexTurnStateHeader))),
			zap.Strings("routing_cookie_names", cookieNames))
		return nil
	}
	if !cfg.FailClosed && !cfg.CookieRequired {
		return nil
	}
	return ErrOpenAICodexTicketUnavailable
}

// openAICodexTicketOutboundModel 预测本请求真正出站的模型名，也就是
// applyOpenAICodexTicket 注入时读到的 body.model。
//
// 调度门控与注入必须按同一个模型名判定门票。普通请求下二者同源：Forward 的
// upstreamModel 与本函数都走 resolveOpenAIAccountUpstreamModelForRequest，且
// Forward 会把 body.model 改写成该值后才注入。但 /responses/compact 例外——
// Forward 会把出站模型进一步改写为 compact 映射或 gateway.openai_compact_model
// （默认非空），此时若门控仍按客户端原始模型判定，就会把「实际出站是非门控
// 模型、根本不需要票」的 compact 请求整片误拦成不可调度。
func (s *OpenAIGatewayService) openAICodexTicketOutboundModel(account *Account, requestedModel string, requireCompact bool) string {
	model := strings.TrimSpace(requestedModel)
	if account == nil || model == "" {
		return model
	}
	if !account.IsOpenAI() {
		return canonicalOpenAIAccountSchedulingModel(account, model)
	}
	_, upstreamModel := resolveOpenAIForwardMappedModels(account, model, requireCompact)
	if requireCompact {
		// 与 Forward 同序：compact 兜底模型优先于普通/compact 映射结果。
		if compactModel := strings.TrimSpace(s.resolveOpenAICompactFallbackModel(account, model)); compactModel != "" {
			upstreamModel = compactModel
		}
	}
	if upstreamModel = strings.TrimSpace(upstreamModel); upstreamModel != "" {
		return upstreamModel
	}
	return model
}

// outboundModel 必须是真正会发给上游的模型名（openAICodexTicketOutboundModel），
// 不是客户端原始模型：注入侧读的是出站 body.model，两侧口径必须一致。
func (s *OpenAIGatewayService) openAICodexTicketBlocksAccount(account *Account, outboundModel string) bool {
	if s == nil || !isOpenAICodexTicketAccount(account) || !s.openAICodexTicketEnabled() {
		return false
	}
	cfg := s.openAICodexTicketConfig()
	if !cfg.FailClosed && !cfg.CookieRequired {
		return false
	}
	model := normalizeOpenAICodexTicketModel(outboundModel)
	if !s.openAICodexTicketGatedModel(model) {
		return false
	}
	ticket := s.lookupOpenAICodexTicket(account, model)
	return !ticket.usable(time.Now(), cfg)
}

func (s *OpenAIGatewayService) fireOpenAICodexTicketProbe(ctx context.Context, account *Account, token, model, proxyURL string, attemptTimeout time.Duration) (state string, status int, err error) {
	result, err := s.fireOpenAICodexTicketProbeDetailed(ctx, account, token, model, proxyURL, attemptTimeout)
	if err != nil {
		return "", result.Status, err
	}
	return result.State, result.Status, nil
}

func (s *OpenAIGatewayService) fireOpenAICodexTicketProbeDetailed(ctx context.Context, account *Account, token, model, proxyURL string, attemptTimeout time.Duration) (result openAICodexTicketProbeResult, err error) {
	return s.fireOpenAICodexTicketProbeSession(ctx, account, token, model, proxyURL, attemptTimeout, nil)
}

func (s *OpenAIGatewayService) fireOpenAICodexTicketProbeSession(ctx context.Context, account *Account, token, model, proxyURL string, attemptTimeout time.Duration, session *codexTicketSession) (result openAICodexTicketProbeResult, err error) {
	result.StartedAt = time.Now().UTC()
	result.ResponseHeaders = map[string]string{}
	var gotConnection, reusedConnection atomic.Bool
	if session != nil {
		result.ResponseHeaders["x-sub2api-connection-session"] = session.id
		defer func() {
			if gotConnection.Load() {
				result.ResponseHeaders["x-sub2api-connection-reused"] = strconv.FormatBool(reusedConnection.Load())
			}
			result.ResponseHeaders["x-sub2api-connection-age-seconds"] = strconv.FormatInt(int64(time.Since(session.createdAt)/time.Second), 10)
		}()
		ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
			GotConn: func(info httptrace.GotConnInfo) {
				gotConnection.Store(true)
				reusedConnection.Store(info.Reused)
			},
		})
	}
	defer func() { result.FinishedAt = time.Now().UTC() }()
	attemptCtx, cancel := context.WithTimeout(ctx, attemptTimeout)
	defer cancel()

	body := []byte(`{"model":` + jsonString(model) + `,"store":false,"stream":true,"instructions":"Reply with exactly: pong","input":[{"role":"user","content":[{"type":"input_text","text":"ping"}]}]}`)
	result.RequestBody = string(body)
	req, err := http.NewRequestWithContext(attemptCtx, http.MethodPost, chatgptCodexURL, bytes.NewReader(body))
	if err != nil {
		result.Reason = "request_build"
		return result, err
	}
	req = req.WithContext(WithHTTPUpstreamProfile(req.Context(), HTTPUpstreamProfileOpenAIHarvest))
	req.Close = true
	req.Host = "chatgpt.com"
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("OpenAI-Beta", "responses=experimental")
	req.Header.Set("session_id", uuid.NewString())
	if err := resolveAndSetOpenAIChatGPTAccountHeaders(attemptCtx, s.accountRepo, req.Header, account); err != nil {
		result.Reason = "headers"
		return result, err
	}
	applyOpenAICodexTicketHarvestIdentity(req.Header, model)
	if session != nil {
		req.Close = false
		req.Header.Set("session_id", session.id)
		if session.ticket != nil {
			req.Header.Set(openAICodexTurnStateHeader, session.ticket.State)
			applyCodexRoutingCookies(req.Header, session.ticket.liveCookies(time.Now(), s.openAICodexTicketConfig()))
		}
	}

	// Synthetic probes must use the dedicated no-reuse transport even when the
	// production account is bound to a plugin. This also avoids reading pluginManager
	// while handlers are still wiring it during gateway construction.
	var resp *http.Response
	if session != nil {
		resp, err = session.client.Do(req)
	} else {
		resp, err = s.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
	}
	if err != nil {
		result.Reason = "transport"
		if errors.Is(err, ErrCodexTicketConnectionLost) {
			result.Reason = "connection_lost"
		}
		return result, err
	}
	if resp == nil {
		result.Reason = "nil_response"
		return result, errors.New("nil upstream response")
	}
	defer func() {
		if resp.Body != nil {
			_ = resp.Body.Close()
		}
	}()
	result.Status = resp.StatusCode
	result.ConnectionClosed = resp.Close
	result.State = extractOpenAICodexTurnState(resp.Header)
	cookieCfg := s.openAICodexTicketConfig()
	if cookieCfg.CookieEnabled {
		var previous []openAICodexTicketCookie
		if session != nil {
			previous = session.ticket.liveCookies(result.StartedAt, cookieCfg)
		}
		result.Cookies = mergeCodexTicketCookies(previous, resp, req.URL, result.StartedAt, cookieCfg.CookieTTLSeconds)
	}
	for _, key := range []string{"Content-Type", "Content-Length", "Retry-After", "x-request-id"} {
		if value := strings.TrimSpace(resp.Header.Get(key)); value != "" {
			result.ResponseHeaders[key] = value
		}
	}
	if state := strings.TrimSpace(resp.Header.Get(openAICodexTurnStateHeader)); state != "" {
		// The admin audit view intentionally keeps the complete turn-state
		// header so operators can compare the exact value returned by upstream.
		result.ResponseHeaders[openAICodexTurnStateHeader] = state
	}
	if session != nil {
		// EOF is required for HTTP/1.1 reuse. Bound both bytes and time; never
		// retain a connection after a truncated or failed response body.
		const maxProbeBody = 1 << 20
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxProbeBody+1))
		if readErr != nil || len(body) > maxProbeBody {
			result.Reason = "response_body"
			return result, errors.New("ticket experiment response incomplete or too large")
		}
		if upstreamModel := codexTicketResponseModel(body); upstreamModel != "" {
			result.ResponseHeaders["x-sub2api-response-model"] = upstreamModel
		}
	}
	for _, key := range []string{"x-egress-ip", "x-proxy-egress-ip"} {
		if value := strings.TrimSpace(resp.Header.Get(key)); net.ParseIP(value) != nil {
			result.EgressIP = value
			result.ResponseHeaders["x-sub2api-egress-ip-source"] = "upstream_header"
			break
		}
	}
	return result, nil
}

func jsonString(v string) string {
	b, err := json.Marshal(v)
	if err != nil {
		return `""`
	}
	return string(b)
}

func applyOpenAICodexTicketHarvestIdentity(h http.Header, model string) {
	ensureCodexIdentityHeaders(h)
	enforceCodexIdentityHeaders(h)
	version := strings.TrimSpace(h.Get("version"))
	if needsOpenAICodexAstraVersion(model) && (version == "" || CompareVersions(version, openAICodexAstraMinVersion) < 0) {
		h.Set("version", openAICodexAstraMinVersion)
		h.Set("user-agent", buildCodexCLIUserAgent(openAICodexAstraMinVersion))
		h.Set("originator", openai.CodexDefaultOriginator)
	}
}

func needsOpenAICodexAstraVersion(model string) bool {
	m := strings.ToLower(normalizeOpenAICodexTicketModel(model))
	return strings.Contains(m, "gpt-6") || strings.Contains(m, "astra")
}

func (s *OpenAIGatewayService) StartOpenAICodexTicketHarvester() {
	if s == nil {
		return
	}
	s.openaiCodexTicketLifecycleMu.Lock()
	defer s.openaiCodexTicketLifecycleMu.Unlock()
	if s.openaiCodexTicketStopped || s.openaiCodexTicketDone != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.openaiCodexTicketCancel = cancel
	s.openaiCodexTicketDone = done
	go func() {
		defer close(done)
		s.openAICodexTicketHarvestLoop(ctx)
	}()
	logger.L().Info("openai_codex_ticket harvester started",
		zap.Int("ttl_seconds", s.openAICodexTicketConfig().TTLSeconds),
		zap.Int("target_length", s.openAICodexTicketConfig().TargetLength),
		zap.Strings("models", s.openAICodexTicketConfig().Models),
	)
}

func (s *OpenAIGatewayService) StopOpenAICodexTicketHarvester() {
	if s == nil {
		return
	}
	s.openaiCodexTicketLifecycleMu.Lock()
	s.openaiCodexTicketStopped = true
	cancel, done := s.openaiCodexTicketCancel, s.openaiCodexTicketDone
	s.openaiCodexTicketLifecycleMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
	s.closeCodexTicketSessions(nil)
}

// DiscardOpenAICodexTickets removes the current ticket(s) for one account, or
// for all active OpenAI accounts when accountID is nil. The harvester notices
// the missing ticket on its next cycle and obtains a replacement automatically.
func (s *OpenAIGatewayService) DiscardOpenAICodexTickets(ctx context.Context, accountID *int64) (OpenAICodexTicketDiscardResult, error) {
	var result OpenAICodexTicketDiscardResult
	if s == nil || s.accountRepo == nil {
		return result, errors.New("codex ticket account repository is unavailable")
	}
	var accounts []Account
	var err error
	if accountID != nil {
		var account *Account
		account, err = s.accountRepo.GetByID(ctx, *accountID)
		if err == nil && account != nil {
			accounts = []Account{*account}
		}
	} else {
		accounts, err = s.accountRepo.ListByPlatform(ctx, PlatformOpenAI)
	}
	if err != nil {
		return result, err
	}

	remover, ok := s.accountRepo.(openAICodexTicketExtraRemover)
	if !ok {
		return result, errors.New("codex ticket account repository does not support clearing tickets")
	}
	for i := range accounts {
		account := &accounts[i]
		if !isOpenAICodexTicketAccount(account) {
			continue
		}
		accountState := s.codexTicketAccountState(account.ID)
		accountState.mu.Lock()
		keys := make([]string, 0)
		seen := make(map[string]struct{})
		if account.Extra != nil {
			for key := range account.Extra {
				if IsOpenAICodexTicketExtraKey(key) {
					keys = append(keys, key)
					seen[key] = struct{}{}
				}
			}
		}
		for _, model := range s.openAICodexTicketConfig().Models {
			key := openAICodexTicketExtraKey(model)
			_, inMemory := s.openaiCodexTickets.Load(openAICodexTicketKey(account.ID, model))
			if _, exists := seen[key]; !exists && inMemory {
				keys = append(keys, key)
			}
		}
		if err := remover.RemoveExtraKeys(ctx, account.ID, keys); err != nil {
			accountState.mu.Unlock()
			return result, err
		}
		accountState.revokedBefore = time.Now()
		for _, key := range keys {
			model := strings.TrimPrefix(key, openAICodexTicketExtraKeyPrefix)
			s.openaiCodexTickets.Delete(openAICodexTicketKey(account.ID, model))
		}
		s.closeCodexTicketSessions(&account.ID)
		accountState.mu.Unlock()
		if len(keys) > 0 {
			result.ClearedAccounts++
		}
		result.ClearedTickets += len(keys)
	}
	return result, nil
}

func (s *OpenAIGatewayService) openAICodexTicketHarvestLoop(ctx context.Context) {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			s.refreshOpenAICodexTickets(ctx)
			finished := time.Now()
			// Re-read the runtime interval while idle, so shortening a long
			// interval takes effect without waiting for the old timer.
			for {
				remaining := time.Duration(s.openAICodexTicketConfig().HarvestProbeIntervalSeconds)*time.Second - time.Since(finished)
				if remaining <= 0 {
					break
				}
				if remaining > time.Second {
					remaining = time.Second
				}
				timer.Reset(remaining)
				select {
				case <-ctx.Done():
					return
				case <-timer.C:
				}
			}
			timer.Reset(0)
		}
	}
}

// refreshOpenAICodexTickets probes each account/model with a missing or soon-to-expire
// ticket once. The loop waits for all probes, then waits the configured interval
// before starting the next cycle.
func (s *OpenAIGatewayService) refreshOpenAICodexTickets(ctx context.Context) {
	if s == nil {
		return
	}
	if s.accountRepo == nil || ctx.Err() != nil || !s.openAICodexTicketEnabledContext(ctx) {
		s.closeCodexTicketSessions(nil)
		return
	}
	accounts, err := s.accountRepo.ListByPlatform(ctx, PlatformOpenAI)
	if err != nil {
		logger.L().Warn("openai_codex_ticket list accounts failed", zap.Error(err))
		return
	}
	cfg := s.openAICodexTicketConfig()
	s.pruneCodexTicketSessions(ctx, accounts, cfg)
	now := time.Now()
	var wg sync.WaitGroup
	probed := 0
	for i := range accounts {
		account := accounts[i]
		if !isOpenAICodexTicketHarvestAccount(&account) {
			continue
		}
		for _, model := range cfg.Models {
			model := normalizeOpenAICodexTicketModel(model)
			if model == "" {
				continue
			}
			// 已有一张有效且未临近过期的票 → 本周期不打，省得白刷。
			if t := s.lookupOpenAICodexTicket(&account, model); !t.policyNeedsRefresh(now, cfg) && !s.codexTicketSessionDue(account.ID, model, now) {
				continue
			}
			acc := account
			// Token/header helpers may update account metadata; each model owns its maps.
			acc.Extra = maps.Clone(account.Extra)
			acc.Credentials = maps.Clone(account.Credentials)
			probed++
			wg.Add(1)
			go func(acc Account, model string) {
				defer wg.Done()
				s.probeOnceOpenAICodexTicket(ctx, &acc, model)
			}(acc, model)
		}
	}
	wg.Wait()
	if probed > 0 {
		logger.L().Info("openai_codex_ticket probe cycle", zap.Int("probed", probed))
	}
}

// probeOnceOpenAICodexTicket 走打票代理打一发。命中合格 292（HTTP 200、长度==target、
// gAAAAA 前缀）就落库；否则记 Info miss，交给下个周期重试。同一 key 并发去重，避免上一发还没
// 回来又叠一发。
func (s *OpenAIGatewayService) probeOnceOpenAICodexTicket(ctx context.Context, account *Account, model string) {
	if s == nil || !isOpenAICodexTicketAccount(account) || ctx.Err() != nil || !s.openAICodexTicketEnabledContext(ctx) {
		return
	}
	cfg := s.openAICodexTicketConfig()
	proxyURL := s.openAICodexTicketHarvestProxyURLContext(ctx)
	if proxyURL == "" || s.httpUpstream == nil || ctx.Err() != nil {
		return
	}
	key := openAICodexTicketKey(account.ID, model)
	_, _, _ = s.openaiCodexTicketFlight.Do(key, func() (any, error) {
		startedAt := time.Now().UTC()
		session, sessionErr := s.codexTicketSessionForProbe(account.ID, model, proxyURL, cfg)
		if sessionErr != nil {
			logger.L().Warn("openai_codex_ticket experiment unavailable", zap.Int64("account_id", account.ID))
			return nil, nil
		}
		keepSession := false
		probeGeneration := s.codexTicketAccountState(account.ID)
		probeGeneration.mu.Lock()
		revokedAtStart := probeGeneration.revokedBefore
		probeGeneration.mu.Unlock()
		defer func() {
			if session != nil && !keepSession {
				s.releaseCodexTicketSession(key, session)
			}
		}()
		writeAudit := func(item *OpenAICodexTicketAudit) {
			if item != nil {
				if item.Outcome != "success" && item.Outcome != "validated" {
					item.EgressIP = ""
					delete(item.ResponseHeaders, "x-sub2api-egress-ip-source")
				}
				if session != nil {
					if item.ResponseHeaders == nil {
						item.ResponseHeaders = map[string]string{}
					}
					item.ResponseHeaders["x-sub2api-connection-session"] = session.id
					item.ResponseHeaders["x-sub2api-connection-successes"] = strconv.Itoa(session.successes)
					item.ResponseHeaders["x-sub2api-connection-retained"] = strconv.FormatBool(keepSession)
				}
			}
			if s.codexTicketAuditRepo == nil || item == nil {
				return
			}
			if item.StartedAt.IsZero() {
				item.StartedAt = startedAt
			}
			if item.FinishedAt.IsZero() {
				item.FinishedAt = time.Now().UTC()
			}
			item.DurationMS = int(item.FinishedAt.Sub(item.StartedAt) / time.Millisecond)
			go func(a *OpenAICodexTicketAudit) {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				if err := s.codexTicketAuditRepo.Insert(ctx, a); err != nil {
					logger.L().Warn("openai_codex_ticket audit persist failed", zap.Error(err))
				}
			}(item)
		}
		token, _, err := s.GetAccessToken(ctx, account)
		if err != nil || strings.TrimSpace(token) == "" {
			writeAudit(&OpenAICodexTicketAudit{AccountID: account.ID, Model: model, StartedAt: startedAt, FinishedAt: time.Now().UTC(), Outcome: "token_error", Reason: "token"})
			logger.L().Info("openai_codex_ticket probe miss",
				zap.Int64("account_id", account.ID), zap.String("model", model),
				zap.String("reason", "token"), zap.Error(err))
			return nil, nil
		}
		probe, perr := s.fireOpenAICodexTicketProbeSession(ctx, account, token, model, proxyURL, time.Duration(cfg.HarvestAttemptTimeoutSeconds)*time.Second, session)
		if perr != nil {
			writeAudit(&OpenAICodexTicketAudit{AccountID: account.ID, Model: model, StartedAt: probe.StartedAt, FinishedAt: probe.FinishedAt, Outcome: "transport_error", Reason: probe.Reason, HTTPStatus: nullableHTTPStatus(probe.Status), RequestBody: probe.RequestBody, ResponseHeaders: probe.ResponseHeaders, EgressIP: probe.EgressIP})
			logger.L().Info("openai_codex_ticket probe miss",
				zap.Int64("account_id", account.ID), zap.String("model", model),
				zap.String("reason", "error"), zap.Error(perr))
			return nil, nil
		}
		state, status := probe.State, probe.Status
		now := time.Now()
		validatedOld := false
		if session != nil && status == http.StatusOK &&
			probe.ResponseHeaders["x-sub2api-response-model"] == model &&
			session.ticket.usable(now, cfg) && (state == "" || state == session.ticket.State) {
			state = session.ticket.State
			validatedOld = true
		}
		// A single semantic miss does not prove the connection has died. Keep
		// only a previously proven, unexpired session for one bounded recheck.
		retainForRecheck := func() {
			if session == nil {
				return
			}
			session.successes = 0
			session.failures++
			if session.failures < 2 && status == http.StatusOK && !probe.ConnectionClosed &&
				session.ticket.usable(now, cfg) && len(session.ticket.liveCookies(now, cfg)) == 2 &&
				len((&openAICodexTicket{Cookies: probe.Cookies}).liveCookies(now, cfg)) == 2 {
				session.nextProbe.Store(now.Add(5 * time.Second).UnixNano())
				keepSession = true
			}
			probeGeneration.mu.Lock()
			if !probeGeneration.revokedBefore.Equal(revokedAtStart) {
				keepSession = false
			}
			probeGeneration.mu.Unlock()
		}
		if status != http.StatusOK || state == "" || len(state) != cfg.TargetLength || !strings.HasPrefix(state, openAICodexTicketStatePrefix) {
			retainForRecheck()
			reason := "invalid_ticket"
			if status != http.StatusOK {
				reason = "http_error"
			}
			writeAudit(&OpenAICodexTicketAudit{AccountID: account.ID, Model: model, StartedAt: probe.StartedAt, FinishedAt: probe.FinishedAt, Outcome: reason, Reason: fmt.Sprintf("status=%d length=%d", status, len(state)), HTTPStatus: nullableHTTPStatus(status), TicketLength: len(state), RequestBody: probe.RequestBody, ResponseHeaders: probe.ResponseHeaders, EgressIP: probe.EgressIP})
			logger.L().Info("openai_codex_ticket probe miss",
				zap.Int64("account_id", account.ID), zap.String("model", model),
				zap.Int("http", status), zap.Int("len", len(state)))
			return nil, nil
		}
		if session != nil {
			if responseModel := probe.ResponseHeaders["x-sub2api-response-model"]; responseModel != model {
				retainForRecheck()
				writeAudit(&OpenAICodexTicketAudit{AccountID: account.ID, Model: model, StartedAt: probe.StartedAt, FinishedAt: probe.FinishedAt, Outcome: "model_mismatch", Reason: "experiment_response_model_missing_or_different", HTTPStatus: nullableHTTPStatus(status), TicketLength: len(state), ResponseHeaders: probe.ResponseHeaders})
				return nil, nil
			}
		}
		attempts := 1
		if previous := s.lookupOpenAICodexTicket(account, model); previous != nil {
			attempts = previous.Attempts + 1
		}
		ticket := &openAICodexTicket{
			EgressIP:       probe.EgressIP,
			EgressIPSource: probe.ResponseHeaders["x-sub2api-egress-ip-source"],
			DurationMS:     int(probe.FinishedAt.Sub(probe.StartedAt) / time.Millisecond),
			Cookies:        probe.Cookies,
			AccountID:      account.ID,
			Model:          model,
			State:          state,
			Length:         len(state),
			CapturedAt:     probe.StartedAt,
			ExpiresAt:      now.Add(time.Duration(cfg.TTLSeconds) * time.Second),
			Attempts:       attempts,
		}
		if (cfg.CookieRequired || session != nil) && len(ticket.liveCookies(now, cfg)) != 2 {
			if session != nil && session.ticket != nil {
				// Propagate explicit cookie removal to scheduling as well as the
				// retained connection; do not leave deleted cookies usable in cache.
				previous := *session.ticket
				previous.Cookies = probe.Cookies
				s.storeOpenAICodexTicket(ctx, account, &previous)
			}
			writeAudit(&OpenAICodexTicketAudit{AccountID: account.ID, Model: model, StartedAt: probe.StartedAt, FinishedAt: probe.FinishedAt, Outcome: "missing_cookie", Reason: "required_routing_cookies_missing_or_expired", HTTPStatus: nullableHTTPStatus(status), TicketLength: ticket.Length, EgressIP: probe.EgressIP, ResponseHeaders: probe.ResponseHeaders})
			return nil, nil
		}
		if session != nil && session.ticket != nil && session.ticket.State == state {
			// An echoed ticket does not prove a new validity period.
			ticket.CapturedAt = session.ticket.CapturedAt
			ticket.ExpiresAt = session.ticket.ExpiresAt
			probe.ResponseHeaders["x-sub2api-ticket-renewed"] = "false"
			if !ticket.usable(now, cfg) {
				writeAudit(&OpenAICodexTicketAudit{AccountID: account.ID, Model: model, Outcome: "expired_ticket", Reason: "echoed_ticket_expired", ResponseHeaders: probe.ResponseHeaders})
				return nil, nil
			}
			validatedOld = true
		} else if session != nil {
			probe.ResponseHeaders["x-sub2api-ticket-renewed"] = "true"
		}
		if session != nil {
			if ticket.EgressIP != "" {
				session.egressIP, session.ipSource = ticket.EgressIP, "upstream_header"
				session.ipChecked = true
			} else if !session.ipChecked && !probe.ConnectionClosed {
				session.ipChecked = true
				var alive bool
				session.egressIP, alive = session.lookupEgressIP(ctx)
				if session.egressIP != "" {
					session.ipSource = "same_connection_trace"
				}
				if !alive {
					probe.ConnectionClosed = true
				}
			}
			ticket.EgressIP, ticket.EgressIPSource = session.egressIP, session.ipSource
			probe.EgressIP = ticket.EgressIP
			if ticket.EgressIP != "" {
				probe.ResponseHeaders["x-sub2api-egress-ip-source"] = ticket.EgressIPSource
			}
		}
		if !s.storeOpenAICodexTicket(ctx, account, ticket) {
			writeAudit(&OpenAICodexTicketAudit{AccountID: account.ID, Model: model, StartedAt: probe.StartedAt, FinishedAt: probe.FinishedAt, Outcome: "discarded", Reason: "ticket_revoked_during_probe", HTTPStatus: nullableHTTPStatus(status), TicketLength: len(state)})
			return nil, nil
		}
		if session != nil {
			session.ticket = ticket
			session.failures = 0
			session.successes++
			delay := 5 * time.Second
			if session.successes > 3 {
				delay = 30 * time.Second
			}
			session.nextProbe.Store(time.Now().Add(delay).UnixNano())
			keepSession = !probe.ConnectionClosed
		}
		outcome, reason := "success", "ticket_captured"
		if validatedOld {
			outcome, reason = "validated", "existing_ticket_validated"
		}
		if ticket.EgressIP == "" && session != nil && validatedOld {
			// Unknown is preferable to an IP measured over another connection.
			probe.ResponseHeaders["x-sub2api-egress-ip-source"] = "unknown"
		}
		hash := sha256.Sum256([]byte(state))
		writeAudit(&OpenAICodexTicketAudit{AccountID: account.ID, Model: model, StartedAt: probe.StartedAt, FinishedAt: probe.FinishedAt, Outcome: outcome, Reason: reason, HTTPStatus: nullableHTTPStatus(status), TicketLength: ticket.Length, Attempts: attempts, TicketExpiresAt: &ticket.ExpiresAt, RequestBody: probe.RequestBody, ResponseHeaders: probe.ResponseHeaders, TicketHash: hex.EncodeToString(hash[:]), EgressIP: probe.EgressIP})
		logger.L().Info("openai_codex_ticket harvested",
			zap.Int64("account_id", account.ID), zap.String("model", model),
			zap.Int("length", ticket.Length), zap.String("mode", "continuous"))
		return nil, nil
	})
}

func nullableHTTPStatus(status int) *int {
	if status <= 0 {
		return nil
	}
	return &status
}

// IsOpenAICodexTicketExtraKey identifies server-managed ticket material.
func IsOpenAICodexTicketExtraKey(key string) bool {
	return strings.HasPrefix(key, openAICodexTicketExtraKeyPrefix)
}

// MergeOpenAICodexTicketExtra preserves only persisted tickets, never summaries or
// blobs supplied by an account edit. The repository repeats this under the row
// lock so a concurrent harvest cannot be overwritten by a stale admin snapshot.
func MergeOpenAICodexTicketExtra(extra, current map[string]any) map[string]any {
	result := maps.Clone(extra)
	for key := range result {
		if IsOpenAICodexTicketExtraKey(key) {
			delete(result, key)
		}
	}
	for key, value := range current {
		if IsOpenAICodexTicketExtraKey(key) {
			if result == nil {
				result = make(map[string]any)
			}
			result[key] = value
		}
	}
	return result
}

// ValidateOpenAICodexTicketHarvestProxyURL validates only syntax, without making
// a network request or including credentials in validation errors.
func ValidateOpenAICodexTicketHarvestProxyURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" || parsed.Opaque != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return errors.New("harvest proxy must be an HTTP(S) or SOCKS5(h) URL with a host and no path, query or fragment")
	}
	switch parsed.Scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return errors.New("harvest proxy scheme must be http, https, socks5 or socks5h")
	}
	if port := parsed.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return errors.New("harvest proxy port must be between 1 and 65535")
		}
	}
	return nil
}

// MaskProxyURL never returns a stored proxy password, even for invalid legacy data.
func MaskProxyURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || ValidateOpenAICodexTicketHarvestProxyURL(raw) != nil {
		return ""
	}
	parsed, _ := url.Parse(raw)
	if parsed.User != nil {
		if _, ok := parsed.User.Password(); ok {
			parsed.User = url.UserPassword(parsed.User.Username(), "***")
		}
	}
	return parsed.String()
}

// IsMaskedProxyURL recognizes the exact password placeholder emitted by the API.
func IsMaskedProxyURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return true
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User == nil {
		return false
	}
	password, ok := parsed.User.Password()
	return ok && password == "***"
}

// Credential shadows do not own tickets. Keep their existing forwarding policy
// instead of imposing a gate for a key the harvester never populates.
func isOpenAICodexTicketAccount(account *Account) bool {
	return account != nil && account.IsOpenAIOAuthLike() && !account.IsShadow()
}

// Tickets are only useful for accounts that can currently receive traffic.
// Group membership is checked separately because it is not part of IsSchedulable.
func isOpenAICodexTicketHarvestAccount(account *Account) bool {
	return isOpenAICodexTicketAccount(account) && account.IsSchedulable() && len(account.GroupIDs) > 0
}

// IsOpenAICodexTicketPrivateExtraKey also covers the retired account-level proxy
// override, whose credentials may remain in older account records.
func IsOpenAICodexTicketPrivateExtraKey(key string) bool {
	return IsOpenAICodexTicketExtraKey(key) || key == "codex_harvest_proxy_url"
}

// RedactOpenAICodexTicketExtra strips ephemeral ticket material from exports
// without changing the source account or unrelated backup fields.
func RedactOpenAICodexTicketExtra(extra map[string]any) map[string]any {
	redacted := maps.Clone(extra)
	for key := range redacted {
		if IsOpenAICodexTicketPrivateExtraKey(key) {
			delete(redacted, key)
		}
	}
	return redacted
}
