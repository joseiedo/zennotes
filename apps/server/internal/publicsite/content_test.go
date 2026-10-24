package publicsite

import (
	"sort"
	"strings"
	"testing"
)

func TestSanitizeStripsFrontmatterAndComments(t *testing.T) {
	in := "---\ntitle: x\nsecret: y\n---\nHello %%hidden%% world <!-- c --> end\n%%\nmulti\n%%\n"
	out := SanitizeBody(in)
	for _, want := range []string{"Hello", "world", "end"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %q", want, out)
		}
	}
	for _, bad := range []string{"secret", "hidden", "multi", "<!--", "%%", "title"} {
		if strings.Contains(out, bad) {
			t.Errorf("leaked %q in %q", bad, out)
		}
	}
}

func TestSanitizeKeepsCodeBlocks(t *testing.T) {
	in := "intro\n```\n%%keep%%\n<!-- keep -->\n---\n```\n~~~~\n%%keep3%%\n~~~\n%%x%%\n~~~~\ninline `%%keep2%%` and ``a ` %%keep4%% ``\n%%gone%%\n"
	out := SanitizeBody(in)
	for _, want := range []string{"%%keep%%", "<!-- keep -->", "---", "%%keep3%%", "`%%keep2%%`", "%%keep4%%"} {
		if !strings.Contains(out, want) {
			t.Errorf("lost %q in %q", want, out)
		}
	}
	if strings.Contains(out, "gone") {
		t.Errorf("leaked: %q", out)
	}
}

func TestSanitizeFailsClosed(t *testing.T) {
	for _, in := range []string{
		"a <!-- never closed secret",
		"a %% never closed secret",
		"---\ntitle: x\nsecret: never closed",
		"\ufeff---\r\nsecret: y\r\n---\r\nbody",
	} {
		if out := SanitizeBody(in); strings.Contains(out, "secret") {
			t.Errorf("leaked in %q -> %q", in, out)
		}
	}
	// A comment spanning a code fence hides the fence too.
	if out := SanitizeBody("%%\n```\nsecret\n```\n%%\nok"); strings.Contains(out, "secret") || !strings.Contains(out, "ok") {
		t.Errorf("got %q", out)
	}
	// Frontmatter only at the very start.
	if out := SanitizeBody("hi\n---\na: b\n---\n"); !strings.Contains(out, "a: b") {
		t.Errorf("got %q", out)
	}
}

