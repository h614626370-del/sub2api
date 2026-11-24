package tutorial

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"path"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	apperrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/extension"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/text"
	"golang.org/x/net/html"
	"gopkg.in/yaml.v3"
)

const (
	maxAssetCount   = 200
	maxAssetTotal   = 50 << 20
	maxPackageBytes = 52 << 20
	maxFrontMatter  = 16 << 10
	maxMarkdownFile = 1<<20 + maxFrontMatter
	assetScheme     = "tutorial-asset:"
)

type packageMetadata struct {
	Title             string `yaml:"title"`
	Summary           string `yaml:"summary"`
	Category          string `yaml:"category,omitempty"`
	CategorySortOrder int    `yaml:"category_sort_order,omitempty"`
	Visibility        string `yaml:"visibility"`
	Pinned            bool   `yaml:"pinned"`
	SortOrder         int    `yaml:"sort_order"`
}

type packageImage struct {
	id   string
	mime string
	data []byte
}

type tutorialPackage struct {
	content  Content
	metadata packageMetadata
	images   map[string]packageImage
}

var tutorialPackageMarkdown = parser.New(parser.WithExtensions(extension.NewGFMParser()))

func packageError(code, message, file string) error {
	e := apperrors.BadRequest("TUTORIAL_PACKAGE_"+code, message)
	if file != "" {
		return e.WithMetadata(map[string]string{"file": file})
	}
	return e
}

