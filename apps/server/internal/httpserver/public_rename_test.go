package httpserver

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZenNotes/zennotes/apps/server/internal/publicsite"
)

// adminPost calls an admin API route with the bearer token and decodes the
// JSON answer (if any) into out.
func adminPost(t *testing.T, f *publicFixture, route string, body any, out any) int {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, f.srv.URL+"/admin/api"+route, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer secret-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", route, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST %s: %d %s", route, resp.StatusCode, raw)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("POST %s: decode %q: %v", route, raw, err)
		}
	}
	return resp.StatusCode
}

func renameFixtureFiles() map[string]string {
	return map[string]string{
		"Blog/index.md":             "# Index\n\n![](img/pub.png)\n",
		"Blog/secret.md":            "SECRET-NOTE body\n",
		"Blog/Drafts/d.md":          "SECRET-DRAFT body\n",
		"Blog/img/pub.png":          string(pngBytes),
		"Blog/img/secret.png":       string(pngBytes),
		"Blog/img/sub/.keep.png":    string(pngBytes),
		"Blog/Other/placeholder.md": "public\n",
		"Private/p.md":              "SECRET-PRIVATE body\n",
	}
}

func renameFixtureRules() map[string]publicsite.Mark {
	return map[string]publicsite.Mark{
		"Blog":                publicsite.MarkPublic,
		"Blog/secret.md":      publicsite.MarkHidden,
		"Blog/Drafts":         publicsite.MarkHidden,
		"Blog/img/secret.png": publicsite.MarkHidden,
		"archive":             publicsite.MarkPublic,
	}
}

func (f *publicFixture) policy() publicsite.Policy {
	return publicsite.NewPolicy(f.s.publicRules.Get(f.s.currentVault().Root()))
}

// assertNotePrivate checks a note path through the policy and every public
// endpoint that would serve it.
func assertNotePrivate(t *testing.T, f *publicFixture, rel string) {
	t.Helper()
	if f.policy().IsPublic(rel) {
		t.Errorf("%s is public after the move: rules %+v", rel, f.s.publicRules.Get(f.s.currentVault().Root()).Rules)
	}
	if r := fetchRaw(t, f.srv, apiPath("note", map[string]string{"path": rel})); r.Status != http.StatusNotFound {
		t.Errorf("note API %s: %d %s", rel, r.Status, r.Body)
	}
	if r := fetchRaw(t, f.srv, publicPageURL(rel)); r.Status != http.StatusNotFound {
		t.Errorf("page %s: %d", rel, r.Status)
	}
}

