package service

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

type openAICodexTicketCookie struct {
	Name       string    `json:"name"`
	Value      string    `json:"value"`
	CapturedAt time.Time `json:"captured_at"`
	ExpiresAt  time.Time `json:"expires_at"`
}

func isCodexRoutingCookie(name string) bool { return name == "__cflb" || name == "__oailb" }

// Missing Set-Cookie preserves the original deadline; explicit deletion or an
// unusable replacement removes the old value instead of reviving it.
func mergeCodexTicketCookies(previous []openAICodexTicketCookie, resp *http.Response, endpoint *url.URL, now time.Time, ttl int) []openAICodexTicketCookie {
	byName := make(map[string]openAICodexTicketCookie)
	for _, c := range previous {
		if now.Before(c.ExpiresAt) {
			byName[c.Name] = c
		}
	}
	for _, c := range resp.Cookies() {
		if isCodexRoutingCookie(c.Name) {
			delete(byName, c.Name)
		}
	}
	for _, c := range captureCodexTicketCookies(resp, endpoint, now, ttl) {
		byName[c.Name] = c
	}
	result := make([]openAICodexTicketCookie, 0, 2)
	for _, name := range []string{"__cflb", "__oailb"} {
		if c, ok := byName[name]; ok {
			result = append(result, c)
		}
	}
	return result
}

// Capture only routing cookies applicable to the exact trusted probe endpoint.
// Never retain arbitrary authentication cookies or expose values in audit headers.
func captureCodexTicketCookies(resp *http.Response, endpoint *url.URL, now time.Time, ttl int) []openAICodexTicketCookie {
	byName := map[string]openAICodexTicketCookie{}
	for _, c := range resp.Cookies() {
		if !isCodexRoutingCookie(c.Name) {
			continue
		}
		delete(byName, c.Name)
		if c.MaxAge < 0 || c.Value == "" || c.Valid() != nil || len(c.Value) > 4096 {
			continue
		}
		domain := strings.ToLower(strings.TrimPrefix(c.Domain, "."))
		host := strings.ToLower(endpoint.Hostname())
		if domain != "" && host != domain && !strings.HasSuffix(host, "."+domain) {
			continue
		}
		path := c.Path
		if path != "" && path != endpoint.Path && (!strings.HasPrefix(endpoint.Path, path) || (!strings.HasSuffix(path, "/") && !strings.HasPrefix(strings.TrimPrefix(endpoint.Path, path), "/"))) {
			continue
		}
		if c.Secure && endpoint.Scheme != "https" {
			continue
		}
		expires := now.Add(time.Duration(ttl) * time.Second)
		if c.MaxAge > 0 {
			if t := now.Add(time.Duration(min(c.MaxAge, ttl)) * time.Second); t.Before(expires) {
				expires = t
			}
		} else if !c.Expires.IsZero() && c.Expires.Before(expires) {
			expires = c.Expires
		}
		if !now.Before(expires) {
			continue
		}
		byName[c.Name] = openAICodexTicketCookie{c.Name, c.Value, now, expires}
	}
	result := make([]openAICodexTicketCookie, 0, len(byName))
	for _, name := range []string{"__cflb", "__oailb"} {
		if c, ok := byName[name]; ok {
			result = append(result, c)
		}
	}
	return result
}

func (t *openAICodexTicket) liveCookies(now time.Time, cfg config.OpenAICodexTicketConfig) []openAICodexTicketCookie {
	if t == nil || !cfg.CookieEnabled {
		return nil
	}
	result := []openAICodexTicketCookie{}
	for _, c := range t.Cookies {
		if !isCodexRoutingCookie(c.Name) || c.CapturedAt.IsZero() {
			continue
		}
		expires := c.ExpiresAt
		limit := c.CapturedAt.Add(time.Duration(cfg.CookieTTLSeconds) * time.Second)
		if limit.Before(expires) {
			expires = limit
		}
		if now.Before(expires) && (&http.Cookie{Name: c.Name, Value: c.Value}).Valid() == nil && c.Value != "" {
			c.ExpiresAt = expires
			result = append(result, c)
		}
	}
	return result
}

func (t *openAICodexTicket) usable(now time.Time, cfg config.OpenAICodexTicketConfig) bool {
	if !t.valid(now, cfg.TargetLength) {
		return false
	}
	if !t.CapturedAt.IsZero() && !now.Before(t.CapturedAt.Add(time.Duration(cfg.TTLSeconds)*time.Second)) {
		return false
	}
	if cfg.CookieRequired && len(t.liveCookies(now, cfg)) != 2 {
		return false
	}
	return true
}

func (t *openAICodexTicket) policyNeedsRefresh(now time.Time, cfg config.OpenAICodexTicketConfig) bool {
	if !t.usable(now, cfg) {
		return true
	}
	until := t.ExpiresAt
	refresh := time.Duration(cfg.RefreshBeforeSeconds) * time.Second
	if !t.CapturedAt.IsZero() {
		if limit := t.CapturedAt.Add(time.Duration(cfg.TTLSeconds) * time.Second); limit.Before(until) {
			until = limit
		}
	}
	if cfg.CookieEnabled {
		cookies := t.liveCookies(now, cfg)
		if len(cookies) != 2 {
			return true
		}
		for _, c := range cookies {
			if c.ExpiresAt.Before(until) {
				until = c.ExpiresAt
			}
			// The upstream may set a much shorter lifetime than our local cap.
			// Limit early refresh using the effective lifetime, not the configured cap,
			// otherwise a fresh short-lived cookie triggers another probe immediately.
			if window := c.ExpiresAt.Sub(c.CapturedAt) / 4; refresh > window {
				refresh = window
			}
		}
	}
	return !until.After(now.Add(refresh))
}

func applyCodexRoutingCookies(h http.Header, cookies []openAICodexTicketCookie) {
	req := &http.Request{Header: h}
	existing := req.Cookies()
	h.Del("Cookie")
	for _, c := range existing {
		if !isCodexRoutingCookie(c.Name) {
			req.AddCookie(c)
		}
	}
	for _, c := range cookies {
		req.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	}
}
