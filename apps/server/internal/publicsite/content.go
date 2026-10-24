package publicsite

import (
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
)

// This file holds the pure text helpers behind the public site: what part of
// a note body may leave the server, and which vault files a public note may
// reference. Everything here is leak-averse: when unsure, strip or deny.

type span struct{ start, end int }

// splitFrontmatter separates a leading YAML frontmatter block from the body.
// It reports whether the body starts a frontmatter block at all. When the
// block is never closed, the whole input is treated as frontmatter (fail
// closed) and rest is empty.
func splitFrontmatter(body string) (fm, rest string, found bool) {
	s := strings.TrimPrefix(body, "\ufeff")
	s = strings.TrimLeft(s, " \t\r\n")
	nl := strings.IndexByte(s, '\n')
	if nl < 0 || strings.TrimRight(s[:nl], " \t\r") != "---" {
		return "", body, false
	}
	pos := nl + 1
	for pos <= len(s) {
		end := strings.IndexByte(s[pos:], '\n')
		var line string
		next := len(s)
		if end < 0 {
			line = s[pos:]
		} else {
			line = s[pos : pos+end]
			next = pos + end + 1
		}
		t := strings.TrimRight(line, " \t\r")
		if t == "---" {
			return s[nl+1 : pos], s[next:], true
		}
		if end < 0 {
			break
		}
		pos = next
	}
	return s[nl+1:], "", true
}

// codeSpans returns the byte ranges of fenced code blocks (``` / ~~~, length
// >= 3, closed by a fence of the same character and at least the same
// length) and single-line inline code spans, sorted by start.
func codeSpans(s string) []span {
	var out []span
	pos := 0
	var fenceChar byte
	fenceLen := 0
	fenceStart := 0
	for pos < len(s) {
		lineEnd := strings.IndexByte(s[pos:], '\n')
		next := len(s)
		line := s[pos:]
		if lineEnd >= 0 {
			line = s[pos : pos+lineEnd]
			next = pos + lineEnd + 1
		}
		trimmed := strings.TrimLeft(line, " \t")
		run := 0
		// A fence line is indented at most 3 spaces; a tab counts as 4.
		indent := 0
		for _, c := range line[:len(line)-len(trimmed)] {
			if c == '\t' {
				indent += 4
			} else {
				indent++
			}
		}
		if indent <= 3 && len(trimmed) > 0 && (trimmed[0] == '`' || trimmed[0] == '~') {
			for run < len(trimmed) && trimmed[run] == trimmed[0] {
				run++
			}
		}
		switch {
		case fenceLen > 0:
			if run >= fenceLen && trimmed[0] == fenceChar && strings.TrimRight(trimmed[run:], " \t\r") == "" {
				out = append(out, span{fenceStart, pos + len(line)})
				fenceLen = 0
			}
		case run >= 3 && !(trimmed[0] == '`' && strings.Contains(trimmed[run:], "`")):
			fenceChar, fenceLen, fenceStart = trimmed[0], run, pos
		default:
			out = append(out, inlineSpans(line, pos)...)
		}
		pos = next
	}
	if fenceLen > 0 {
		out = append(out, span{fenceStart, len(s)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].start < out[j].start })
	return out
}

func inlineSpans(line string, base int) []span {
	var out []span
	for i := 0; i < len(line); {
		if line[i] != '`' {
			i++
			continue
		}
		bs := 0
		for k := i - 1; k >= 0 && line[k] == '\\'; k-- {
			bs++
		}
		if bs%2 == 1 { // escaped backtick cannot open a span
			i++
			continue
		}
		n := 0
		for i+n < len(line) && line[i+n] == '`' {
			n++
		}
		j := i + n
		closed := false
		for j < len(line) {
			if line[j] != '`' {
				j++
				continue
			}
			m := 0
			for j+m < len(line) && line[j+m] == '`' {
				m++
			}
			if m == n {
				out = append(out, span{base + i, base + j + m})
				i = j + m
				closed = true
				break
			}
			j += m
		}
		if !closed {
			i += n
		}
	}
	return out
}

func inSpans(idx int, spans []span) bool {
	for _, sp := range spans {
		if idx >= sp.start && idx < sp.end {
			return true
		}
	}
	return false
}

