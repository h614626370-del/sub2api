package routes

import (
	"context"
	"encoding/xml"
	"errors"
	"net/http"
	"net/http/httptest"
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
	router.Use(HomepageDocuments(stub))
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
