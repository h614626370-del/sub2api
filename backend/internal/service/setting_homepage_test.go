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
}

func (s *homepageRepoStub) GetMultiple(context.Context, []string) (map[string]string, error) {
	return map[string]string{SettingKeyHomepage: s.raw, SettingKeyBackendModeEnabled: s.backend}, nil
}

func (s *homepageRepoStub) CompareAndSetHomepage(_ context.Context, value, old string) error {
	if s.writeError != nil {
		return s.writeError
	}
	if s.raw != old {
		return ErrHomepageConflict
	}
	s.raw = value
	return nil
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
