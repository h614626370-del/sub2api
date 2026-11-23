package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type homepageRepoStub struct {
	SettingRepository
	raw, backend string
	writeError   error
	assets       map[string][]byte
}

func (s *homepageRepoStub) GetMultiple(context.Context, []string) (map[string]string, error) {
	return map[string]string{SettingKeyHomepage: s.raw, SettingKeyBackendModeEnabled: s.backend}, nil
}

func (s *homepageRepoStub) CompareAndSetHomepage(_ context.Context, value, old string, uploads map[string][]byte, retained []string) error {
	if s.writeError != nil {
		return s.writeError
	}
	if s.raw != old {
		return ErrHomepageConflict
	}
	next := make(map[string][]byte)
	for _, digest := range retained {
		if data, ok := uploads[digest]; ok {
			next[digest] = data
		} else {
			next[digest] = s.assets[digest]
		}
	}
	s.assets = next
	s.raw = value
	return nil
}

func (s *homepageRepoStub) ReadHomepageAsset(_ context.Context, digest string) ([]byte, error) {
	if data, ok := s.assets[digest]; ok {
		return data, nil
	}
	return nil, ErrHomepageAssetNotFound
}

func TestHomepageAssetsFollowPublishedSnapshot(t *testing.T) {
	ctx := context.Background()
	repo := &homepageRepoStub{}
	svc := &SettingService{settingRepo: repo}
	page := HomepageConfig{Enabled: true, PublicOrigin: "https://public.example", HTML: "<!doctype html><title>Home</title>", EntryFile: "index.html"}
	first := []HomepageAssetInput{
		{Name: "hero.jpg", Data: []byte{0xff, 0xd8, 0xff, 0xe0}},
		{Name: "home.css", Data: []byte("body { color: black }")},
	}
	saved, err := svc.SaveHomepageWithAssets(ctx, 0, page, &first)
	require.NoError(t, err)
	require.Len(t, saved.Assets, 2)
	require.Equal(t, "index.html", saved.EntryFile)
	asset, data, err := svc.PublicHomepageAsset(ctx, "hero.jpg")
	require.NoError(t, err)
	require.Equal(t, "image/jpeg", asset.ContentType)
	require.Equal(t, first[0].Data, data)

	replacement := []HomepageAssetInput{{Name: "hero.jpg", Data: []byte{0xff, 0xd8, 0xff, 0xe1}}}
	page.EntryFile = "home.html"
	saved, err = svc.SaveHomepageWithAssets(ctx, 1, page, &replacement)
	require.NoError(t, err)
	require.Equal(t, "home.html", saved.EntryFile)
	require.Len(t, saved.Assets, 1)
	_, data, err = svc.PublicHomepageAsset(ctx, "hero.jpg")
	require.NoError(t, err)
	require.Equal(t, replacement[0].Data, data)
	_, _, err = svc.PublicHomepageAsset(ctx, "home.css")
	require.ErrorIs(t, err, ErrHomepageAssetNotFound)
	saved, err = svc.RestoreHomepage(ctx, 2)
	require.NoError(t, err)
	require.Equal(t, "index.html", saved.EntryFile)
	_, data, err = svc.PublicHomepageAsset(ctx, "hero.jpg")
	require.NoError(t, err)
	require.Equal(t, first[0].Data, data)
	_, _, err = svc.PublicHomepageAsset(ctx, "home.css")
	require.NoError(t, err)

	page.Enabled = false
	page.EntryFile = ""
	saved, err = svc.SaveHomepage(ctx, 3, page)
	require.NoError(t, err)
	require.Equal(t, "index.html", saved.EntryFile)
	require.Len(t, saved.Assets, 2)
	require.Len(t, repo.assets, 2)
	_, _, err = svc.PublicHomepageAsset(ctx, "hero.jpg")
	require.ErrorIs(t, err, ErrHomepageAssetNotFound)
}

