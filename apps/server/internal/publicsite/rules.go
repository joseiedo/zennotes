package publicsite

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Mark string

const (
	MarkPublic Mark = "public"
	MarkHidden Mark = "hidden"
)

// SiteRules are one vault's public-site settings. Rule keys are vault-relative
// POSIX paths of folders or files, without leading or trailing slash.
type SiteRules struct {
	HomeNote string          `json:"homeNote"`
	Rules    map[string]Mark `json:"rules"`
}

type rulesFile struct {
	Vaults map[string]SiteRules `json:"vaults"`
}

// Store persists rules for all vaults in one JSON file.
type Store struct {
	path string
	mu   sync.Mutex
}

func NewStore(path string) *Store { return &Store{path: path} }

func (s *Store) load() rulesFile {
	f := rulesFile{}
	data, err := os.ReadFile(s.path)
	if err != nil || json.Unmarshal(data, &f) != nil || f.Vaults == nil {
		return rulesFile{Vaults: map[string]SiteRules{}}
	}
	return f
}

// Get never fails: missing, unreadable or corrupt data yields empty rules.
func (s *Store) Get(vaultRoot string) SiteRules {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.load().Vaults[vaultRoot]
	if r.Rules == nil {
		r.Rules = map[string]Mark{}
	}
	return r
}

func validKey(k string) bool {
	if k == "" || strings.Contains(k, "\\") || strings.HasPrefix(k, "/") || strings.HasSuffix(k, "/") {
		return false
	}
	for _, seg := range strings.Split(k, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

func (s *Store) Put(vaultRoot string, r SiteRules) error {
	for k, m := range r.Rules {
		if !validKey(k) {
			return fmt.Errorf("invalid rule path %q", k)
		}
		if m != MarkPublic && m != MarkHidden {
			return fmt.Errorf("invalid mark %q for %q", m, k)
		}
	}
	if r.Rules == nil {
		r.Rules = map[string]Mark{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.load()
	f.Vaults[vaultRoot] = r
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(s.path, data)
}

func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".public-site-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	fail := func(err error) error {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		return fail(err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}