func TestFrontmatterDescription(t *testing.T) {
	cases := map[string]string{
		"---\ntitle: t\ndescription: Hi there\n---\nbody": "Hi there",
		"---\ndescription: \"Quoted: yes\"\n---\n":        "Quoted: yes",
		"---\ndescription: >\n  folded\n  text\n---\n":    "folded text",
		"no frontmatter\ndescription: nope":               "",
		"---\ntitle: t\n---\ndescription: body":           "",
	}
	for in, want := range cases {
		if got := FrontmatterDescription(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestExtractAssetRefs(t *testing.T) {
	body := "![[img.png|300]]\n![a](sub/pic.jpg \"t\")\n[pdf](docs/a.pdf)\n[[Other note]]\n[web](https://x.y/a.png)\n" +
		"<img src=\"b.gif\">\n![[draw.excalidraw]]\n[[link.excalidraw]]\n`![[nope.png]]`\n" +
		"```\n![[nope2.png]]\n```\n[[file.pdf#page=2]]\n[[note.md]]\n![[o.excalidraw.md]]\n[[o2.excalidraw.md]]\n[x](#a) [y](//h/p.png)\n"
	got := ExtractAssetRefs("post.md", body)
	sort.Strings(got)
	want := []string{"b.gif", "docs/a.pdf", "draw.excalidraw", "file.pdf", "img.png", "o.excalidraw.md", "sub/pic.jpg"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestResolveAssetRef(t *testing.T) {
	assets := []string{"attachments/img.png", "Blog/sub/pic.jpg", ".private/x.png", "Blog/b.gif", "a/dup.png", "b/dup.png"}
	cases := []struct {
		href string
		want string
		ok   bool
	}{
		{"img.png", "attachments/img.png", true},
		{"sub/pic.jpg", "Blog/sub/pic.jpg", true},
		{"b.gif", "Blog/b.gif", true},
		{"attachments/img.png", "attachments/img.png", true},
		{"sub/pic%2Ejpg?x=1#h", "Blog/sub/pic.jpg", true},
		{"x.png", "", false},
		{".private/x.png", "", false},
		{"../../etc/passwd", "", false},
		{"missing.png", "", false},
		{"dup.png", "", false},
		{"https://a/img.png", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, ok := ResolveAssetRef("Blog/post.md", c.href, assets)
		if got != c.want || ok != c.ok {
			t.Errorf("%q: got (%q,%v) want (%q,%v)", c.href, got, ok, c.want, c.ok)
		}
	}
}

func TestExtractAssetRefsNormalizesBeforeClassifying(t *testing.T) {
	for _, body := range []string{
		"[x](draw%2Eexcalidraw)", "![](draw%2Eexcalidraw)", "[x](draw.excalidraw/)",
		"[x](./draw.excalidraw)", "[x](note%2Emd)", "[x](o%2Eexcalidraw%2Emd)", "<img src=\"d%2Eexcalidraw\">",
	} {
		if got := ExtractAssetRefs("post.md", body); len(got) != 0 {
			t.Errorf("%q -> %v", body, got)
		}
	}
	if got := ExtractAssetRefs("post.md", "![[draw%2Eexcalidraw]]"); len(got) != 1 {
		t.Errorf("embed should count: %v", got)
	}
}

func TestSanitizeCommonMarkCodeStrictness(t *testing.T) {
	for _, in := range []string{
		"para\n\n    ```\n%% secret %%\n```\n",
		"para\n\n\t```\n%% secret %%\n```\n",
		"a \\` %%secret%% ` b",
	} {
		if out := SanitizeBody(in); strings.Contains(out, "secret") {
			t.Errorf("leaked in %q -> %q", in, out)
		}
	}
	if out := SanitizeBody("   ```\n%%keep%%\n   ```\n"); !strings.Contains(out, "%%keep%%") {
		t.Errorf("3-space fence should be code: %q", out)
	}
}

func TestSanitizeFrontmatterOnlyDashCloser(t *testing.T) {
	out := SanitizeBody("---\na: 1\n...\nsecret: y\n---\nbody")
	if strings.Contains(out, "secret") || !strings.Contains(out, "body") {
		t.Errorf("got %q", out)
	}
}

// I4: a "../" href is resolved against the note's folder, as ResolveAssetRef
// does, before the vault-escape check.
func TestExtractAssetRefsNoteRelativeParent(t *testing.T) {
	got := ExtractAssetRefs("Blog/post.md", "![](../attachments/a.png)\n![](../../escape.png)\n<img src=\"../b.gif\">\n")
	want := []string{"../attachments/a.png", "../b.gif"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v want %v", got, want)
	}
	if got := ExtractAssetRefs("post.md", "![](../a.png)"); len(got) != 0 {
		t.Errorf("root note escaping the vault: %v", got)
	}
	if p, ok := ResolveAssetRef("Blog/post.md", "../attachments/a.png", []string{"attachments/a.png"}); !ok || p != "attachments/a.png" {
		t.Errorf("resolve: %q %v", p, ok)
	}
}

// M1: refs are listed without #fragment / ?query for every syntax, matching
// what the public client requests.
func TestExtractAssetRefsDropsFragmentAndQuery(t *testing.T) {
	got := ExtractAssetRefs("post.md", "![[doc.pdf#page=3]]\n[d](other.pdf#page=2)\n![](img.png?v=1)\n<img src=\"c.gif#x\">\n")
	want := []string{"doc.pdf", "other.pdf", "img.png", "c.gif"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v want %v", got, want)
	}
	if got := ExtractAssetRefs("Blog/post.md", "[q](?x=1) [h](#top)"); len(got) != 0 {
		t.Errorf("query/fragment-only hrefs: %v", got)
	}
}