// SanitizeBody removes the leading frontmatter block and every <!-- --> and
// %% %% comment (inline or multi-line) from a note body. Code fences and
// inline code spans are left untouched. An unterminated comment or
// frontmatter block removes everything to the end of the text.
func SanitizeBody(body string) string {
	_, s, _ := splitFrontmatter(body)
	spans := codeSpans(s)
	var b strings.Builder
	b.Grow(len(s))
	si := 0
	for i := 0; i < len(s); {
		for si < len(spans) && spans[si].start < i {
			si++
		}
		if si < len(spans) && spans[si].start == i {
			b.WriteString(s[i:spans[si].end])
			i = spans[si].end
			continue
		}
		switch {
		case strings.HasPrefix(s[i:], "<!--"):
			if e := strings.Index(s[i+4:], "-->"); e >= 0 {
				i += 4 + e + 3
			} else {
				i = len(s)
			}
		case strings.HasPrefix(s[i:], "%%"):
			if e := strings.Index(s[i+2:], "%%"); e >= 0 {
				i += 2 + e + 2
			} else {
				i = len(s)
			}
		default:
			b.WriteByte(s[i])
			i++
		}
	}
	return b.String()
}

// FrontmatterDescription returns the `description:` scalar from the leading
// frontmatter block, or "".
func FrontmatterDescription(body string) string {
	fm, _, found := splitFrontmatter(body)
	if !found {
		return ""
	}
	lines := strings.Split(strings.ReplaceAll(fm, "\r\n", "\n"), "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, "description:") {
			continue
		}
		val := strings.TrimSpace(strings.TrimPrefix(line, "description:"))
		if val == "|" || val == ">" || strings.HasPrefix(val, "|-") || strings.HasPrefix(val, ">-") ||
			strings.HasPrefix(val, "|+") || strings.HasPrefix(val, ">+") {
			var parts []string
			for _, l := range lines[i+1:] {
				if l != "" && l[0] != ' ' && l[0] != '\t' {
					break
				}
				if t := strings.TrimSpace(l); t != "" {
					parts = append(parts, t)
				}
			}
			return strings.Join(parts, " ")
		}
		if len(val) >= 2 && (val[0] == '"' || val[0] == '\'') && val[len(val)-1] == val[0] {
			val = val[1 : len(val)-1]
		}
		return val
	}
	return ""
}

var (
	wikilinkRe = regexp.MustCompile(`(!?)\[\[([^\]\n]+)\]\]`)
	mdLinkRe   = regexp.MustCompile(`(!?)\[[^\]\n]*\]\(([^)\n]*)\)`)
	imgTagRe   = regexp.MustCompile(`(?is)<img\b[^>]*?\bsrc\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>"']+))`)
	schemeRe   = regexp.MustCompile(`^[a-zA-Z][a-zA-Z\d+.-]*:`)
)

func stripQueryAndHash(href string) string {
	if i := strings.IndexByte(href, '#'); i >= 0 {
		href = href[:i]
	}
	if i := strings.IndexByte(href, '?'); i >= 0 {
		href = href[:i]
	}
	return href
}

func notLocalHref(h string) bool {
	return h == "" || strings.HasPrefix(h, "#") || strings.HasPrefix(h, "//") || schemeRe.MatchString(h)
}

func isExcalidrawName(clean string) bool {
	l := strings.ToLower(clean)
	return strings.HasSuffix(l, ".excalidraw") || strings.HasSuffix(l, ".excalidraw.md")
}

// ExtractAssetRefs lists the local file hrefs referenced by sanitized note
// text, deduplicated, in first-seen order. Code is ignored. Excalidraw
// drawings only count when embedded with ![[...]].
func ExtractAssetRefs(sanitized string) []string {
	spans := codeSpans(sanitized)
	seen := map[string]bool{}
	var out []string
	add := func(h string) {
		if !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	// accept applies the shared filters; embed marks ![[ ]] embeds.
	accept := func(h string, embed, wiki bool) {
		h = strings.TrimSpace(h)
		if notLocalHref(h) {
			return
		}
		// Classify on the decoded, normalized path, exactly as
		// ResolveAssetRef will resolve it (%2E, trailing slash, ./ ...).
		clean := posixNormalize(decodeHrefPath(h))
		if clean == "" || escapesVault(clean) {
			return
		}
		if isExcalidrawName(clean) {
			if embed && wiki {
				add(h)
			}
			return
		}
		ext := strings.ToLower(path.Ext(clean))
		if wiki && (ext == "" || ext == ".md") {
			return
		}
		if !wiki && ext == ".md" {
			return
		}
		add(h)
	}

	type hit struct {
		idx int
		run func()
	}
	var hits []hit
	for _, m := range wikilinkRe.FindAllStringSubmatchIndex(sanitized, -1) {
		if inSpans(m[0], spans) {
			continue
		}
		embed := m[3] > m[2]
		target := sanitized[m[4]:m[5]]
		if i := strings.IndexByte(target, '|'); i >= 0 {
			target = target[:i]
		}
		target = stripQueryAndHash(strings.TrimSpace(target))
		hits = append(hits, hit{m[0], func() { accept(target, embed, true) }})
	}
	for _, m := range mdLinkRe.FindAllStringSubmatchIndex(sanitized, -1) {
		if inSpans(m[0], spans) {
			continue
		}
		href := mdHref(sanitized[m[4]:m[5]])
		hits = append(hits, hit{m[0], func() { accept(href, false, false) }})
	}
	for _, m := range imgTagRe.FindAllStringSubmatchIndex(sanitized, -1) {
		if inSpans(m[0], spans) {
			continue
		}
		var src string
		for g := 1; g <= 3; g++ {
			if m[2*g] >= 0 {
				src = sanitized[m[2*g]:m[2*g+1]]
			}
		}
		hits = append(hits, hit{m[0], func() { accept(src, false, false) }})
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].idx < hits[j].idx })
	for _, h := range hits {
		h.run()
	}
	return out
}