func TestHomepageRejectsInvalidAssets(t *testing.T) {
	for _, file := range []HomepageAssetInput{
		{Name: "../logo.png", Data: []byte("invalid")},
		{Name: "logo.svg", Data: []byte("<svg></svg>")},
		{Name: "logo.png", Data: []byte("<html>")},
		{Name: "logo.png"},
	} {
		t.Run(file.Name, func(t *testing.T) {
			_, _, err := prepareHomepageAssets(nil, []HomepageAssetInput{file})
			require.ErrorIs(t, err, ErrHomepageAssetInvalid)
		})
	}
	_, _, err := prepareHomepageAssets(nil, []HomepageAssetInput{{Name: "large.jpg", Data: make([]byte, MaxHomepageAssetBytes+1)}})
	require.ErrorIs(t, err, ErrHomepageAssetsTooLarge)
}

func TestHomepageSaveRestore(t *testing.T) {
	ctx := context.Background()
	repo := &homepageRepoStub{}
	svc := &SettingService{settingRepo: repo}
	initial, err := svc.GetHomepage(ctx)
	require.NoError(t, err)
	require.False(t, initial.Enabled)
	require.False(t, initial.HasPrevious)
	_, err = svc.RestoreHomepage(ctx, 0)
	require.ErrorIs(t, err, ErrHomepageNoBackup)

	page := HomepageConfig{Enabled: true, PublicOrigin: " https://Public.example/ ", HTML: `<!doctype html><html><head><title>Page A</title><link rel="canonical" href="https://public.example/"><meta name="description" content="Description"><script type="application/ld+json">{}</script></head><body>A</body></html>`}
	saved, err := svc.SaveHomepage(ctx, 0, page)
	require.NoError(t, err)
	require.EqualValues(t, 1, saved.Revision)
	require.Equal(t, "https://public.example", saved.PublicOrigin)
	require.Empty(t, saved.Warnings)
	page.HTML = "<!doctype html><title>Page B</title><script>void 0</script>"
	_, err = svc.SaveHomepage(ctx, 0, page)
	require.ErrorIs(t, err, ErrHomepageConflict)
	repo.writeError = errors.New("database unavailable")
	_, err = svc.SaveHomepage(ctx, 1, page)
	require.ErrorIs(t, err, repo.writeError)
	current, _, err := svc.PublicHomepage(ctx)
	require.NoError(t, err)
	require.Contains(t, current.HTML, "Page A")

	repo.writeError = nil
	saved, err = svc.SaveHomepage(ctx, 1, page)
	require.NoError(t, err)
	require.Contains(t, saved.Warnings, "scripts_blocked")
	require.Contains(t, saved.Warnings, "missing_canonical")
	restored, err := svc.RestoreHomepage(ctx, 2)
	require.NoError(t, err)
	require.EqualValues(t, 3, restored.Revision)
	require.Contains(t, restored.HTML, "Page A")
	require.True(t, restored.HasPrevious)
	repo.backend = "true"
	_, backendOnly, err := svc.PublicHomepage(ctx)
	require.NoError(t, err)
	require.True(t, backendOnly)
}

func TestHomepageValidation(t *testing.T) {
	for _, origin := range []string{"https://host/path", "https://host?#", "https://user@host", "//host", "javascript:alert(1)", "https://host:65536", "https://host:0", "https://host%0A", "https://host:", "https://[invalid]"} {
		t.Run(origin, func(t *testing.T) {
			require.Error(t, validateHomepage(&HomepageConfig{PublicOrigin: origin}))
		})
	}
	for _, body := range []string{"", "<p>Fragment", `<!doctype html><meta http-equiv="ReFrEsH" content="0; url=/login">`, strings.Repeat("a", MaxHomepageBytes+1)} {
		require.Error(t, validateHomepage(&HomepageConfig{Enabled: true, PublicOrigin: "https://example.test", HTML: body}))
	}
	_, warnings := inspectHomepage(HomepageConfig{PublicOrigin: "https://example.test", HTML: `<!doctype html><link rel="canonical" href="https://other.test/">`})
	require.Contains(t, warnings, "canonical_mismatch")
	_, warnings = inspectHomepage(HomepageConfig{HTML: `<!doctype html><link rel="canonical" href="/"><link rel="canonical" href="/home">`})
	require.Contains(t, warnings, "duplicate_canonical")
}
