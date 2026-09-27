package service

import (
	"context"
	"encoding/xml"
	"io"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Only user environment blocks and structured search tools are eligible.
// Ordinary messages, instructions, timestamps and unrelated platforms stay intact.
func hasAccountTimezoneFields(body []byte) bool {
	for _, item := range gjson.GetBytes(body, "input").Array() {
		if item.Get("role").String() != "user" {
			continue
		}
		content := item.Get("content")
		if content.Type == gjson.String && strings.HasPrefix(strings.TrimSpace(content.String()), "<environment_context>") {
			return true
		}
		for _, part := range content.Array() {
			if part.Get("type").String() == "input_text" && strings.HasPrefix(strings.TrimSpace(part.Get("text").String()), "<environment_context>") {
				return true
			}
		}
	}
	for _, tool := range gjson.GetBytes(body, "tools").Array() {
		kind := tool.Get("type").String()
		if kind == "web_search" || strings.HasPrefix(kind, "web_search_") {
			return true
		}
	}
	return false
}

func (s *OpenAIGatewayService) applyAccountTimezone(ctx context.Context, account *Account, body []byte) []byte {
	if !isAccountTimezoneEligible(account) || !hasAccountTimezoneFields(body) {
		return body
	}
	defaultZone := ""
	if s != nil && s.settingService != nil {
		defaultZone = s.settingService.GetOpenAIOAuthDefaultTimezone(ctx)
	}
	fingerprint := timezoneProxyFingerprint(account.Proxy)
	align := func(state *AccountTimezoneState) []byte {
		zone, _ := resolveAccountTimezone(state, fingerprint, defaultZone)
		return alignAccountTimezone(body, zone, time.Now())
	}
	state := accountTimezoneState(account, account.Proxy)
	if s != nil && s.accountTimezoneManager != nil {
		fresh, err := s.accountTimezoneManager.GetAccountTimezone(ctx, account.ID)
		if err != nil || fresh == nil {
			return align(nil)
		}
		state = fresh
		// A long-lived WS connection may still use the previous proxy.
		if state.ProxyFingerprint != fingerprint {
			return align(nil)
		}
		if state.Source != "proxy" && state.HasProxy {
			key := strconv.FormatInt(account.ID, 10) + ":" + fingerprint
			if failed, ok := s.accountTimezoneFailures.Load(key); ok {
				if until, valid := failed.(time.Time); valid && time.Now().Before(until) {
					return align(state)
				}
				s.accountTimezoneFailures.Delete(key)
			}
			result, err, _ := s.accountTimezoneFlight.Do(key, func() (any, error) {
				return s.accountTimezoneManager.DetectAccountTimezone(ctx, account.ID, false)
			})
			if err != nil {
				s.accountTimezoneFailures.Store(key, time.Now().Add(5*time.Minute))
				return align(state)
			}
			s.accountTimezoneFailures.Delete(key)
			detected, ok := result.(*AccountTimezoneState)
			if !ok || detected == nil {
				return align(state)
			}
			state = detected
			if state.ProxyFingerprint != fingerprint {
				return align(nil)
			}
		}
	}
	return align(state)
}

func alignAccountTimezone(body []byte, zone string, now time.Time) []byte {
	if !validAccountTimezone(zone) || !gjson.ValidBytes(body) {
		return body
	}
	loc, _ := time.LoadLocation(zone)
	date := now.In(loc).Format("2006-01-02")
	// sjson changes only selected strings and retains raw number representations.
	result := body
	alignText := func(path string, text gjson.Result) {
		if text.Type != gjson.String {
			return
		}
		aligned := alignTimezoneEnvironment(text.String(), date, zone)
		if aligned == text.String() {
			return
		}
		if next, err := sjson.SetBytes(result, path, aligned); err == nil {
			result = next
		}
	}
	for i, item := range gjson.GetBytes(body, "input").Array() {
		if item.Get("role").String() != "user" {
			continue
		}
		content := item.Get("content")
		path := "input." + strconv.Itoa(i) + ".content"
		if content.Type == gjson.String {
			alignText(path, content)
		} else if content.IsArray() {
			for j, part := range content.Array() {
				if part.Get("type").String() == "input_text" {
					alignText(path+"."+strconv.Itoa(j)+".text", part.Get("text"))
				}
			}
		}
	}
	for i, tool := range gjson.GetBytes(body, "tools").Array() {
		kind := tool.Get("type").String()
		if kind != "web_search" && !strings.HasPrefix(kind, "web_search_") {
			continue
		}
		path := "tools." + strconv.Itoa(i) + ".user_location"
		location := tool.Get("user_location")
		if !location.IsObject() {
			if next, err := sjson.SetBytes(result, path, map[string]string{"type": "approximate", "timezone": zone}); err == nil {
				result = next
			}
		} else {
			if next, err := sjson.SetBytes(result, path+".timezone", zone); err == nil {
				result = next
			}
		}
	}
	return result
}

// Validate XML first, then replace direct children by byte range so unrelated
// fields, whitespace and escaped text retain their original representation.
func alignTimezoneEnvironment(text, date, zone string) string {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "<environment_context>") || !strings.HasSuffix(trimmed, "</environment_context>") {
		return text
	}
	type replacement struct {
		start, end int
		value      string
	}
	var edits []replacement
	//nolint:gosec // Token-only parsing; directives and processing instructions are rejected below, with no custom entities or object decoding.
	decoder := xml.NewDecoder(strings.NewReader(text))
	depth, start := 0, 0
	name := ""
	roots := 0
	for {
		offset := int(decoder.InputOffset())
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return text
		}
		switch token := token.(type) {
		case xml.StartElement:
			depth++
			if depth == 1 {
				roots++
				if token.Name.Local != "environment_context" || token.Name.Space != "" || roots != 1 {
					return text
				}
			}
			if depth == 2 && token.Name.Space == "" && (token.Name.Local == "timezone" || token.Name.Local == "current_date") {
				start, name = offset, token.Name.Local
			}
		case xml.EndElement:
			if depth == 2 && name != "" {
				value := zone
				if name == "current_date" {
					value = date
				}
				edits = append(edits, replacement{start, int(decoder.InputOffset()), "<" + name + ">" + value + "</" + name + ">"})
				name = ""
			}
			depth--
		case xml.Directive, xml.ProcInst:
			return text
		case xml.CharData:
			if depth == 0 && strings.TrimSpace(string(token)) != "" {
				return text
			}
		}
	}
	if depth != 0 || roots != 1 {
		return text
	}
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		text = text[:e.start] + e.value + text[e.end:]
	}
	return text
}
