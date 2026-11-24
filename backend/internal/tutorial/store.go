// Package tutorial implements the tutorial center and its admin editor.
package tutorial

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	apperrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrNotFound = apperrors.NotFound("TUTORIAL_NOT_FOUND", "tutorial not found")
	ErrConflict = apperrors.Conflict("TUTORIAL_CONFLICT", "This tutorial has changed. Reload before saving.")
	ErrInvalid  = apperrors.BadRequest("TUTORIAL_INVALID", "Invalid tutorial content or category")
	ErrSlug     = apperrors.BadRequest("TUTORIAL_SLUG_INVALID", "Use up to 80 lowercase letters, digits and hyphens, including at least one letter")
	ErrSlugUsed = apperrors.BadRequest("TUTORIAL_SLUG_USED", "This tutorial address is already in use")
	ErrSlugLock = apperrors.BadRequest("TUTORIAL_SLUG_LOCKED", "The published tutorial address is fixed")
)

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

type Content struct {
	Slug       string `json:"slug,omitempty"`
	Visibility string `json:"visibility"`
	Title      string `json:"title"`
	Summary    string `json:"summary"`
	Content    string `json:"content"`
	CategoryID *int64 `json:"category_id"`
	Pinned     bool   `json:"pinned"`
	SortOrder  int    `json:"sort_order"`
}
type Article struct {
	ID           int64   `json:"id"`
	RouteSlug    string  `json:"route_slug,omitempty"`
	PreviousID   *int64  `json:"previous_id,omitempty"`
	PreviousSlug *string `json:"previous_slug,omitempty"`
	NextID       *int64  `json:"next_id,omitempty"`
	NextSlug     *string `json:"next_slug,omitempty"`
	Content
	Version          int64      `json:"version"`
	PublishedVersion *int64     `json:"published_version,omitempty"`
	Published        bool       `json:"published"`
	HasChanges       bool       `json:"has_changes"`
	UpdatedAt        time.Time  `json:"updated_at"`
	PublishedAt      *time.Time `json:"published_at"`
}
type Category struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	SortOrder int    `json:"sort_order"`
}
type Store struct{ db *sql.DB }

type Access uint8

const (
	MemberAccess Access = iota
	AdminAccess
	PublicAccess
)

const publicCondition = `COALESCE(published->>'visibility','login')='public'`

const articleColumns = `id, draft, published, version, published_version, updated_at, published_at, route_slug`

func scanArticle(row interface{ Scan(...any) error }, access Access) (*Article, error) {
	var a Article
	var draft, published []byte
	if err := row.Scan(&a.ID, &draft, &published, &a.Version, &a.PublishedVersion, &a.UpdatedAt, &a.PublishedAt, &a.RouteSlug); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	a.Published = len(published) > 0
	a.HasChanges = a.Published && (a.PublishedVersion == nil || *a.PublishedVersion != a.Version)
	raw := draft
	if access != AdminAccess {
		if !a.Published {
			return nil, ErrNotFound
		}
		raw = published
		a.Version = 0
		a.PublishedVersion = nil
		a.HasChanges = false
		if a.PublishedAt != nil {
			a.UpdatedAt = *a.PublishedAt
		}
	}
	if err := json.Unmarshal(raw, &a.Content); err != nil {
		return nil, err
	}
	if a.Visibility == "" {
		a.Visibility = "login"
	}
	if a.RouteSlug != "" {
		a.Slug = a.RouteSlug
	}
	return &a, nil
}
func (s *Store) Get(ctx context.Context, id int64, access Access) (*Article, error) {
	return s.getBy(ctx, "id", id, access)
}

func (s *Store) GetBySlug(ctx context.Context, slug string, access Access) (*Article, error) {
	if !validSlug(slug) {
		return nil, ErrNotFound
	}
	return s.getBy(ctx, "route_slug", slug, access)
}

func (s *Store) getBy(ctx context.Context, column string, value any, access Access) (*Article, error) {
	query := `SELECT ` + articleColumns + ` FROM tutorials WHERE ` + column + `=$1`
	if access != AdminAccess {
		query += ` AND published IS NOT NULL`
	}
	if access == PublicAccess {
		query += ` AND ` + publicCondition
	}
	return scanArticle(s.db.QueryRowContext(ctx, query, value), access)
}

