package publicsite

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func newRewriteStore(t *testing.T, rules map[string]Mark) *Store {
	t.Helper()
	s := NewStore(filepath.Join(t.TempDir(), "public-site.json"))
	if err := s.Put("/v", SiteRules{HomeNote: "Blog/index.md", Rules: rules}); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRewriteHiddenMovesHiddenMarksOnly(t *testing.T) {
	s := newRewriteStore(t, map[string]Mark{
		"Blog":               MarkPublic,
		"Blog/Drafts":        MarkHidden,
		"Blog/Drafts/x.md":   MarkHidden,
		"Blog/Drafts/pub.md": MarkPublic,
		"Blog/DraftsOther":   MarkHidden,
	})
	if err := s.RewriteHidden("/v", "Blog/Drafts", "Blog/drafts", false); err != nil {
		t.Fatal(err)
	}
	want := map[string]Mark{
		"Blog":               MarkPublic,
		"Blog/drafts":        MarkHidden,
		"Blog/drafts/x.md":   MarkHidden,
		"Blog/Drafts/pub.md": MarkPublic, // public marks are never carried
		"Blog/DraftsOther":   MarkHidden, // a sibling sharing the prefix string is untouched
	}
	got := s.Get("/v")
	if !reflect.DeepEqual(got.Rules, want) || got.HomeNote != "Blog/index.md" {
		t.Fatalf("got %+v", got)
	}
	if !reflect.DeepEqual(NewStore(s.path).Get("/v").Rules, want) {
		t.Fatal("rewrite not persisted")
	}
}

func TestRewriteHiddenCopyKeepsSource(t *testing.T) {
	s := newRewriteStore(t, map[string]Mark{"Blog": MarkPublic, "Blog/a.md": MarkHidden})
	if err := s.RewriteHidden("/v", "Blog/a.md", "Blog/a copy.md", true); err != nil {
		t.Fatal(err)
	}
	want := map[string]Mark{"Blog": MarkPublic, "Blog/a.md": MarkHidden, "Blog/a copy.md": MarkHidden}
	if got := s.Get("/v").Rules; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
}

func TestRewriteHiddenOverridesPublicMarkAtDestination(t *testing.T) {
	s := newRewriteStore(t, map[string]Mark{"Blog/a.md": MarkHidden, "Blog/b.md": MarkPublic})
	if err := s.RewriteHidden("/v", "Blog/a.md", "Blog/b.md", false); err != nil {
		t.Fatal(err)
	}
	if got := s.Get("/v").Rules["Blog/b.md"]; got != MarkHidden {
		t.Fatalf("destination mark = %q", got)
	}
}

func TestRewriteHiddenNoHiddenMarksDoesNotWrite(t *testing.T) {
	s := newRewriteStore(t, map[string]Mark{"Blog": MarkPublic})
	dir := filepath.Dir(s.path)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if err := s.RewriteHidden("/v", "Blog/a.md", "Blog/b.md", false); err != nil {
		t.Fatalf("no-op rewrite failed: %v", err)
	}
}

// A rules file that cannot be written must not let the moved item go public:
// the hidden mark is kept in memory and Get still reports it.
func TestRewriteHiddenWriteFailureFailsClosed(t *testing.T) {
	s := newRewriteStore(t, map[string]Mark{"Blog": MarkPublic, "Blog/Drafts": MarkHidden})
	dir := filepath.Dir(s.path)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if err := s.RewriteHidden("/v", "Blog/Drafts", "Blog/Later", false); err == nil {
		t.Fatal("expected write error")
	}
	p := NewPolicy(s.Get("/v"))
	if p.IsPublic("Blog/Later/x.md") || p.IsPublic("Blog/Drafts/x.md") {
		t.Fatalf("hidden content exposed after failed rules write: %+v", s.Get("/v").Rules)
	}
	if !p.IsPublic("Blog/other.md") {
		t.Fatal("unrelated public content lost")
	}
	// Once the file is writable again, the next rewrite persists the pending mark.
	_ = os.Chmod(dir, 0o700)
	if err := s.Put("/v", s.Get("/v")); err != nil {
		t.Fatal(err)
	}
	if NewPolicy(NewStore(s.path).Get("/v")).IsPublic("Blog/Later/x.md") {
		t.Fatal("pending hidden mark not persisted by the next Put")
	}
}

// A move must never make content public: a source that was not public (hidden
// by an ancestor, or simply unmarked) gets a hidden mark at a destination
// that would otherwise be public.
func TestRewriteHiddenHidesSourceThatWasNotPublic(t *testing.T) {
	cases := []struct{ name, from, to string }{
		{"hidden by its folder, archived into a public archive", "Blog/Drafts/d.md", "archive/Blog/Drafts/d.md"},
		{"unmarked private note moved into a public folder", "Private/p.md", "Blog/p.md"},
		{"unmarked private folder moved into a public folder", "Private", "Blog/Private"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newRewriteStore(t, map[string]Mark{"Blog": MarkPublic, "Blog/Drafts": MarkHidden, "archive": MarkPublic})
			if err := s.RewriteHidden("/v", c.from, c.to, false); err != nil {
				t.Fatal(err)
			}
			if got := s.Get("/v").Rules[c.to]; got != MarkHidden {
				t.Fatalf("destination mark = %q, rules %+v", got, s.Get("/v").Rules)
			}
			if NewPolicy(NewStore(s.path).Get("/v")).IsPublic(c.to) {
				t.Fatal("hidden destination mark not persisted")
			}
		})
	}
}

func TestRewriteHiddenLeavesPublicSourceAlone(t *testing.T) {
	s := newRewriteStore(t, map[string]Mark{"Blog": MarkPublic, "archive": MarkPublic})
	if err := s.RewriteHidden("/v", "Blog/a.md", "archive/Blog/a.md", false); err != nil {
		t.Fatal(err)
	}
	want := map[string]Mark{"Blog": MarkPublic, "archive": MarkPublic}
	if got := s.Get("/v").Rules; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
}

func TestRewriteHiddenPrivateDestinationNeedsNoMark(t *testing.T) {
	s := newRewriteStore(t, map[string]Mark{"Blog": MarkPublic})
	if err := s.RewriteHidden("/v", "Private/p.md", "Other/p.md", false); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("/v").Rules["Other/p.md"]; ok {
		t.Fatalf("unneeded mark added: %+v", s.Get("/v").Rules)
	}
}
