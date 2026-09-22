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

// Only explicit environment metadata and structured search tools are eligible.
// Ordinary messages, instructions, timestamps and unrelated platforms stay intact.
func hasAccountTimezoneFields(body []byte) bool {
	if strings.Contains(string(body), "environments.environment_context") {
		return true
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
	if account == nil || !account.IsOpenAI() || !hasAccountTimezoneFields(body) {
		return body
	}
	state := accountTimezoneState(account, account.Proxy)
	if s != nil && s.accountTimezoneManager != nil {
		fresh, err := s.accountTimezoneManager.GetAccountTimezone(ctx, account.ID)
		if err != nil {
			return body
		}
		state = fresh
		// A long-lived WS connection may still use the previous proxy.
		if state.Source != "manual" && state.ProxyFingerprint != timezoneProxyFingerprint(account.Proxy) {
			return body
		}
		if state.Timezone == "" && state.HasProxy {
			key := strconv.FormatInt(account.ID, 10) + ":" + timezoneProxyFingerprint(account.Proxy)
			if failed, ok := s.accountTimezoneFailures.Load(key); ok {
				if until, valid := failed.(time.Time); valid && time.Now().Before(until) {
					return body
				}
				s.accountTimezoneFailures.Delete(key)
			}
			result, err, _ := s.accountTimezoneFlight.Do(key, func() (any, error) {
				return s.accountTimezoneManager.DetectAccountTimezone(ctx, account.ID, false)
			})
			if err != nil {
				s.accountTimezoneFailures.Store(key, time.Now().Add(5*time.Minute))
				return body
			}
			s.accountTimezoneFailures.Delete(key)
			detected, ok := result.(*AccountTimezoneState)
			if !ok || detected == nil {
				return body
			}
			state = detected
			if state.Source != "manual" && state.ProxyFingerprint != timezoneProxyFingerprint(account.Proxy) {
				return body
			}
		}
	}
	return alignAccountTimezone(body, state.Timezone, time.Now())
}

func alignAccountTimezone(body []byte, zone string, now time.Time) []byte {
	if !validAccountTimezone(zone) || !gjson.ValidBytes(body) {
		return body
	}
	loc, _ := time.LoadLocation(zone)
	date := now.In(loc).Format("2006-01-02")
	// sjson changes only selected strings and retains raw number representations.
	result := body
	for i, item := range gjson.GetBytes(body, "input").Array() {
		if item.Get("role").String() != "user" {
			continue
		}
		kinds := item.Get("internal_chat_message_metadata_passthrough.content_item_kinds").Array()
		for j, part := range item.Get("content").Array() {
			if j >= len(kinds) || kinds[j].String() != "environments.environment_context" || part.Get("type").String() != "input_text" {
				continue
			}
			text := part.Get("text")
			if text.Type != gjson.String {
				continue
			}
			aligned := alignTimezoneEnvironment(text.String(), date, zone)
			if aligned == text.String() {
				continue
			}
			path := "input." + strconv.Itoa(i) + ".content." + strconv.Itoa(j) + ".text"
			if next, err := sjson.SetBytes(result, path, aligned); err == nil {
				result = next
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