func (s *Store) Neighbors(ctx context.Context, a *Article, access Access) error {
	where := `published IS NOT NULL`
	if access == PublicAccess {
		where += ` AND ` + publicCondition
	}
	return s.db.QueryRowContext(ctx, `SELECT previous_id,previous_slug,next_id,next_slug FROM (
 SELECT id,lag(id) OVER w AS previous_id,lag(NULLIF(route_slug,'')) OVER w AS previous_slug,
 lead(id) OVER w AS next_id,lead(NULLIF(route_slug,'')) OVER w AS next_slug
 FROM tutorials WHERE `+where+`
 WINDOW w AS (ORDER BY (published->>'pinned')::boolean DESC,(published->>'sort_order')::int,published_at DESC,id DESC)
 ) ordered WHERE id=$1`, a.ID).Scan(&a.PreviousID, &a.PreviousSlug, &a.NextID, &a.NextSlug)
}
func (s *Store) List(ctx context.Context, access Access, search, status string, category int64, page, size int) ([]Article, int64, error) {
	// Reader searches are scoped to the published snapshot.
	source := "published"
	where := "published IS NOT NULL"
	if access == AdminAccess {
		source = "draft"
		where = "TRUE"
	}
	if access == PublicAccess {
		where += ` AND ` + publicCondition
	}
	args := []any{strings.ToLower(search), category}
	where += ` AND ($1=''
		OR strpos(lower(` + source + `->>'title'),$1)>0
		OR strpos(lower(` + source + `->>'summary'),$1)>0
		OR strpos(lower(` + source + `->>'content'),$1)>0)`
	where += ` AND ($2=0 OR (` + source + `->>'category_id')::bigint=$2)`
	if access == AdminAccess && status == "draft" {
		where += ` AND published IS NULL`
	}
	if access == AdminAccess && status == "published" {
		where += ` AND published IS NOT NULL`
	}
	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM tutorials WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, size, (page-1)*size)
	rows, err := s.db.QueryContext(ctx, `SELECT `+articleColumns+` FROM tutorials WHERE `+where+` ORDER BY (`+source+`->>'pinned')::boolean DESC, (`+source+`->>'sort_order')::int, published_at DESC NULLS LAST, id DESC LIMIT $3 OFFSET $4`, args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	items := []Article{}
	for rows.Next() {
		a, err := scanArticle(rows, access)
		if err != nil {
			return nil, 0, err
		}
		a.Content.Content = ""
		items = append(items, *a)
	}
	return items, total, rows.Err()
}
func validateContent(in *Content, publish bool) error {
	in.Slug = strings.TrimSpace(in.Slug)
	if in.Slug != "" && !validSlug(in.Slug) {
		return ErrSlug
	}
	if in.Visibility == "" {
		in.Visibility = "login"
	}
	if in.Visibility != "login" && in.Visibility != "public" {
		return ErrInvalid
	}
	in.Title = strings.TrimSpace(in.Title)
	in.Summary = strings.TrimSpace(in.Summary)
	if utf8.RuneCountInString(in.Title) > 160 || utf8.RuneCountInString(in.Summary) > 500 || len(in.Content) > 1<<20 || in.SortOrder < -100000 || in.SortOrder > 100000 {
		return ErrInvalid
	}
	if publish && (in.Title == "" || strings.TrimSpace(in.Content) == "") {
		return ErrInvalid
	}
	if in.CategoryID != nil && *in.CategoryID <= 0 {
		return ErrInvalid
	}
	return nil
}

func validSlug(slug string) bool {
	return len(slug) <= 80 && slug != "categories" && slugPattern.MatchString(slug) &&
		strings.ContainsAny(slug, "abcdefghijklmnopqrstuvwxyz")
}