func TestRenameKeepsHiddenNotesPrivate(t *testing.T) {
	cases := []struct {
		name string
		run  func(t *testing.T, f *publicFixture) []string
	}{
		{"notes/rename", func(t *testing.T, f *publicFixture) []string {
			var meta struct{ Path string }
			adminPost(t, f, "/notes/rename", map[string]string{"path": "Blog/secret.md", "title": "renamed"}, &meta)
			return []string{meta.Path}
		}},
		{"notes/rename case-only", func(t *testing.T, f *publicFixture) []string {
			var meta struct{ Path string }
			adminPost(t, f, "/notes/rename", map[string]string{"path": "Blog/secret.md", "title": "Secret"}, &meta)
			return []string{meta.Path}
		}},
		{"notes/move", func(t *testing.T, f *publicFixture) []string {
			var meta struct{ Path string }
			adminPost(t, f, "/notes/move", map[string]string{"path": "Blog/secret.md", "targetFolder": "inbox", "targetSubpath": "Blog/Other"}, &meta)
			return []string{meta.Path}
		}},
		{"notes/duplicate", func(t *testing.T, f *publicFixture) []string {
			var meta struct{ Path string }
			adminPost(t, f, "/notes/duplicate", map[string]string{"path": "Blog/secret.md"}, &meta)
			return []string{meta.Path, "Blog/secret.md"}
		}},
		{"notes/archive", func(t *testing.T, f *publicFixture) []string {
			var meta struct{ Path string }
			adminPost(t, f, "/notes/archive", map[string]string{"path": "Blog/secret.md"}, &meta)
			return []string{meta.Path}
		}},
		{"notes/archive+unarchive onto a taken name", func(t *testing.T, f *publicFixture) []string {
			var meta struct{ Path string }
			adminPost(t, f, "/notes/archive", map[string]string{"path": "Blog/secret.md"}, &meta)
			if err := os.WriteFile(filepath.Join(f.root, "Blog", "secret.md"), []byte("new\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			adminPost(t, f, "/notes/unarchive", map[string]string{"path": meta.Path}, &meta)
			return []string{meta.Path}
		}},
		{"notes/trash+restore onto a taken name", func(t *testing.T, f *publicFixture) []string {
			var meta struct{ Path string }
			adminPost(t, f, "/notes/trash", map[string]string{"path": "Blog/secret.md"}, &meta)
			if err := os.WriteFile(filepath.Join(f.root, "Blog", "secret.md"), []byte("new\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			adminPost(t, f, "/notes/restore", map[string]string{"path": meta.Path}, &meta)
			return []string{meta.Path}
		}},
		{"folders/rename", func(t *testing.T, f *publicFixture) []string {
			var out struct{ Subpath string }
			adminPost(t, f, "/folders/rename", map[string]string{"folder": "inbox", "oldSubpath": "Blog/Drafts", "newSubpath": "Blog/Later"}, &out)
			return []string{out.Subpath + "/d.md"}
		}},
		{"folders/rename case-only", func(t *testing.T, f *publicFixture) []string {
			var out struct{ Subpath string }
			adminPost(t, f, "/folders/rename", map[string]string{"folder": "inbox", "oldSubpath": "Blog/Drafts", "newSubpath": "Blog/drafts"}, &out)
			return []string{out.Subpath + "/d.md"}
		}},
		{"notes/archive of a note hidden by its folder", func(t *testing.T, f *publicFixture) []string {
			var meta struct{ Path string }
			adminPost(t, f, "/notes/archive", map[string]string{"path": "Blog/Drafts/d.md"}, &meta)
			return []string{meta.Path}
		}},
		{"notes/move of an unmarked private note into a public folder", func(t *testing.T, f *publicFixture) []string {
			var meta struct{ Path string }
			adminPost(t, f, "/notes/move", map[string]string{"path": "Private/p.md", "targetFolder": "inbox", "targetSubpath": "Blog"}, &meta)
			return []string{meta.Path}
		}},
		{"folders/duplicate", func(t *testing.T, f *publicFixture) []string {
			var out struct{ Subpath string }
			adminPost(t, f, "/folders/duplicate", map[string]string{"folder": "inbox", "subpath": "Blog/Drafts"}, &out)
			return []string{out.Subpath + "/d.md", "Blog/Drafts/d.md"}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newPublicFixture(t, renameFixtureFiles(), renameFixtureRules(), "Blog/index.md")
			for _, p := range c.run(t, f) {
				if p == "" || p == "/d.md" {
					t.Fatal("route returned no path")
				}
				assertNotePrivate(t, f, p)
			}
			// Unrelated public content stays public.
			if r := fetchRaw(t, f.srv, "/Blog/index"); r.Status != http.StatusOK {
				t.Errorf("public index: %d", r.Status)
			}
		})
	}
}

func TestRenameKeepsHiddenAssetsPrivate(t *testing.T) {
	cases := []struct {
		name string
		run  func(t *testing.T, f *publicFixture) []string
	}{
		{"assets/rename", func(t *testing.T, f *publicFixture) []string {
			var meta struct{ Path string }
			adminPost(t, f, "/assets/rename", map[string]string{"path": "Blog/img/secret.png", "name": "renamed.png"}, &meta)
			return []string{meta.Path}
		}},
		{"assets/move", func(t *testing.T, f *publicFixture) []string {
			var meta struct{ Path string }
			adminPost(t, f, "/assets/move", map[string]string{"path": "Blog/img/secret.png", "targetDir": "Blog/Other"}, &meta)
			return []string{meta.Path}
		}},
		{"assets/duplicate", func(t *testing.T, f *publicFixture) []string {
			var meta struct{ Path string }
			adminPost(t, f, "/assets/duplicate", map[string]string{"path": "Blog/img/secret.png"}, &meta)
			return []string{meta.Path, "Blog/img/secret.png"}
		}},
		{"assets/delete+restore onto a taken name", func(t *testing.T, f *publicFixture) []string {
			var deleted map[string]any
			adminPost(t, f, "/assets/delete", map[string]string{"path": "Blog/img/secret.png"}, &deleted)
			if err := os.WriteFile(filepath.Join(f.root, "Blog", "img", "secret.png"), pngBytes, 0o644); err != nil {
				t.Fatal(err)
			}
			var meta struct{ Path string }
			adminPost(t, f, "/assets/restore", deleted, &meta)
			return []string{meta.Path}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newPublicFixture(t, renameFixtureFiles(), renameFixtureRules(), "Blog/index.md")
			for _, p := range c.run(t, f) {
				if p == "" {
					t.Fatal("route returned no path")
				}
				if f.policy().IsPublic(p) {
					t.Errorf("%s is public after the move: rules %+v", p, f.s.publicRules.Get(f.s.currentVault().Root()).Rules)
				}
			}
		})
	}
}

// A renamed public item becomes private (public marks are not carried).
func TestRenamePublicFolderBecomesPrivate(t *testing.T) {
	f := newPublicFixture(t, map[string]string{"Pub/a.md": "x\n", "Pub/Drafts/d.md": "SECRET\n"},
		map[string]publicsite.Mark{"Pub": publicsite.MarkPublic, "Pub/Drafts": publicsite.MarkHidden}, "")
	var out struct{ Subpath string }
	adminPost(t, f, "/folders/rename", map[string]string{"folder": "inbox", "oldSubpath": "Pub", "newSubpath": "Moved"}, &out)
	assertNotePrivate(t, f, "Moved/a.md")
	assertNotePrivate(t, f, "Moved/Drafts/d.md")
	rules := f.s.publicRules.Get(f.s.currentVault().Root()).Rules
	if rules["Moved/Drafts"] != publicsite.MarkHidden || rules["Pub"] != publicsite.MarkPublic {
		t.Fatalf("rules = %+v", rules)
	}
}

// With the public site off nothing touches rules and the routes behave as before.
func TestRenameWithPublicSiteOff(t *testing.T) {
	_, base, jar := newPublicSiteAdmin(t, false)
	client := &http.Client{Jar: jar}
	body, _ := json.Marshal(map[string]string{"folder": "inbox", "subpath": "X"})
	resp, err := client.Post(base+"/api/folders/create", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	body, _ = json.Marshal(map[string]string{"folder": "inbox", "oldSubpath": "X", "newSubpath": "Y"})
	resp, err = client.Post(base+"/api/folders/rename", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("rename: %d", resp.StatusCode)
	}
}

// If the rules file cannot be written after the move, the request fails and
// the moved item still stays private.
func TestRenameRulesWriteFailureFailsClosed(t *testing.T) {
	f := newPublicFixture(t, renameFixtureFiles(), renameFixtureRules(), "Blog/index.md")
	dir := t.TempDir()
	store := publicsite.NewStore(filepath.Join(dir, "public-site.json"))
	if err := store.Put(f.s.currentVault().Root(), f.s.publicRules.Get(f.s.currentVault().Root())); err != nil {
		t.Fatal(err)
	}
	f.s.publicRules = store
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	b, _ := json.Marshal(map[string]string{"folder": "inbox", "oldSubpath": "Blog/Drafts", "newSubpath": "Blog/Later"})
	req, _ := http.NewRequest(http.MethodPost, f.srv.URL+"/admin/api/folders/rename", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer secret-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	assertNotePrivate(t, f, "Blog/Later/d.md")
}
