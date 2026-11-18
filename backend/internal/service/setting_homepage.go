package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"golang.org/x/net/html"
)

const (
	SettingKeyHomepage = "standalone_homepage"
	MaxHomepageBytes   = 1 << 20
)

var (
	ErrHomepageConflict = infraerrors.Conflict("HOMEPAGE_CONFLICT", "homepage changed; reload before saving")
	ErrHomepageNoBackup = infraerrors.Conflict("HOMEPAGE_NO_BACKUP", "no previous homepage is available")
	homepageHostname    = regexp.MustCompile(`^(?i:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)(?:\.(?i:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?))*$`)
)

type HomepageConfig struct {
	Enabled      bool   `json:"enabled"`
	PublicOrigin string `json:"public_origin"`
	HTML         string `json:"html"`
}

type HomepageState struct {
	HomepageConfig
	Revision    int64    `json:"revision"`
	HasPrevious bool     `json:"has_previous"`
	BackendOnly bool     `json:"backend_only"`
	Warnings    []string `json:"warnings"`
}

type homepageRecord struct {
	Current  HomepageConfig  `json:"current"`
	Previous *HomepageConfig `json:"previous,omitempty"`
	Revision int64           `json:"revision"`
}

// Match the repository's optimistic-write pattern so two control-plane instances
// cannot overwrite each other's publication or its rollback snapshot.
type homepageCompareAndSetter interface {
	CompareAndSetHomepage(ctx context.Context, value, previous string) error
}

func (s *SettingService) readHomepage(ctx context.Context) (homepageRecord, string, bool, error) {
	values, err := s.settingRepo.GetMultiple(ctx, []string{SettingKeyHomepage, SettingKeyBackendModeEnabled})
	if err != nil {
		return homepageRecord{}, "", false, err
	}
	raw := values[SettingKeyHomepage]
	backendOnly := values[SettingKeyBackendModeEnabled] == "true"
	var record homepageRecord
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &record); err != nil {
			return homepageRecord{}, "", backendOnly, fmt.Errorf("decode homepage settings: %w", err)
		}
	}
	return record, raw, backendOnly, nil
}

func (s *SettingService) PublicHomepage(ctx context.Context) (HomepageConfig, bool, error) {
	record, _, backendOnly, err := s.readHomepage(ctx)
	return record.Current, backendOnly, err
}

func (s *SettingService) GetHomepage(ctx context.Context) (HomepageState, error) {
	record, _, backendOnly, err := s.readHomepage(ctx)
	if err != nil {
		return HomepageState{}, err
	}
	return record.state(backendOnly), nil
}

func (s *SettingService) SaveHomepage(ctx context.Context, revision int64, config HomepageConfig) (HomepageState, error) {
	return s.mutateHomepage(ctx, revision, &config)
}

func (s *SettingService) RestoreHomepage(ctx context.Context, revision int64) (HomepageState, error) {
	return s.mutateHomepage(ctx, revision, nil)
}

func (s *SettingService) mutateHomepage(ctx context.Context, revision int64, next *HomepageConfig) (HomepageState, error) {
	record, previousRaw, backendOnly, err := s.readHomepage(ctx)
	if err != nil {
		return HomepageState{}, err
	}
	if revision != record.Revision {
		return HomepageState{}, ErrHomepageConflict
	}
	if next == nil {
		if record.Previous == nil {
			return HomepageState{}, ErrHomepageNoBackup
		}
		next = record.Previous
	}
	if err := validateHomepage(next); err != nil {
		return HomepageState{}, err
	}
	if record.Current == *next {
		return record.state(backendOnly), nil
	}
	previous := record.Current
	record.Current = *next
	record.Previous = &previous
	record.Revision++
	value, err := json.Marshal(record)
	if err != nil {
		return HomepageState{}, err
	}
	repo, ok := s.settingRepo.(homepageCompareAndSetter)
	if !ok {
		return HomepageState{}, fmt.Errorf("homepage settings repository does not support atomic writes")
	}
	if err := repo.CompareAndSetHomepage(ctx, string(value), previousRaw); err != nil {
		return HomepageState{}, err
	}
	return record.state(backendOnly), nil
}

func (r homepageRecord) state(backendOnly bool) HomepageState {
	_, warnings := inspectHomepage(r.Current)
	return HomepageState{
		HomepageConfig: r.Current,
		Revision:       r.Revision,
		HasPrevious:    r.Previous != nil,
		BackendOnly:    backendOnly,
		Warnings:       warnings,
	}
}

