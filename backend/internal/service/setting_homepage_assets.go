package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	MaxHomepageAssets          = 32
	MaxHomepageAssetBytes      = 2 << 20
	MaxHomepageTotalAssetBytes = 8 << 20
)

var (
	homepageAssetName         = regexp.MustCompile(`^[\pL\pN][\pL\pN._ -]*\.[A-Za-z0-9]+$`)
	ErrHomepageAssetNotFound  = infraerrors.NotFound("HOMEPAGE_ASSET_NOT_FOUND", "homepage asset not found")
	ErrHomepageAssetInvalid   = infraerrors.BadRequest("HOMEPAGE_ASSET_INVALID", "invalid homepage asset name, contents, or reference")
	ErrHomepageAssetsTooLarge = infraerrors.BadRequest("HOMEPAGE_ASSETS_TOO_LARGE", "homepage resources exceed the file count or size limit")
)

type HomepageAsset struct {
	Name        string `json:"name"`
	ContentType string `json:"content_type"`
	Size        int    `json:"size"`
	Digest      string `json:"digest"`
}

type HomepageAssetInput struct {
	Name string `json:"name"`
	// Nil keeps an existing file. A non-nil value uploads a new or replacement file.
	Data []byte `json:"data"`
}

type homepageAssetReader interface {
	ReadHomepageAsset(ctx context.Context, digest string) ([]byte, error)
}

func ValidHomepageAssetName(name string) bool {
	if len(name) > 180 || !homepageAssetName.MatchString(name) {
		return false
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif", ".css", ".woff2":
		return true
	default:
		return false
	}
}

func (s *SettingService) PublicHomepageAsset(ctx context.Context, name string) (HomepageAsset, []byte, error) {
	cfg, backendOnly, err := s.PublicHomepage(ctx)
	if err != nil {
		return HomepageAsset{}, nil, err
	}
	if !cfg.Enabled || backendOnly || cfg.PublicOrigin == "" || cfg.HTML == "" {
		return HomepageAsset{}, nil, ErrHomepageAssetNotFound
	}
	for _, asset := range cfg.Assets {
		if asset.Name == name {
			repo, ok := s.settingRepo.(homepageAssetReader)
			if !ok {
				return HomepageAsset{}, nil, fmt.Errorf("homepage settings repository does not support asset reads")
			}
			data, err := repo.ReadHomepageAsset(ctx, asset.Digest)
			return asset, data, err
		}
	}
	return HomepageAsset{}, nil, ErrHomepageAssetNotFound
}

func prepareHomepageAssets(current []HomepageAsset, input []HomepageAssetInput) ([]HomepageAsset, map[string][]byte, error) {
	if len(input) > MaxHomepageAssets {
		return nil, nil, ErrHomepageAssetsTooLarge
	}
	existing := make(map[string]HomepageAsset, len(current))
	for _, asset := range current {
		existing[asset.Name] = asset
	}
	var assets []HomepageAsset
	uploads := make(map[string][]byte)
	seen := make(map[string]bool, len(input))
	total := 0
	for _, file := range input {
		if !ValidHomepageAssetName(file.Name) || seen[file.Name] {
			return nil, nil, ErrHomepageAssetInvalid
		}
		seen[file.Name] = true
		asset, exists := existing[file.Name]
		if file.Data != nil {
			if len(file.Data) > MaxHomepageAssetBytes {
				return nil, nil, ErrHomepageAssetsTooLarge
			}
			contentType := homepageAssetContentType(file.Name, file.Data)
			if contentType == "" {
				return nil, nil, ErrHomepageAssetInvalid
			}
			hash := sha256.Sum256(file.Data)
			digest := hex.EncodeToString(hash[:])
			asset = HomepageAsset{Name: file.Name, ContentType: contentType, Size: len(file.Data), Digest: digest}
			uploads[digest] = file.Data
		} else if !exists {
			return nil, nil, ErrHomepageAssetInvalid
		}
		total += asset.Size
		if total > MaxHomepageTotalAssetBytes {
			return nil, nil, ErrHomepageAssetsTooLarge
		}
		assets = append(assets, asset)
	}
	slices.SortFunc(assets, func(a, b HomepageAsset) int { return strings.Compare(a.Name, b.Name) })
	return assets, uploads, nil
}

func homepageAssetContentType(name string, data []byte) string {
	if len(data) == 0 {
		return ""
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".css":
		if utf8.Valid(data) && !slices.Contains(data, 0) {
			return "text/css; charset=utf-8"
		}
	case ".woff2":
		if len(data) >= 48 && string(data[:4]) == "wOF2" {
			return "font/woff2"
		}
	case ".jpg", ".jpeg", ".png", ".webp", ".gif":
		switch contentType := http.DetectContentType(data); {
		case contentType == "image/jpeg" && (strings.EqualFold(path.Ext(name), ".jpg") || strings.EqualFold(path.Ext(name), ".jpeg")):
			return contentType
		case contentType == "image/"+strings.ToLower(path.Ext(name)[1:]):
			return contentType
		}
	}
	return ""
}
