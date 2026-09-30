package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/requestcapture"
	"github.com/gin-gonic/gin"
)

const (
	SettingKeyBPS403SessionBlockEnabled    = "bps403_session_block_enabled"
	SettingKeyBPS403SessionBlockTTLSeconds = "bps403_session_block_ttl_seconds"
	SettingKeyBPS403CaptureEnabled         = "bps403_capture_enabled"
	BPS403SessionMaxTTLSeconds             = 7 * 24 * 60 * 60
	bps403RequestBodyKey                   = "bps403_original_request_body"
	bps403CapturedKey                      = "bps403_request_captured"
	BPS403SessionBlockedCode               = "session_blocked_by_bps403"
	BPS403SessionBlockedMessage            = "该会话因 BPS 上游返回 403 已被屏蔽，请开启新会话 / This session was blocked after an upstream BPS 403; please start a new session"
)

type bps403Runtime struct {
	enabled   bool
	capture   bool
	ttl       time.Duration
	expiresAt int64
}

func (s *SettingService) getBPS403Runtime(ctx context.Context) bps403Runtime {
	fallback := bps403Runtime{ttl: time.Hour}
	if s == nil || s.settingRepo == nil {
		return fallback
	}
	if v, ok := s.bps403RuntimeCache.Load().(*bps403Runtime); ok && time.Now().UnixNano() < v.expiresAt {
		return *v
	}
	result, _, _ := s.bps403RuntimeSF.Do("bps403_runtime", func() (any, error) {
		if v, ok := s.bps403RuntimeCache.Load().(*bps403Runtime); ok && time.Now().UnixNano() < v.expiresAt {
			return v, nil
		}
		dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		v := fallback
		cacheTTL := time.Minute
		values := make(map[string]string, 3)
		for _, key := range []string{SettingKeyBPS403SessionBlockEnabled, SettingKeyBPS403SessionBlockTTLSeconds, SettingKeyBPS403CaptureEnabled} {
			value, err := s.settingRepo.GetValue(dbCtx, key)
			if err != nil && !errors.Is(err, ErrSettingNotFound) {
				cacheTTL = 5 * time.Second
				continue
			}
			values[key] = strings.TrimSpace(value)
		}
		v.enabled = values[SettingKeyBPS403SessionBlockEnabled] == "true"
		v.capture = values[SettingKeyBPS403CaptureEnabled] == "true"
		if n, err := strconv.Atoi(values[SettingKeyBPS403SessionBlockTTLSeconds]); err == nil && n > 0 && n <= BPS403SessionMaxTTLSeconds {
			v.ttl = time.Duration(n) * time.Second
		}
		v.expiresAt = time.Now().Add(cacheTTL).UnixNano()
		s.bps403RuntimeCache.Store(&v)
		return &v, nil
	})
	if v, ok := result.(*bps403Runtime); ok {
		return *v
	}
	return fallback
}

func (s *OpenAIGatewayService) BPS403SessionBlockEnabled(ctx context.Context) bool {
	return s != nil && s.settingService.getBPS403Runtime(ctx).enabled
}

// Reuse the explicit, typed, API-key-scoped identity resolver, but never share
// block keys with cyber_policy. Disabling either feature leaves the other intact.
func bps403SessionKey(identity CyberSessionIdentityResolution) string {
	if !identity.Resolved() {
		return ""
	}
	sum := sha256.Sum256([]byte("bps403-session:v1|" + identity.BlockKey))
	return hex.EncodeToString(sum[:])
}

func (s *OpenAIGatewayService) FindBPS403SessionBlockedForIdentity(ctx context.Context, identity CyberSessionIdentityResolution) string {
	if !s.BPS403SessionBlockEnabled(ctx) {
		return ""
	}
	key := bps403SessionKey(identity)
	store := s.cyberSessionBlockStore()
	if key == "" || store == nil {
		return ""
	}
	matched, err := store.FindCyberSessionBlocked(ctx, []string{key})
	if err != nil {
		logger.LegacyPrintf("service.bps403_session", "session block lookup failed: error_type=%T", err)
		return ""
	}
	return matched
}

