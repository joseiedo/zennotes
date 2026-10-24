package httpserver

import (
	"bytes"
	"html"
	"io/fs"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

// builtinPublicShell is used when no public bundle is available (dev, or a
// server built without dist-public). It carries the same mount points the
// bundled public.html does.
const builtinPublicShell = `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"></head><body><div id="zn-root"></div><div id="zn-seo"></div></body></html>`

const publicDescriptionLimit = 160

// PublicPage is what the page shell knows about the note being rendered. Every
// field must already be safe to publish: Body is the sanitized body.
type PublicPage struct {
	Title       string
	Description string
	Body        string
	// URLPath is the page's absolute path on the public site ("/" for home).
	URLPath string
}

var (
	titleTagRe   = regexp.MustCompile(`(?is)<title\b[^>]*>.*?</title>`)
	blankLineRe  = regexp.MustCompile(`\n[ \t\r]*\n`)
	seoMountOpen = []byte(`<div id="zn-seo">`)
)

// publicShellTemplate returns public.html from the bundle, or the built-in
// shell when there is no bundle.
func (s *Server) publicShellTemplate() []byte {
	s.mu.RLock()
	fsys := s.publicStatic
	s.mu.RUnlock()
	if fsys != nil {
		if data, err := fs.ReadFile(fsys, "public.html"); err == nil {
			return data
		}
	}
	return []byte(builtinPublicShell)
}

// publicPageURL encodes a vault note path as its public URL path: each
// segment escaped, `.md` dropped.
func publicPageURL(notePath string) string {
	segs := strings.Split(strings.TrimSuffix(notePath, ".md"), "/")
	for i, seg := range segs {
		segs[i] = url.PathEscape(seg)
	}
	return "/" + strings.Join(segs, "/")
}

// publicDescription prefers the frontmatter description, then the first
// characters of the sanitized body with whitespace collapsed.
func publicDescription(frontmatter, sanitized string) string {
	if d := strings.Join(strings.Fields(frontmatter), " "); d != "" {
		return d
	}
	d := strings.Join(strings.Fields(sanitized), " ")
	if utf8.RuneCountInString(d) > publicDescriptionLimit {
		d = strings.TrimSpace(string([]rune(d)[:publicDescriptionLimit]))
	}
	return d
}

// seoParagraphs renders the sanitized body as escaped <p> paragraphs, split
// on blank lines, for crawlers that do not run the bundle.
func seoParagraphs(sanitized string) string {
	var b strings.Builder
	for _, para := range blankLineRe.Split(strings.ReplaceAll(sanitized, "\r\n", "\n"), -1) {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		b.WriteString("<p>")
		b.WriteString(html.EscapeString(para))
		b.WriteString("</p>")
	}
	return b.String()
}

// injectHead replaces the template's <title> (if any) and inserts tags right
// before </head>.
func injectHead(doc []byte, title string, tags string) []byte {
	titleTag := "<title>" + html.EscapeString(title) + "</title>"
	if loc := titleTagRe.FindIndex(doc); loc != nil {
		out := make([]byte, 0, len(doc)+len(tags))
		out = append(out, doc[:loc[0]]...)
		out = append(out, doc[loc[1]:]...)
		doc = out
	}
	snippet := titleTag + tags
	if idx := indexOfFold(doc, []byte("</head>")); idx >= 0 {
		out := make([]byte, 0, len(doc)+len(snippet))
		out = append(out, doc[:idx]...)
		out = append(out, snippet...)
		out = append(out, doc[idx:]...)
		return out
	}
	return append([]byte(snippet), doc...)
}

func injectSEO(doc []byte, content string) []byte {
	idx := bytes.Index(doc, seoMountOpen)
	if idx < 0 || content == "" {
		return doc
	}
	at := idx + len(seoMountOpen)
	out := make([]byte, 0, len(doc)+len(content))
	out = append(out, doc[:at]...)
	out = append(out, content...)
	out = append(out, doc[at:]...)
	return out
}

// renderShell renders the page shell for a public note. A nil page renders
// the bare shell (home without a public home note). All values are escaped.
func (s *Server) renderShell(page *PublicPage) []byte {
	doc := s.publicShellTemplate()
	if page == nil {
		return doc
	}
	esc := html.EscapeString
	var tags strings.Builder
	tags.WriteString(`<meta name="description" content="` + esc(page.Description) + `">`)
	tags.WriteString(`<meta property="og:title" content="` + esc(page.Title) + `">`)
	tags.WriteString(`<meta property="og:description" content="` + esc(page.Description) + `">`)
	if base := strings.TrimRight(s.currentConfig().PublicURL, "/"); base != "" {
		abs := base + page.URLPath
		tags.WriteString(`<meta property="og:url" content="` + esc(abs) + `">`)
		tags.WriteString(`<link rel="canonical" href="` + esc(abs) + `">`)
	}
	tags.WriteString(`<meta property="og:type" content="article">`)
	tags.WriteString(`<meta name="twitter:card" content="summary">`)
	doc = injectHead(doc, page.Title, tags.String())
	return injectSEO(doc, seoParagraphs(page.Body))
}

// renderNotFoundShell is the one page-404 body: title "Not found", no meta.
func (s *Server) renderNotFoundShell() []byte {
	return injectHead(s.publicShellTemplate(), "Not found", "")
}