func readTutorialPackage(src io.ReaderAt, size int64) (*tutorialPackage, error) {
	if size > maxPackageBytes {
		return nil, packageError("ZIP_SIZE", "Tutorial ZIP exceeds 52 MiB", "")
	}
	archive, err := zip.NewReader(src, size)
	if err != nil || len(archive.File) > 512 {
		return nil, packageError("ZIP_INVALID", "Invalid tutorial ZIP or too many entries", "")
	}
	files := make(map[string]*zip.File, len(archive.File))
	seen := make(map[string]bool, len(archive.File))
	markdown := ""
	for _, entry := range archive.File {
		name := strings.TrimSuffix(entry.Name, "/")
		key := strings.ToLower(name)
		if !packagePathValid(name) || seen[key] {
			return nil, packageError("PATH_INVALID", "Unsafe or duplicate ZIP entry", name)
		}
		seen[key] = true
		if entry.FileInfo().IsDir() {
			if name != "images" && !strings.HasPrefix(name, "images/") {
				return nil, packageError("LAYOUT", "Expected one Markdown file and an images directory at the ZIP root", name)
			}
			continue
		}
		if !entry.Mode().IsRegular() {
			return nil, packageError("PATH_INVALID", "ZIP entries must be regular files", name)
		}
		switch {
		case strings.HasPrefix(name, "images/"):
			files[name] = entry
		case !strings.Contains(name, "/") && isMarkdownFile(name) && markdown == "":
			markdown = name
			files[name] = entry
		default:
			return nil, packageError("LAYOUT", "Expected one Markdown file and an images directory at the ZIP root", name)
		}
	}
	if markdown == "" {
		return nil, packageError("LAYOUT", "The ZIP does not contain a Markdown file at the root", "")
	}
	if len(files)-1 > maxAssetCount {
		return nil, packageError("IMAGE_QUOTA", "Tutorial image quota exceeded (200 images / 50 MiB)", "")
	}
	markdownRaw, err := readPackageEntry(files[markdown], maxMarkdownFile, "MARKDOWN_SIZE", "Markdown file is too large")
	if err != nil {
		return nil, err
	}
	delete(files, markdown)
	meta, body, err := splitPackageMarkdown(markdownRaw)
	if err != nil {
		return nil, err
	}
	doc := tutorialPackageMarkdown.Parse(body)
	if meta.Title == "" {
		for node := doc.FirstChild(); node != nil; node = node.NextSibling() {
			if heading, ok := node.(*ast.Heading); ok && heading.Level == 1 {
				var title strings.Builder
				_ = ast.Walk(heading, func(child ast.Node, entering bool) (ast.WalkStatus, error) {
					if part, ok := child.(*ast.Text); entering && ok {
						title.WriteString(part.Value.Value(body))
					}
					return ast.WalkContinue, nil
				})
				meta.Title = strings.TrimSpace(title.String())
				break
			}
		}
		if meta.Title == "" {
			meta.Title = strings.TrimSuffix(markdown, path.Ext(markdown))
		}
	}
	pkg := &tutorialPackage{
		content:  Content{Title: meta.Title, Summary: meta.Summary, Content: string(body), Visibility: meta.Visibility, Pinned: meta.Pinned, SortOrder: meta.SortOrder},
		metadata: meta,
		images:   make(map[string]packageImage, len(files)),
	}
	if err = validateContent(&pkg.content, true); err != nil {
		return nil, packageError("CONTENT_INVALID", "Invalid tutorial title, summary, body, visibility or sort order", "")
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	total := 0
	for _, name := range names {
		ext := strings.ToLower(path.Ext(name))
		expectedMIME := packageImageMIME(ext)
		if expectedMIME == "" {
			return nil, packageError("IMAGE_FORMAT", "Image must be a PNG, JPEG or GIF", name)
		}
		raw, err := readPackageEntry(files[name], maxAssetBytes, "IMAGE_SIZE", "Image exceeds 5 MiB")
		if err != nil {
			return nil, err
		}
		total += len(raw)
		if total > maxAssetTotal {
			return nil, packageError("IMAGE_QUOTA", "Tutorial image quota exceeded (200 images / 50 MiB)", name)
		}
		mime, err := validateImage(raw)
		if err != nil || mime != expectedMIME {
			return nil, packageError("IMAGE_FORMAT", "Invalid image format, extension or dimensions (maximum 24 megapixels)", name)
		}
		id, err := newAssetID()
		if err != nil {
			return nil, err
		}
		pkg.images[name] = packageImage{id: id, mime: mime, data: raw}
	}
	pkg.content.Content, err = rewritePackageImages(doc, body, func(destination string) (string, error) {
		name, err := localPackageImage(destination)
		if err != nil || name == "" {
			return "", err
		}
		image, ok := pkg.images[name]
		if !ok {
			return "", packageError("IMAGE_MISSING", "Referenced image is missing from the ZIP", name)
		}
		return assetScheme + image.id, nil
	})
	if err != nil {
		return nil, err
	}
	if len(pkg.content.Content) > 1<<20 {
		return nil, packageError("MARKDOWN_SIZE", "Markdown body exceeds 1 MiB after importing image references", "")
	}
	return pkg, nil
}

func isMarkdownFile(name string) bool {
	ext := strings.ToLower(path.Ext(name))
	return ext == ".md" || ext == ".markdown"
}

func packagePathValid(name string) bool {
	return name != "." && len(name) <= 512 && fs.ValidPath(name) &&
		!strings.ContainsAny(name, `:\`) && !strings.ContainsFunc(name, unicode.IsControl)
}

func packageImageMIME(ext string) string {
	switch ext {
	case ".png":
		return "image/png"
	case ".jpeg", ".jpg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	}
	return ""
}

func readPackageEntry(entry *zip.File, limit int, code, message string) ([]byte, error) {
	if entry.UncompressedSize64 > uint64(limit) {
		return nil, packageError(code, message, entry.Name)
	}
	reader, err := entry.Open()
	if err != nil {
		return nil, packageError("ZIP_INVALID", "Could not open ZIP entry", entry.Name)
	}
	defer func() { _ = reader.Close() }()
	raw, err := io.ReadAll(io.LimitReader(reader, int64(limit)+1))
	if len(raw) > limit {
		return nil, packageError(code, message, entry.Name)
	}
	if err != nil {
		return nil, packageError("ZIP_INVALID", "Could not read ZIP entry", entry.Name)
	}
	return raw, nil
}

func splitPackageMarkdown(raw []byte) (packageMetadata, []byte, error) {
	var meta packageMetadata
	if !utf8.Valid(raw) || bytes.IndexByte(raw, 0) >= 0 {
		return meta, nil, packageError("CONTENT_INVALID", "Markdown must be valid UTF-8 text", "")
	}
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	first, rest, found := bytes.Cut(raw, []byte("\n"))
	if !found || string(bytes.TrimSuffix(first, []byte("\r"))) != "---" {
		return meta, raw, nil
	}
	header := rest
	for offset := 0; offset <= maxFrontMatter && len(rest) > 0; {
		line, next, newline := bytes.Cut(rest, []byte("\n"))
		if string(bytes.TrimSuffix(line, []byte("\r"))) == "---" {
			decoder := yaml.NewDecoder(bytes.NewReader(header[:offset]))
			decoder.KnownFields(true)
			if err := decoder.Decode(&meta); err != nil && !errors.Is(err, io.EOF) {
				return meta, nil, packageError("METADATA", "Invalid tutorial YAML metadata", "")
			}
			if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
				return meta, nil, packageError("METADATA", "Expected a single YAML metadata document", "")
			}
			meta.Category = strings.TrimSpace(meta.Category)
			if utf8.RuneCountInString(meta.Category) > 80 || meta.CategorySortOrder < -100000 || meta.CategorySortOrder > 100000 {
				return meta, nil, packageError("METADATA", "Invalid tutorial category name or category sort order", "")
			}
			return meta, next, nil
		}
		if !newline {
			break
		}
		offset += len(line) + 1
		rest = next
	}
	return meta, nil, packageError("METADATA", "Tutorial YAML metadata needs a closing --- line within 16 KiB", "")
}

func localPackageImage(destination string) (string, error) {
	parsed, err := url.Parse(destination)
	if err == nil && (parsed.Scheme == "https" || parsed.Scheme == "http") && parsed.Host != "" {
		return "", nil
	}
	if err == nil && parsed.Scheme == "" && parsed.Host == "" && !parsed.ForceQuery && parsed.RawQuery == "" && !strings.Contains(destination, "#") {
		name := strings.TrimPrefix(parsed.Path, "./")
		if strings.HasPrefix(name, "images/") && packagePathValid(name) {
			return name, nil
		}
	}
	return "", packageError("IMAGE_PATH", "Use an image path under images/ inside the ZIP", truncatePath(destination))
}

func truncatePath(s string) string {
	runes := []rune(s)
	if len(runes) > 200 {
		return string(runes[:200]) + "..."
	}
	return s
}

type markdownEdit struct {
	text.Index
	replacement string
}

// Edit only source spans resolved by the parser, preserving surrounding Markdown.
func rewritePackageImages(doc ast.Node, source []byte, replace func(string) (string, error)) (string, error) {
	edits := make(map[text.Index]string)
	err := ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		var img *ast.Image
		switch node := node.(type) {
		case *ast.Image:
			img = node
		case *ast.RawHTML:
			return ast.WalkContinue, rewritePackageHTML(node.Value.Indices(), source, edits, replace)
		case *ast.HTMLBlock:
			segments := node.Value.Segments()
			indices := make([]text.Index, 0, len(segments))
			for _, segment := range segments {
				indices = append(indices, text.NewIndexFromSegment(segment))
			}
			return ast.WalkContinue, rewritePackageHTML(indices, source, edits, replace)
		default:
			return ast.WalkContinue, nil
		}
		index := img.Destination.Index()
		if _, ok := edits[index]; ok {
			return ast.WalkContinue, nil
		}
		value, err := replace(img.Destination.Value(source))
		if err != nil {
			return ast.WalkStop, err
		}
		if value != "" {
			if img.Destination.IsOwned() || index.Start == index.Stop {
				return ast.WalkStop, packageError("IMAGE_PATH", "Cannot resolve the Markdown image destination", "")
			}
			edits[index] = value
		}
		return ast.WalkContinue, nil
	})
	if err != nil {
		return "", err
	}
	ordered := make([]markdownEdit, 0, len(edits))
	for index, replacement := range edits {
		ordered = append(ordered, markdownEdit{Index: index, replacement: replacement})
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Start < ordered[j].Start })
	var result strings.Builder
	offset := 0
	for _, edit := range ordered {
		if edit.Start < offset || edit.Stop > len(source) {
			return "", fmt.Errorf("overlapping tutorial image destinations")
		}
		result.Write(source[offset:edit.Start])
		result.WriteString(edit.replacement)
		offset = edit.Stop
	}
	result.Write(source[offset:])
	return result.String(), nil
}

func rewritePackageHTML(indices []text.Index, source []byte, edits map[text.Index]string, replace func(string) (string, error)) error {
	if len(indices) == 0 {
		return nil
	}
	var raw bytes.Buffer
	for _, index := range indices {
		raw.Write(source[index.Start:index.Stop])
	}
	tokenizer := html.NewTokenizer(bytes.NewReader(raw.Bytes()))
	segment, segmentOffset, offset := 0, 0, 0
	for {
		kind := tokenizer.Next()
		if kind == html.ErrorToken {
			return nil
		}
		start := offset
		offset += len(tokenizer.Raw())
		if kind != html.StartTagToken && kind != html.SelfClosingTagToken {
			continue
		}
		token := tokenizer.Token()
		if token.Data != "img" {
			continue
		}
		for i, attribute := range token.Attr {
			if attribute.Key != "src" {
				continue
			}
			destination, err := replace(attribute.Val)
			if err != nil {
				return err
			}
			if destination == "" {
				break
			}
			for segment < len(indices)-1 && start >= segmentOffset+indices[segment].Stop-indices[segment].Start {
				segmentOffset += indices[segment].Stop - indices[segment].Start
				segment++
			}
			sourceStart := indices[segment].Start + start - segmentOffset
			tokenLength := offset - start
			last, end := segment, indices[segment].Stop
			for sourceStart+tokenLength > end && last+1 < len(indices) && indices[last+1].Start == end {
				last++
				end = indices[last].Stop
			}
			if sourceStart+tokenLength > end {
				return packageError("IMAGE_PATH", "Use Markdown syntax for images that span multiple quoted HTML lines", truncatePath(attribute.Val))
			}
			token.Attr[i].Val = destination
			edits[text.NewIndex(sourceStart, sourceStart+tokenLength)] = token.String()
			break
		}
	}
}
