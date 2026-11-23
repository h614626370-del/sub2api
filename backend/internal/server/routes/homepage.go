package routes

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const homepageCSP = "default-src 'none'; script-src 'none'; style-src 'self' https: 'unsafe-inline'; img-src 'self' https: data:; font-src 'self' https: data:; media-src 'self' https:; base-uri 'none'; form-action 'none'; frame-ancestors 'none'; sandbox allow-same-origin allow-popups allow-popups-to-escape-sandbox"

const homepageSessionMarker = `<meta name="sub2api-redirect-signed-in" content="true">`
const homepageAssetPrefix = "/homepage-assets/"

// The SPA login route restores the session and chooses the appropriate console.
const homepageSessionScript = `(() => {
  try {
    if (window.localStorage.getItem("auth_token") && window.localStorage.getItem("auth_user")) {
      window.location.replace("/login");
    }
  } catch {
    // The public homepage remains available if storage is disabled.
  }
})();`

var homepageSessionCSP = func() string {
	hash := sha256.Sum256([]byte(homepageSessionScript))
	policy := strings.Replace(homepageCSP, "script-src 'none'", "script-src 'sha256-"+base64.StdEncoding.EncodeToString(hash[:])+"'", 1)
	return policy + " allow-scripts"
}()

type homepageSiteReader interface {
	PublicHomepage(context.Context) (service.HomepageConfig, bool, error)
	PublicHomepageAsset(context.Context, string) (service.HomepageAsset, []byte, error)
}

type homepageSitemap struct {
	XMLName   xml.Name `xml:"urlset"`
	Namespace string   `xml:"xmlns,attr"`
	URL       string   `xml:"url>loc"`
}

// Serve exact document URLs before the embedded SPA fallback.
func HomepageDocuments(reader homepageSiteReader, embeddedFileExists func(string) bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		if strings.HasPrefix(path, homepageAssetPrefix) || path == strings.TrimSuffix(homepageAssetPrefix, "/") {
			serveHomepageAsset(c, reader, strings.TrimPrefix(path, homepageAssetPrefix), true)
			return
		}
		switch path {
		case "/", "/home", "/home/", "/robots.txt", "/sitemap.xml":
		default:
			// A flat upload can use relative resource URLs without overriding app files or routes.
			name := strings.TrimPrefix(path, "/")
			if c.FullPath() == "" && (c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead) &&
				service.ValidHomepageAssetName(name) && (embeddedFileExists == nil || !embeddedFileExists(name)) {
				serveHomepageAsset(c, reader, name, false)
			}
			return
		}
		if method := c.Request.Method; method != http.MethodGet && method != http.MethodHead {
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		cfg, backendOnly, err := reader.PublicHomepage(ctx)
		if err != nil {
			if path == "/robots.txt" || path == "/sitemap.xml" {
				c.Header("Cache-Control", "no-store")
				c.AbortWithStatus(http.StatusServiceUnavailable)
			}
			return
		}
		active := cfg.Enabled && !backendOnly && cfg.PublicOrigin != "" && cfg.HTML != ""
		switch path {
		case "/":
			if active {
				body, policy := cfg.HTML, homepageCSP
				if c.Query("landing") != "1" && strings.Contains(body, homepageSessionMarker) {
					body = strings.Replace(body, homepageSessionMarker, "<script>"+homepageSessionScript+"</script>", 1)
					policy = homepageSessionCSP
				}
				c.Header("Content-Security-Policy", policy)
				homepageDocument(c, "text/html; charset=utf-8", []byte(body))
			}
		case "/home", "/home/":
			if active {
				target := "/"
				if query := c.Request.URL.RawQuery; query != "" {
					target += "?" + query
				}
				c.Header("Cache-Control", "no-store")
				c.Header("Location", target)
				c.AbortWithStatus(http.StatusMovedPermanently)
			}
		case "/robots.txt":
			robots := "User-agent: *\nDisallow: /admin/\nDisallow: /api/\nDisallow: /dashboard\nDisallow: /keys\n"
			if backendOnly {
				robots = "User-agent: *\nDisallow: /\n"
			} else if active {
				robots += "\nSitemap: " + cfg.PublicOrigin + "/sitemap.xml\n"
			}
			homepageDocument(c, "text/plain; charset=utf-8", []byte(robots))
		case "/sitemap.xml":
			if !active {
				c.Header("Cache-Control", "no-store")
				c.AbortWithStatus(http.StatusNotFound)
				return
			}
			body, err := xml.MarshalIndent(homepageSitemap{
				Namespace: "http://www.sitemaps.org/schemas/sitemap/0.9",
				URL:       cfg.PublicOrigin + "/",
			}, "", "  ")
			if err != nil {
				c.AbortWithStatus(http.StatusInternalServerError)
				return
			}
			homepageDocument(c, "application/xml; charset=utf-8", append([]byte(xml.Header), body...))
		}
	}
}