func checkSlug(ctx context.Context, tx *sql.Tx, id int64, slug string) error {
	if slug == "" {
		return nil
	}
	var exists bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM tutorials
 WHERE id<>$1 AND (route_slug=$2 OR draft->>'slug'=$2))`, id, slug).Scan(&exists)
	if err == nil && exists {
		return ErrSlugUsed
	}
	return err
}

func (s *Store) Create(ctx context.Context, author int64, in Content) (*Article, error) {
	if err := validateContent(&in, false); err != nil {
		return nil, err
	}
	// All content writes serialize against category removal, including new drafts.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(8348201)`); err != nil {
		return nil, err
	}
	if err = validCategory(ctx, tx, in.CategoryID); err != nil {
		return nil, err
	}
	if err = checkSlug(ctx, tx, 0, in.Slug); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	a, err := scanArticle(tx.QueryRowContext(ctx, `INSERT INTO tutorials(draft,author_id) VALUES($1,$2) RETURNING `+articleColumns, string(raw), author), AdminAccess)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return a, nil
}
func validCategory(ctx context.Context, tx *sql.Tx, id *int64) error {
	if id == nil {
		return nil
	}
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tutorial_categories WHERE id=$1)`, *id).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrInvalid
	}
	return nil
}
func (s *Store) Change(ctx context.Context, id, version int64, action string, in Content) (*Article, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(8348201)`); err != nil {
		return nil, err
	}
	a, err := scanArticle(tx.QueryRowContext(ctx, `SELECT `+articleColumns+` FROM tutorials WHERE id=$1 FOR UPDATE`, id), AdminAccess)
	if err != nil {
		return nil, err
	}
	if a.Version != version {
		return nil, ErrConflict
	}
	switch action {
	case "save":
		if a.RouteSlug != "" {
			if in.Slug != "" && strings.TrimSpace(in.Slug) != a.RouteSlug {
				return nil, ErrSlugLock
			}
			in.Slug = a.RouteSlug
		}
		if err = validateContent(&in, false); err != nil {
			return nil, err
		}
		if err = checkSlug(ctx, tx, id, in.Slug); err != nil {
			return nil, err
		}
		if err = validCategory(ctx, tx, in.CategoryID); err != nil {
			return nil, err
		}
		raw, e := json.Marshal(in)
		if e != nil {
			return nil, e
		}
		_, err = tx.ExecContext(ctx, `UPDATE tutorials SET draft=$2,version=version+1,updated_at=NOW() WHERE id=$1`, id, string(raw))
	case "publish":
		if err = validateContent(&a.Content, true); err != nil {
			return nil, err
		}
		refs := imageReferences(a.Content.Content)
		for key := range refs {
			var exists bool
			if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tutorial_assets WHERE id=$1 AND tutorial_id=$2)`, key, id).Scan(&exists); err != nil {
				return nil, err
			}
			if !exists {
				return nil, ErrInvalid
			}
		}
		if err = checkSlug(ctx, tx, id, a.Slug); err != nil {
			return nil, err
		}
		_, err = tx.ExecContext(ctx, `UPDATE tutorials SET published=draft,route_slug=$2,published_version=version+1,version=version+1,published_at=NOW(),updated_at=NOW() WHERE id=$1`, id, a.Slug)
		if err == nil {
			err = queueUnusedAssets(ctx, tx, id, refs)
		}
	case "unpublish":
		_, err = tx.ExecContext(ctx, `UPDATE tutorials SET published=NULL,published_version=NULL,version=version+1,published_at=NULL,updated_at=NOW() WHERE id=$1`, id)
		if err == nil {
			err = queueUnusedAssets(ctx, tx, id, imageReferences(a.Content.Content))
		}
	case "delete":
		if err = queueUnusedAssets(ctx, tx, id, nil); err != nil {
			return nil, err
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM tutorials WHERE id=$1`, id)
	default:
		return nil, ErrInvalid
	}
	if err != nil {
		return nil, err
	}
	if action != "delete" {
		a, err = scanArticle(tx.QueryRowContext(ctx, `SELECT `+articleColumns+` FROM tutorials WHERE id=$1`, id), AdminAccess)
		if err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return a, nil
}
func (s *Store) Categories(ctx context.Context, access Access) ([]Category, error) {
	query := `SELECT id,name,sort_order FROM tutorial_categories`
	if access != AdminAccess {
		query += ` WHERE EXISTS(SELECT 1 FROM tutorials t WHERE t.published IS NOT NULL AND (t.published->>'category_id')::bigint=tutorial_categories.id`
		if access == PublicAccess {
			query += ` AND ` + publicCondition
		}
		query += `)`
	}
	rows, err := s.db.QueryContext(ctx, query+` ORDER BY sort_order,id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := []Category{}
	for rows.Next() {
		var c Category
		if err = rows.Scan(&c.ID, &c.Name, &c.SortOrder); err != nil {
			return nil, err
		}
		items = append(items, c)
	}
	return items, rows.Err()
}
func (s *Store) SaveCategory(ctx context.Context, id int64, in Category) (*Category, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || utf8.RuneCountInString(in.Name) > 80 || in.SortOrder < -100000 || in.SortOrder > 100000 {
		return nil, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(8348201)`); err != nil {
		return nil, err
	}
	var exists bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tutorial_categories WHERE name=$1 AND id<>$2)`, in.Name, id).Scan(&exists); err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrInvalid
	}
	if id == 0 {
		err = tx.QueryRowContext(ctx, `INSERT INTO tutorial_categories(name,sort_order) VALUES($1,$2) RETURNING id`, in.Name, in.SortOrder).Scan(&in.ID)
	} else {
		in.ID = id
		var result sql.Result
		result, err = tx.ExecContext(ctx, `UPDATE tutorial_categories SET name=$2,sort_order=$3 WHERE id=$1`, id, in.Name, in.SortOrder)
		if err == nil {
			n, _ := result.RowsAffected()
			if n == 0 {
				return nil, ErrNotFound
			}
		}
	}
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &in, nil
}
func (s *Store) DeleteCategory(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(8348201)`); err != nil {
		return err
	}
	// Snapshot category removal is metadata-only and must invalidate concurrent editors.
	_, err = tx.ExecContext(ctx, `UPDATE tutorials SET draft=CASE WHEN (draft->>'category_id')::bigint=$1 THEN jsonb_set(draft,'{category_id}','null') ELSE draft END,
 published=CASE WHEN (published->>'category_id')::bigint=$1 THEN jsonb_set(published,'{category_id}','null') ELSE published END,
 published_version=CASE WHEN published_version=version THEN version+1 ELSE published_version END,
 version=version+1,updated_at=NOW() WHERE (draft->>'category_id')::bigint=$1 OR (published->>'category_id')::bigint=$1`, id)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM tutorial_categories WHERE id=$1`, id); err != nil {
		return err
	}
	return tx.Commit()
}