func validateHomepage(cfg *HomepageConfig) error {
	origin := strings.TrimSpace(cfg.PublicOrigin)
	if origin != "" {
		u, err := url.Parse(origin)
		if len(origin) > 320 || err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil ||
			(u.Path != "" && u.Path != "/") || u.RawPath != "" || u.ForceQuery ||
			strings.Contains(origin, "#") || u.RawQuery != "" || strings.HasSuffix(u.Host, ":") ||
			(net.ParseIP(u.Hostname()) == nil && !homepageHostname.MatchString(u.Hostname())) {
			return infraerrors.BadRequest("HOMEPAGE_INVALID_ORIGIN", "enter an HTTP or HTTPS origin without a path, query or fragment")
		}
		if p := u.Port(); p != "" {
			port, err := strconv.ParseUint(p, 10, 16)
			if err != nil || port == 0 {
				return infraerrors.BadRequest("HOMEPAGE_INVALID_ORIGIN", "invalid public origin port")
			}
		}
		cfg.PublicOrigin = strings.ToLower(u.Scheme + "://" + u.Host)
	} else {
		cfg.PublicOrigin = ""
	}
	if len(cfg.HTML) > MaxHomepageBytes {
		return infraerrors.BadRequest("HOMEPAGE_TOO_LARGE", "homepage HTML must not exceed 1 MiB")
	}
	if !utf8.ValidString(cfg.HTML) || strings.ContainsRune(cfg.HTML, 0) {
		return infraerrors.BadRequest("HOMEPAGE_INVALID_HTML", "homepage HTML must be valid UTF-8")
	}
	if cfg.Enabled && (cfg.PublicOrigin == "" || strings.TrimSpace(cfg.HTML) == "") {
		return infraerrors.BadRequest("HOMEPAGE_REQUIRED", "public origin and HTML are required to enable the homepage")
	}
	if strings.TrimSpace(cfg.HTML) == "" {
		cfg.HTML = ""
		return nil
	}
	complete, _ := inspectHomepage(*cfg)
	if !complete {
		return infraerrors.BadRequest("HOMEPAGE_INVALID_DOCUMENT", "include an HTML doctype and omit meta refresh")
	}
	return nil
}

func inspectHomepage(cfg HomepageConfig) (bool, []string) {
	warnings := []string{}
	if cfg.HTML == "" {
		return false, warnings
	}
	doc, err := html.Parse(strings.NewReader(cfg.HTML))
	if err != nil {
		return false, warnings
	}
	doctype, safe, script := false, true, false
	for node := range doc.Descendants() {
		switch {
		case node.Type == html.DoctypeNode && node.Data == "html":
			doctype = true
		case node.Type != html.ElementNode:
			continue
		case node.Data == "meta" && strings.EqualFold(strings.TrimSpace(homepageAttr(node, "http-equiv")), "refresh"):
			safe = false
		case node.Data == "script" && !strings.EqualFold(strings.TrimSpace(homepageAttr(node, "type")), "application/ld+json"):
			script = true
		}
	}
	hasTitle, hasDescription := false, false
	var canonicals []string
	head := doc.FirstChild
	for head != nil && (head.Data != "html" || head.Type != html.ElementNode) {
		head = head.NextSibling
	}
	if head != nil {
		head = head.FirstChild
	}
	if head != nil && head.Data == "head" {
		for child := range head.ChildNodes() {
			if child.Type != html.ElementNode {
				continue
			}
			switch child.Data {
			case "title":
				hasTitle = hasTitle || child.FirstChild != nil && strings.TrimSpace(child.FirstChild.Data) != ""
			case "meta":
				hasDescription = hasDescription || strings.EqualFold(homepageAttr(child, "name"), "description") &&
					strings.TrimSpace(homepageAttr(child, "content")) != ""
			case "link":
				for _, rel := range strings.Fields(homepageAttr(child, "rel")) {
					if strings.EqualFold(rel, "canonical") {
						canonicals = append(canonicals, strings.TrimSpace(homepageAttr(child, "href")))
						break
					}
				}
			}
		}
	}
	if !hasTitle {
		warnings = append(warnings, "missing_title")
	}
	if !hasDescription {
		warnings = append(warnings, "missing_description")
	}
	switch {
	case len(canonicals) == 0:
		warnings = append(warnings, "missing_canonical")
	case len(canonicals) > 1:
		warnings = append(warnings, "duplicate_canonical")
	case cfg.PublicOrigin != "" && canonicals[0] != cfg.PublicOrigin+"/" && canonicals[0] != cfg.PublicOrigin:
		warnings = append(warnings, "canonical_mismatch")
	}
	if script {
		warnings = append(warnings, "scripts_blocked")
	}
	return doctype && safe, warnings
}

func homepageAttr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}
