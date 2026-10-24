package vault

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSearchTextFilteredAppliesFilterBeforeCap(t *testing.T) {
	root := t.TempDir()
	var b strings.Builder
	for i := 0; i < 250; i++ {
		b.WriteString("needle private line\n")
	}
	if err := os.WriteFile(filepath.Join(root, "A.md"), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Z.md"), []byte("needle public line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	v, err := New(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	all, err := v.SearchText("needle")
	if err != nil || len(all) != 200 {
		t.Fatalf("unfiltered: %d %v", len(all), err)
	}
	got, err := v.SearchTextFiltered("needle", func(p string) bool { return p == "Z.md" })
	if err != nil || len(got) != 1 || got[0].Path != "Z.md" {
		t.Fatalf("filtered: %+v %v", got, err)
	}
}