func serveHomepageAsset(c *gin.Context, reader homepageSiteReader, name string, explicit bool) {
	if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
		c.Header("Allow", "GET, HEAD")
		c.Header("Cache-Control", "no-store")
		c.AbortWithStatus(http.StatusMethodNotAllowed)
		return
	}
	if !service.ValidHomepageAssetName(name) {
		c.Header("Cache-Control", "no-store")
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	asset, data, err := reader.PublicHomepageAsset(ctx, name)
	if err != nil {
		if !explicit {
			return
		}
		c.Header("Cache-Control", "no-store")
		if errors.Is(err, service.ErrHomepageAssetNotFound) {
			c.AbortWithStatus(http.StatusNotFound)
		} else {
			c.AbortWithStatus(http.StatusServiceUnavailable)
		}
		return
	}
	c.Header("Content-Type", asset.ContentType)
	c.Header("Content-Security-Policy", "default-src 'none'; sandbox")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", "public, no-cache")
	c.Header("ETag", `"`+asset.Digest+`"`)
	http.ServeContent(c.Writer, c.Request, name, time.Time{}, bytes.NewReader(data))
	c.Abort()
}

func homepageDocument(c *gin.Context, contentType string, body []byte) {
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Type", contentType)
	c.Header("X-Content-Type-Options", "nosniff")
	if c.Request.Method == http.MethodHead {
		c.AbortWithStatus(http.StatusOK)
		return
	}
	c.Data(http.StatusOK, contentType, body)
	c.Abort()
}

func registerHomepageAdminRoutes(admin *gin.RouterGroup, settings *service.SettingService) {
	group := admin.Group("/homepage")
	group.Use(func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
	})
	group.GET("", func(c *gin.Context) {
		state, err := settings.GetHomepage(c.Request.Context())
		if !response.ErrorFrom(c, err) {
			response.Success(c, state)
		}
	})
	group.PUT("", func(c *gin.Context) {
		var input struct {
			Enabled      bool                          `json:"enabled"`
			PublicOrigin string                        `json:"public_origin"`
			HTML         string                        `json:"html"`
			EntryFile    string                        `json:"entry_file"`
			Revision     *int64                        `json:"revision"`
			Assets       *[]service.HomepageAssetInput `json:"assets"`
		}
		if !bindHomepageRequest(c, &input, 6*service.MaxHomepageBytes+2*service.MaxHomepageTotalAssetBytes+65536) {
			return
		}
		if input.Revision == nil {
			response.BadRequest(c, "revision is required")
			return
		}
		config := service.HomepageConfig{Enabled: input.Enabled, PublicOrigin: input.PublicOrigin, HTML: input.HTML, EntryFile: input.EntryFile}
		state, err := settings.SaveHomepageWithAssets(c.Request.Context(), *input.Revision, config, input.Assets)
		if !response.ErrorFrom(c, err) {
			response.Success(c, state)
		}
	})
	group.POST("/restore", func(c *gin.Context) {
		var input struct {
			Revision *int64 `json:"revision"`
		}
		if !bindHomepageRequest(c, &input, 4096) {
			return
		}
		if input.Revision == nil {
			response.BadRequest(c, "revision is required")
			return
		}
		state, err := settings.RestoreHomepage(c.Request.Context(), *input.Revision)
		if !response.ErrorFrom(c, err) {
			response.Success(c, state)
		}
	})
}

func bindHomepageRequest(c *gin.Context, target any, limit int64) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
	if err := c.ShouldBindJSON(target); err != nil {
		response.BadRequest(c, "invalid homepage request")
		return false
	}
	return true
}