// mdHref extracts the destination from the inside of a markdown link's
// parentheses, dropping an optional "title".
func mdHref(inner string) string {
	inner = strings.TrimSpace(inner)
	if strings.HasPrefix(inner, "<") {
		if e := strings.IndexByte(inner, '>'); e > 0 {
			return strings.TrimSpace(inner[1:e])
		}
	}
	for i := 0; i+1 < len(inner); i++ {
		if (inner[i] == ' ' || inner[i] == '\t') && strings.ContainsRune("\"'(", rune(strings.TrimLeft(inner[i:], " \t")[0])) {
			return strings.TrimSpace(inner[:i])
		}
	}
	return inner
}

func decodeHrefPath(v string) string {
	cleaned := stripQueryAndHash(v)
	if d, err := url.PathUnescape(cleaned); err == nil {
		return d
	}
	return cleaned
}

// posixNormalize mirrors the TS helper: empty and "." segments are dropped
// and an escaping ".." makes the whole result "..".
func posixNormalize(in string) string {
	var out []string
	for _, p := range strings.Split(in, "/") {
		switch p {
		case "", ".":
		case "..":
			if len(out) == 0 {
				return ".."
			}
			out = out[:len(out)-1]
		default:
			out = append(out, p)
		}
	}
	return strings.Join(out, "/")
}

func escapesVault(p string) bool { return p == ".." || strings.HasPrefix(p, "../") }

func hasDotSegment(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		if strings.HasPrefix(seg, ".") {
			return true
		}
	}
	return false
}

func lastSegment(p string) string {
	segs := strings.FieldsFunc(p, func(r rune) bool { return r == '/' })
	if len(segs) == 0 {
		return ""
	}
	return strings.ToLower(segs[len(segs)-1])
}

// ResolveAssetRef resolves href, written in the note at notePath, to a
// vault-relative path in assetPaths. It ports resolveAssetVaultRelativePath
// (packages/app-core/src/lib/local-assets.ts): note-relative, then
// vault-root-relative, then a unique case-insensitive basename match. It
// returns false for scheme/fragment hrefs, paths escaping the vault, results
// with a dot-prefixed segment, and anything not listed in assetPaths.
func ResolveAssetRef(notePath, href string, assetPaths []string) (string, bool) {
	trimmed := strings.TrimSpace(href)
	if notLocalHref(trimmed) {
		return "", false
	}
	finish := func(p string) (string, bool) {
		if p == "" || escapesVault(p) || hasDotSegment(p) {
			return "", false
		}
		return p, true
	}
	has := func(p string) bool {
		for _, a := range assetPaths {
			if a == p {
				return true
			}
		}
		return false
	}

	noteDir := ""
	if i := strings.LastIndexByte(notePath, '/'); i >= 0 {
		noteDir = notePath[:i]
	}
	decoded := decodeHrefPath(trimmed)
	isAbs := strings.HasPrefix(decoded, "/")
	var target string
	switch {
	case isAbs:
		target = strings.TrimLeft(decoded, "/")
	case noteDir != "":
		target = strings.TrimSuffix(noteDir, "/") + "/" + decoded
	default:
		target = decoded
	}
	target = posixNormalize(target)
	if escapesVault(target) {
		return "", false
	}
	if has(target) {
		return finish(target)
	}

	if !isAbs && noteDir != "" {
		root := posixNormalize(decoded)
		if root != "" && root != target && !escapesVault(root) && has(root) {
			return finish(root)
		}
	}

	base := lastSegment(target)
	if base == "" {
		return "", false
	}
	var match string
	n := 0
	for _, a := range assetPaths {
		if lastSegment(a) == base {
			match = a
			n++
		}
	}
	if n == 1 {
		return finish(match)
	}
	return "", false
}
