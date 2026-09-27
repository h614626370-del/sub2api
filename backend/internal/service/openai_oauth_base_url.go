package service

import (
	"context"
	"net/url"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
)

const openAICodexBaseURL = "https://chatgpt.com/backend-api/codex"

func (s *OpenAIGatewayService) resolveOpenAIOAuthURL(ctx context.Context, account *Account, defaultURL string) (string, error) {
	credentialAccount, err := resolveCredentialAccount(ctx, s.accountRepo, account)
	if err != nil {
		return "", err
	}
	return resolveOpenAIOAuthURL(credentialAccount, defaultURL, s.validateUpstreamBaseURL)
}

// The override applies only to Codex endpoints, never OAuth or account management.
func normalizeOpenAIOAuthBaseURL(account *Account) error {
	if account == nil || account.Platform != PlatformOpenAI || account.Type != AccountTypeOAuth {
		return nil
	}
	value, exists := account.Credentials["base_url"]
	if !exists {
		return nil
	}
	raw, ok := value.(string)
	if !ok {
		return infraerrors.BadRequest("INVALID_OPENAI_OAUTH_BASE_URL", "base_url must be a string")
	}
	base, err := parseOpenAIOAuthBaseURL(raw)
	if err != nil {
		return err
	}
	if base == "" {
		delete(account.Credentials, "base_url")
	} else {
		account.Credentials["base_url"] = base
	}
	return nil
}

func parseOpenAIOAuthBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	invalid := func() (string, error) {
		return "", infraerrors.BadRequest("INVALID_OPENAI_OAUTH_BASE_URL", "base_url must be an HTTP(S) Codex base URL without userinfo, query or fragment")
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return invalid()
	}
	normalized, err := urlvalidator.ValidateURLFormat(raw, true)
	if err != nil {
		return invalid()
	}
	// Accept a pasted Responses endpoint as well as its base path.
	return strings.TrimSuffix(strings.TrimRight(normalized, "/"), "/responses"), nil
}

func resolveOpenAIOAuthURL(account *Account, defaultURL string, validate func(string) (string, error)) (string, error) {
	if account == nil || account.Platform != PlatformOpenAI || account.Type != AccountTypeOAuth {
		return defaultURL, nil
	}
	base, err := parseOpenAIOAuthBaseURL(account.GetCredential("base_url"))
	if err != nil {
		return "", err
	}
	if base == "" {
		return defaultURL, nil
	}
	base, err = validate(base)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(base, "/") + strings.TrimPrefix(defaultURL, openAICodexBaseURL), nil
}
