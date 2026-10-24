package publicsite

import (
	"testing"

	"github.com/ZenNotes/zennotes/apps/server/internal/vault"
)

func testPolicy() Policy {
	return NewPolicy(SiteRules{Rules: map[string]Mark{
		"Blog":              MarkPublic,
		"Blog/Drafts":       MarkHidden,
		"Blog/Drafts/Ready": MarkPublic,
		"Notes/one.md":      MarkPublic,
	}})
}

func TestPolicyIsPublic(t *testing.T) {
	p := testPolicy()
	cases := map[string]bool{
		"Blog/a.md":              true,
		"Blog/Drafts/x.md":       false,
		"Blog/Drafts/Ready/y.md": true,
		"Notes/one.md":           true,
		"Notes/two.md":           false,
		"Other/z.md":             false,
		"Blog/.secret/a.md":      false,
		"../Blog/a.md":           false,
		"/Blog/a.md":             false,
		"Blog\\a.md":             false,
		"blog/a.md":              false,
		"":                       false,
		"Blog/../Blog/a.md":      false,
	}
	for in, want := range cases {
		if got := p.IsPublic(in); got != want {
			t.Errorf("IsPublic(%q)=%v want %v", in, got, want)
		}
	}
}

func TestPolicyIsPublicNote(t *testing.T) {
	p := testPolicy()
	if p.IsPublicNote(vault.NoteMeta{Path: "Blog/t.md", Folder: vault.FolderTrash}) {
		t.Error("trash note must be private")
	}
	if p.IsPublicNote(vault.NoteMeta{Path: "Blog/d.excalidraw"}) {
		t.Error("excalidraw must not be a note")
	}
	if !p.IsPublicNote(vault.NoteMeta{Path: "Blog/a.md"}) {
		t.Error("public md should be public")
	}
}
