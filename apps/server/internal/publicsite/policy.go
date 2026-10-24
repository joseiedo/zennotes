// Package publicsite decides which vault content may be served by the opt-in
// public read-only site. Every public handler must go through Policy; it
// always fails closed.
package publicsite

import (
	"strings"

	"github.com/ZenNotes/zennotes/apps/server/internal/vault"
)

// Policy answers visibility questions for one vault's rules. It is a cheap
// value type: building one only keeps a reference to the rules map.
type Policy struct {
	rules map[string]Mark
}

func NewPolicy(r SiteRules) Policy {
	return Policy{rules: r.Rules}
}

// IsPublic reports whether the vault-relative POSIX path rel is public. The
// nearest mark on the path or its ancestors wins; no mark means private.
func (p Policy) IsPublic(rel string) bool {
	if rel == "" || strings.Contains(rel, "\\") || strings.HasPrefix(rel, "/") {
		return false
	}
	segs := strings.Split(rel, "/")
	for _, s := range segs {
		if s == "" || s == "." || s == ".." || strings.HasPrefix(s, ".") {
			return false
		}
	}
	for i := len(segs); i >= 1; i-- {
		if m, ok := p.rules[strings.Join(segs[:i], "/")]; ok {
			return m == MarkPublic
		}
	}
	return false
}

// IsPublicNote reports whether m is a public markdown note.
func (p Policy) IsPublicNote(m vault.NoteMeta) bool {
	return m.Folder != vault.FolderTrash &&
		strings.HasSuffix(m.Path, ".md") &&
		p.IsPublic(m.Path)
}
