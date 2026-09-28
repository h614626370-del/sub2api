package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const (
	OpenAIBPSEnabledKey = "openai_bps_enabled"
	OpenAIBPSModelsKey  = "openai_bps_models"
)

func (a *Account) OpenAIBPSEnabled() bool {
	if a == nil || !a.IsOpenAIOAuth() || a.IsShadow() {
		return false
	}
	enabled, _ := a.Extra[OpenAIBPSEnabledKey].(bool)
	return enabled
}

func (a *Account) UsesOpenAIBPS(model string) bool {
	if a == nil {
		return false
	}
	if a.IsOpenAIBPS() {
		return true // Historical adapter fixtures; public creation is rejected.
	}
	if !a.OpenAIBPSEnabled() {
		return false
	}
	mapped := a.GetMappedModel(model)
	for _, candidate := range bpsModelList(a.Extra[OpenAIBPSModelsKey]) {
		if candidate == mapped {
			return true
		}
	}
	return false
}

func bpsModelList(raw any) []string {
	var result []string
	switch values := raw.(type) {
	case []string:
		result = append(result, values...)
	case []any:
		for _, value := range values {
			if s, ok := value.(string); ok {
				result = append(result, s)
			}
		}
	}
	return result
}

// Normalize only operator-owned fields; credentials remain in the OAuth provider.
func NormalizeOpenAIBPSMode(account *Account, previous map[string]any) error {
	if account == nil {
		return nil
	}
	if account.Extra == nil {
		account.Extra = map[string]any{}
	}
	for _, key := range []string{OpenAIBPSEnabledKey, OpenAIBPSModelsKey} {
		if _, exists := account.Extra[key]; !exists {
			if value, found := previous[key]; found {
				account.Extra[key] = value
			}
		}
	}
	raw, exists := account.Extra[OpenAIBPSEnabledKey]
	enabled, valid := raw.(bool)
	if exists && !valid {
		return fmt.Errorf("openai_bps_enabled must be a boolean")
	}
	if !account.IsOpenAIOAuth() || account.IsShadow() {
		if enabled {
			return fmt.Errorf("BPS mode requires a non-shadow OpenAI OAuth account")
		}
		delete(account.Extra, OpenAIBPSEnabledKey)
		delete(account.Extra, OpenAIBPSModelsKey)
		return nil
	}
	rawModels, supplied := account.Extra[OpenAIBPSModelsKey]
	models := bpsModelList(rawModels)
	if supplied {
		switch values := rawModels.(type) {
		case []string:
		case []any:
			if len(values) != len(models) {
				return fmt.Errorf("openai_bps_models must contain only strings")
			}
		default:
			return fmt.Errorf("openai_bps_models must be an array")
		}
	} else if enabled {
		models = OpenAIBPSDefaultModels()
	}
	normalized := make([]string, 0, len(models))
	seen := map[string]bool{}
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model != "" && !seen[model] {
			if len(model) > 200 || strings.ContainsAny(model, "\r\n*") {
				return fmt.Errorf("BPS model names must be exact model IDs")
			}
			seen[model] = true
			normalized = append(normalized, model)
		}
	}
	if enabled && len(normalized) == 0 {
		return fmt.Errorf("at least one BPS model is required")
	}
	if enabled || supplied {
		account.Extra[OpenAIBPSModelsKey] = normalized
	}
	return nil
}

type openAIChannelContextKey struct{}
type openAIChannelSelection struct {
	selected bool
	bps      bool
}

// A request-scoped lock is shared by retries, but never across requests.
func WithOpenAIChannelSelection(ctx context.Context) context.Context {
	if _, ok := ctx.Value(openAIChannelContextKey{}).(*openAIChannelSelection); ok {
		return ctx
	}
	return context.WithValue(ctx, openAIChannelContextKey{}, &openAIChannelSelection{})
}

func openAIChannelAllowed(ctx context.Context, account *Account, model string) bool {
	selection, _ := ctx.Value(openAIChannelContextKey{}).(*openAIChannelSelection)
	return selection == nil || !selection.selected || selection.bps == account.UsesOpenAIBPS(model)
}

func SelectOpenAIChannel(c *gin.Context, account *Account, model string) error {
	ctx := WithOpenAIChannelSelection(c.Request.Context())
	selection, _ := ctx.Value(openAIChannelContextKey{}).(*openAIChannelSelection)
	bps := account.UsesOpenAIBPS(model)
	if selection.selected && selection.bps != bps {
		return &bpsError{503, "upstream_channel_unavailable", "No available account on the selected upstream channel"}
	}
	selection.selected, selection.bps = true, bps
	c.Request = c.Request.WithContext(ctx)
	return nil
}

func rejectBPSUnsupportedEndpoint(c *gin.Context, account *Account, body []byte) error {
	if !account.UsesOpenAIBPS(gjson.GetBytes(body, "model").String()) {
		return nil
	}
	path := strings.TrimSuffix(c.Request.URL.Path, "/")
	if c.Request.Method == http.MethodPost &&
		(strings.HasSuffix(path, "/responses") || strings.HasSuffix(path, "/responses/compact")) {
		return nil
	}
	err := &bpsError{400, "bps_unsupported_endpoint", "BPS mode supports HTTP Responses and compaction only"}
	WriteOpenAIBPSError(c, err)
	return err
}
