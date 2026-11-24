package tutorial

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/setup"
	"github.com/gin-gonic/gin"
)

const maxAssetBytes = 5 << 20

var assetIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
var assetReferencePattern = regexp.MustCompile(`tutorial-asset:([a-f0-9]{32})\b`)

type Handler struct {
	store    *Store
	assetDir string
}

func NewHandler(db *sql.DB) *Handler {
	dir := setup.GetDataDir()
	return &Handler{store: &Store{db: db}, assetDir: filepath.Join(dir, "tutorials", "assets")}
}
func private(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
}
func (h *Handler) RegisterAdmin(r *gin.RouterGroup) {
	r.Use(private)
	r.GET("", h.list(AdminAccess))
	r.POST("", h.create)
	r.POST("/import", h.importPackage)
	r.GET("/categories", h.categories(AdminAccess))
	r.POST("/categories", h.saveCategory)
	r.PUT("/categories/:category", h.saveCategory)
	r.DELETE("/categories/:category", h.deleteCategory)
	r.GET("/:id", h.get(AdminAccess))
	r.PUT("/:id", h.change("save"))
	r.DELETE("/:id", h.change("delete"))
	r.POST("/:id/import", h.importPackage)
	r.GET("/:id/export", h.exportPackage)
	r.POST("/:id/publish", h.change("publish"))
	r.POST("/:id/unpublish", h.change("unpublish"))
	r.POST("/:id/assets", h.upload)
	r.GET("/:id/assets/:asset", h.asset(AdminAccess))
}
func (h *Handler) RegisterUser(r *gin.RouterGroup) {
	r.Use(private)
	r.GET("", h.list(MemberAccess))
	r.GET("/categories", h.categories(MemberAccess))
	r.GET("/:id", h.get(MemberAccess))
	r.GET("/:id/assets/:asset", h.asset(MemberAccess))
}

// Public reads deliberately never inspect a JWT: only published public snapshots qualify.
func (h *Handler) RegisterPublic(r *gin.RouterGroup) {
	r.Use(private)
	r.GET("", h.list(PublicAccess))
	r.GET("/categories", h.categories(PublicAccess))
	r.GET("/:id", h.get(PublicAccess))
	r.GET("/:id/assets/:asset", h.asset(PublicAccess))
}
func idParam(c *gin.Context, name string) (int64, bool) {
	id, e := strconv.ParseInt(c.Param(name), 10, 64)
	if e != nil || id <= 0 {
		response.BadRequest(c, "Invalid tutorial ID")
		return 0, false
	}
	return id, true
}
func respond(c *gin.Context, data any, err error) {
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, data)
}
func (h *Handler) list(access Access) gin.HandlerFunc {
	return func(c *gin.Context) {
		page, size := response.ParsePagination(c)
		if size > 100 {
			size = 100
		}
		if size < 1 {
			size = 20
		}
		if page < 1 {
			page = 1
		}
		category, _ := strconv.ParseInt(c.Query("category_id"), 10, 64)
		search := c.Query("search")
		if len(search) > 500 {
			response.BadRequest(c, "Search is too long")
			return
		}
		items, total, err := h.store.List(c.Request.Context(), access, search, c.Query("status"), category, page, size)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		response.Paginated(c, items, total, page, size)
	}
}
func (h *Handler) get(access Access) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := idParam(c, "id")
		if !ok {
			return
		}
		a, e := h.store.Get(c.Request.Context(), id, access)
		if e == nil && access != AdminAccess {
			e = h.store.Neighbors(c.Request.Context(), a, access)
			if e == sql.ErrNoRows {
				e = ErrNotFound
			}
		}
		respond(c, a, e)
	}
}
func (h *Handler) create(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2<<20)
	var in Content
	if c.ShouldBindJSON(&in) != nil {
		response.BadRequest(c, "Invalid tutorial")
		return
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "Authentication required")
		return
	}
	a, e := h.store.Create(c.Request.Context(), subject.UserID, in)
	respond(c, a, e)
}
func (h *Handler) change(action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := idParam(c, "id")
		if !ok {
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2<<20)
		var in struct {
			Content
			Version int64 `json:"version"`
		}
		if c.ShouldBindJSON(&in) != nil || in.Version < 1 {
			response.BadRequest(c, "Version is required")
			return
		}
		a, e := h.store.Change(c.Request.Context(), id, in.Version, action, in.Content)
		if e == nil && action == "delete" {
			for _, key := range a.DeletedAssets {
				_ = os.Remove(filepath.Join(h.assetDir, key))
			}
		}
		respond(c, a, e)
	}
}
func (h *Handler) categories(access Access) gin.HandlerFunc {
	return func(c *gin.Context) {
		items, e := h.store.Categories(c.Request.Context(), access)
		respond(c, items, e)
	}
}
func (h *Handler) saveCategory(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	var in Category
	if c.ShouldBindJSON(&in) != nil {
		response.BadRequest(c, "Invalid category")
		return
	}
	var id int64
	if c.Param("category") != "" {
		var ok bool
		id, ok = idParam(c, "category")
		if !ok {
			return
		}
	}
	a, e := h.store.SaveCategory(c.Request.Context(), id, in)
	respond(c, a, e)
}
func (h *Handler) deleteCategory(c *gin.Context) {
	id, ok := idParam(c, "category")
	if !ok {
		return
	}
	respond(c, gin.H{"deleted": true}, h.store.DeleteCategory(c.Request.Context(), id))
}

