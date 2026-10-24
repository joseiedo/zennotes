package publicsite

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestStoreMissingFileIsEmpty(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "public-site.json"))
	r := s.Get("/v1")
	if r.HomeNote != "" || len(r.Rules) != 0 {
		t.Fatalf("want empty, got %+v", r)
	}
}

func TestStoreCorruptFileIsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "public-site.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := NewStore(path).Get("/v1")
	if r.HomeNote != "" || len(r.Rules) != 0 {
		t.Fatalf("want empty, got %+v", r)
	}
}

func TestStoreRoundTripPerVault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "public-site.json")
	s := NewStore(path)
	in := SiteRules{HomeNote: "Blog/index.md", Rules: map[string]Mark{"Blog": MarkPublic}}
	if err := s.Put("/v1", in); err != nil {
		t.Fatal(err)
	}
	if got := s.Get("/v1"); !reflect.DeepEqual(got, in) {
		t.Fatalf("got %+v", got)
	}
	if got := NewStore(path).Get("/v1"); !reflect.DeepEqual(got, in) {
		t.Fatalf("reload got %+v", got)
	}
	if r := s.Get("/v2"); r.HomeNote != "" || len(r.Rules) != 0 {
		t.Fatalf("v2 should be empty, got %+v", r)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", info.Mode().Perm())
	}
}

func TestStoreRejectsBadKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "public-site.json")
	s := NewStore(path)
	good := SiteRules{Rules: map[string]Mark{"Blog": MarkPublic}}
	if err := s.Put("/v1", good); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	bad := []SiteRules{
		{Rules: map[string]Mark{"../x": MarkPublic}},
		{Rules: map[string]Mark{"/abs": MarkPublic}},
		{Rules: map[string]Mark{"a\\b": MarkPublic}},
		{Rules: map[string]Mark{"": MarkPublic}},
		{Rules: map[string]Mark{"a/../b": MarkPublic}},
		{Rules: map[string]Mark{"Blog": Mark("maybe")}},
	}
	for _, b := range bad {
		if err := s.Put("/v1", b); err == nil {
			t.Errorf("expected error for %+v", b)
		}
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("file changed after rejected puts")
	}
}
