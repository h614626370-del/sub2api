package routes

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type homepageReaderStub struct {
	cfg         service.HomepageConfig
	backendOnly bool
	err         error
	reads       int
	assets      map[string][]byte
}

func (s *homepageReaderStub) PublicHomepageAsset(_ context.Context, name string) (service.HomepageAsset, []byte, error) {
	data, ok := s.assets[name]
	if !ok {
		return service.HomepageAsset{}, nil, service.ErrHomepageAssetNotFound
	}
	return service.HomepageAsset{Name: name, ContentType: "image/png", Digest: "asset-digest"}, data, nil
}

func TestHomepageAssetResponse(t *testing.T) {
	data := []byte("image bytes")
	stub := &homepageReaderStub{assets: map[string][]byte{"背景 图.png": data, "reserved.png": data, "routed.png": data}}
	router := gin.New()
	router.Use(HomepageDocuments(stub, func(name string) bool { return name == "reserved.png" }))
	router.GET("/routed.png", func(c *gin.Context) { c.String(200, "route") })
	router.NoRoute(func(c *gin.Context) { c.String(200, "SPA") })
	request := func(method, path, etag string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("If-None-Match", etag)
		r := httptest.NewRecorder()
		router.ServeHTTP(r, req)
		return r
	}
	const path = "/homepage-assets/%E8%83%8C%E6%99%AF%20%E5%9B%BE.png"
	got := request(http.MethodGet, path, "")
	require.Equal(t, http.StatusOK, got.Code)
	require.Equal(t, data, got.Body.Bytes())
	require.Equal(t, "image/png", got.Header().Get("Content-Type"))
	require.Equal(t, "nosniff", got.Header().Get("X-Content-Type-Options"))
	require.Equal(t, "public, no-cache", got.Header().Get("Cache-Control"))
	require.Empty(t, request(http.MethodHead, path, "").Body.String())
	require.Equal(t, http.StatusNotModified, request(http.MethodGet, path, got.Header().Get("ETag")).Code)
	require.Equal(t, http.StatusNotFound, request(http.MethodGet, "/homepage-assets/no.png", "").Code)
	require.Equal(t, http.StatusNotFound, request(http.MethodGet, "/homepage-assets/../x.png", "").Code)
	require.Equal(t, http.StatusMethodNotAllowed, request(http.MethodPost, path, "").Code)
	require.Equal(t, data, request(http.MethodGet, strings.TrimPrefix(path, "/homepage-assets"), "").Body.Bytes())
	require.Equal(t, "SPA", request(http.MethodGet, "/reserved.png", "").Body.String())
	require.Equal(t, "SPA", request(http.MethodGet, "/unknown.png", "").Body.String())
	require.Equal(t, "route", request(http.MethodGet, "/routed.png", "").Body.String())
}

func (s *homepageReaderStub) PublicHomepage(context.Context) (service.HomepageConfig, bool, error) {
	s.reads++
	return s.cfg, s.backendOnly, s.err
}

func TestHomepageDocuments(t *testing.T) {
	stub := &homepageReaderStub{cfg: service.HomepageConfig{
		Enabled: true, PublicOrigin: "https://public.example", HTML: "<!doctype html><title>Published</title>",
	}}
	router := gin.New()
	router.Use(HomepageDocuments(stub, nil))
	router.NoRoute(func(c *gin.Context) { c.String(200, "SPA") })
	request := func(method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://untrusted-host.example"+path, nil)
		r := httptest.NewRecorder()
		router.ServeHTTP(r, req)
		return r
	}
	r := request(http.MethodGet, "/?utm_source=guide")
	require.Equal(t, 200, r.Code)
	require.Equal(t, stub.cfg.HTML, r.Body.String())
	require.Contains(t, r.Header().Get("Content-Security-Policy"), "script-src 'none'")
	require.Equal(t, "no-store", r.Header().Get("Cache-Control"))
	r = request(http.MethodHead, "/")
	require.Equal(t, 200, r.Code)
	require.Empty(t, r.Body.String())
	r = request(http.MethodGet, "/home/?ref=abc%2Fdef&utm_source=guide")
	require.Equal(t, 301, r.Code)
	require.Equal(t, "/?ref=abc%2Fdef&utm_source=guide", r.Header().Get("Location"))
	r = request(http.MethodGet, "/sitemap.xml")
	require.Equal(t, 200, r.Code)
	require.Equal(t, "application/xml; charset=utf-8", r.Header().Get("Content-Type"))
	var sitemap homepageSitemap
	require.NoError(t, xml.Unmarshal(r.Body.Bytes(), &sitemap))
	require.Equal(t, "https://public.example/", sitemap.URL)
	r = request(http.MethodGet, "/robots.txt")
	require.Contains(t, r.Body.String(), "Sitemap: https://public.example/sitemap.xml")
	require.NotContains(t, r.Body.String(), "untrusted-host.example")
	before := stub.reads
	require.Equal(t, "SPA", request(http.MethodGet, "/api/example").Body.String())
	require.Equal(t, "SPA", request(http.MethodPost, "/").Body.String())
	require.Equal(t, before, stub.reads)
	stub.backendOnly = true
	require.Equal(t, "SPA", request(http.MethodGet, "/").Body.String())
	require.Equal(t, 404, request(http.MethodGet, "/sitemap.xml").Code)
	require.Equal(t, "User-agent: *\nDisallow: /\n", request(http.MethodGet, "/robots.txt").Body.String())
	stub.backendOnly, stub.cfg.Enabled = false, false
	require.Equal(t, "SPA", request(http.MethodGet, "/home").Body.String())
	require.Equal(t, 404, request(http.MethodGet, "/sitemap.xml").Code)
	stub.err = errors.New("database unavailable")
	require.Equal(t, "SPA", request(http.MethodGet, "/").Body.String())
	require.Equal(t, 503, request(http.MethodGet, "/robots.txt").Code)
}

func TestHomepageSessionRedirect(t *testing.T) {
	stub := &homepageReaderStub{cfg: service.HomepageConfig{
		Enabled:      true,
		PublicOrigin: "https://public.example",
		HTML:         "<!doctype html><head>" + homepageSessionMarker + "<script>custom()</script></head>",
	}}
	router := gin.New()
	router.Use(HomepageDocuments(stub, nil))
	router.NoRoute(func(c *gin.Context) { c.String(200, "SPA") })

	for _, tc := range []struct {
		name, path, body, policy string
	}{
		{"signed-in routing", "/", "<!doctype html><head><script>" + homepageSessionScript + "</script><script>custom()</script></head>", homepageSessionCSP},
		{"public preview", "/?landing=1", stub.cfg.HTML, homepageCSP},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRecorder()
			router.ServeHTTP(r, httptest.NewRequest(http.MethodGet, tc.path, nil))
			require.Equal(t, http.StatusOK, r.Code)
			require.Equal(t, tc.body, r.Body.String())
			require.Equal(t, tc.policy, r.Header().Get("Content-Security-Policy"))
			require.Equal(t, "no-store", r.Header().Get("Cache-Control"))
		})
	}
	hash := sha256.Sum256([]byte(homepageSessionScript))
	require.Contains(t, homepageSessionCSP, "script-src 'sha256-"+base64.StdEncoding.EncodeToString(hash[:])+"';")
	require.Contains(t, homepageSessionCSP, "frame-ancestors 'none'")
}