// Remember the already-read plaintext before model/compatibility rewriting.
// The bytes stay request-scoped; successful requests never persist this body.
func RememberBPS403RequestBody(c *gin.Context, body []byte) {
	if c == nil {
		return
	}
	if _, exists := c.Get(bps403RequestBodyKey); !exists {
		c.Set(bps403RequestBodyKey, body)
	}
}

func bps403OriginalBody(c *gin.Context) []byte {
	if c.Request != nil {
		if body := requestcapture.DeferredBody(c.Request.Context()); body != nil {
			return body
		}
	}
	v, _ := c.Get(bps403RequestBodyKey)
	body, _ := v.([]byte)
	return body
}

// This wrapper is used only for the actual BPS Responses HTTP calls, including
// compaction and repair attempts. Attachments, native Codex and local 403s do not
// enter it. Any real HTTP 403 on the exact path qualifies, regardless of its code.
func (s *OpenAIGatewayService) doBPS403ObservedRequest(c *gin.Context, account *Account, req *http.Request, proxyURL string) (*http.Response, error) {
	resp, err := s.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
	if err == nil && resp != nil && resp.StatusCode == http.StatusForbidden && req.URL.Path == "/basispoints/api/responses" {
		s.observeBPS403Response(c, account, req, resp)
	}
	return resp, err
}

func (s *OpenAIGatewayService) observeBPS403Response(c *gin.Context, account *Account, req *http.Request, resp *http.Response) {
	if c == nil || c.Request == nil || isQualityObservation(req.Context()) {
		return
	}
	runtime := s.settingService.getBPS403Runtime(req.Context())
	if !runtime.enabled && !runtime.capture {
		return
	}
	apiKeyID := getAPIKeyIDFromContext(c)
	if apiKeyID <= 0 {
		return
	} // Internal probes have no authenticated client session.
	body := bps403OriginalBody(c)
	identity := ResolveCyberSessionIdentity(apiKeyID, c, body)
	key := bps403SessionKey(identity)
	if runtime.enabled && key != "" {
		if store := s.cyberSessionBlockStore(); store != nil {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(req.Context()), 500*time.Millisecond)
			err := store.SetCyberSessionBlocked(ctx, "", []string{key}, runtime.ttl)
			cancel()
			if err != nil {
				logger.LegacyPrintf("service.bps403_session", "session block write failed: error_type=%T", err)
			}
		}
	}
	// A single request may have repair attempts. Preserve the first actual 403.
	if !runtime.capture || c.GetBool(bps403CapturedKey) {
		return
	}
	c.Set(bps403CapturedKey, true)
	manager := s.settingService.requestCapture
	if manager == nil || req.GetBody == nil {
		logger.LegacyPrintf("service.bps403_session", "request capture unavailable")
		return
	}
	reader, err := req.GetBody()
	if err != nil {
		return
	}
	wire, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil {
		return
	}
	requestID, _ := c.Request.Context().Value(ctxkey.ClientRequestID).(string)
	meta := requestcapture.Meta{APIKeyID: apiKeyID, SessionHash: key, RequestID: requestID, Method: c.Request.Method, Path: c.Request.URL.Path, Protocol: "http", ClientRequestID: c.GetHeader("X-Client-Request-ID")}
	if v, ok := c.Get("api_key"); ok {
		if apiKey, ok := v.(*APIKey); ok && apiKey != nil {
			meta.UserID = apiKey.UserID
			if apiKey.GroupID != nil {
				meta.GroupID = *apiKey.GroupID
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(req.Context()), 5*time.Second)
	defer cancel()
	_, err = manager.CaptureBPS403(ctx, meta, account.ID, body, wire, req.Header, resp.Header)
	if err != nil {
		logger.LegacyPrintf("service.bps403_session", "request capture failed: error_type=%T", err)
	}
}