func validateImage(raw []byte) (string, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 24000000 {
		return "", ErrInvalid
	}
	mime := map[string]string{"png": "image/png", "jpeg": "image/jpeg", "gif": "image/gif"}[format]
	if mime == "" {
		return "", ErrInvalid
	}
	if _, _, err = image.Decode(bytes.NewReader(raw)); err != nil {
		return "", ErrInvalid
	}
	return mime, nil
}
func (h *Handler) upload(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxAssetBytes+(1<<20))
	file, _, err := c.Request.FormFile("file")
	if c.Request.MultipartForm != nil {
		defer func() { _ = c.Request.MultipartForm.RemoveAll() }()
	}
	if err != nil {
		response.BadRequest(c, "Choose a PNG, JPEG or GIF image up to 5 MB")
		return
	}
	defer func() { _ = file.Close() }()
	raw, err := io.ReadAll(io.LimitReader(file, maxAssetBytes+1))
	if err != nil || len(raw) > maxAssetBytes {
		response.BadRequest(c, "Image exceeds 5 MB")
		return
	}
	mime, err := validateImage(raw)
	if err != nil {
		response.BadRequest(c, "Invalid image; use PNG, JPEG or GIF (up to 24 megapixels)")
		return
	}
	tx, err := h.store.db.BeginTx(c.Request.Context(), nil)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	defer func() { _ = tx.Rollback() }()
	var article int64
	err = tx.QueryRowContext(c.Request.Context(), `SELECT id FROM tutorials WHERE id=$1 FOR UPDATE`, id).Scan(&article)
	if err == sql.ErrNoRows {
		response.ErrorFrom(c, ErrNotFound)
		return
	}
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	var count, size int64
	err = tx.QueryRowContext(c.Request.Context(), `SELECT count(*),COALESCE(sum(size),0) FROM tutorial_assets WHERE tutorial_id=$1`, id).Scan(&count, &size)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if count >= maxAssetCount || size+int64(len(raw)) > maxAssetTotal {
		response.BadRequest(c, "Tutorial image quota exceeded (200 images / 50 MB)")
		return
	}
	assetID, err := newAssetID()
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	filename, err := h.writeAsset(assetID, raw)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(filename)
		}
	}()
	_, err = tx.ExecContext(c.Request.Context(), `INSERT INTO tutorial_assets(id,tutorial_id,mime,size) VALUES($1,$2,$3,$4)`, assetID, id, mime, len(raw))
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	keep = true
	response.Success(c, gin.H{"id": assetID, "reference": assetScheme + assetID})
}

func newAssetID() (string, error) {
	var key [16]byte
	if _, err := rand.Read(key[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(key[:]), nil
}

func (h *Handler) writeAsset(id string, raw []byte) (string, error) {
	if err := os.MkdirAll(h.assetDir, 0700); err != nil {
		return "", err
	}
	filename := filepath.Join(h.assetDir, id)
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	_, writeErr := f.Write(raw)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(filename)
		if writeErr != nil {
			return "", writeErr
		}
		return "", closeErr
	}
	return filename, nil
}
func referencesAsset(content, key string) bool {
	for _, m := range assetReferencePattern.FindAllStringSubmatch(content, -1) {
		if m[1] == key {
			return true
		}
	}
	return false
}
func (h *Handler) asset(access Access) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := idParam(c, "id")
		if !ok {
			return
		}
		key := c.Param("asset")
		if !assetIDPattern.MatchString(key) {
			response.ErrorFrom(c, ErrNotFound)
			return
		}
		a, err := h.store.Get(c.Request.Context(), id, access)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		if access != AdminAccess && !referencesAsset(a.Content.Content, key) {
			response.ErrorFrom(c, ErrNotFound)
			return
		}
		var mime string
		err = h.store.db.QueryRowContext(c.Request.Context(), `SELECT mime FROM tutorial_assets WHERE id=$1 AND tutorial_id=$2`, key, id).Scan(&mime)
		if err == sql.ErrNoRows {
			response.ErrorFrom(c, ErrNotFound)
			return
		}
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		raw, err := os.ReadFile(filepath.Join(h.assetDir, key))
		if err != nil {
			response.ErrorFrom(c, ErrNotFound)
			return
		}
		c.Header("Content-Disposition", "inline")
		c.Data(http.StatusOK, mime, raw)
	}
}
