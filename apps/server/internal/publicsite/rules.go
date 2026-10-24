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
	// pending holds hidden marks that a RewriteHidden could not persist, per
	// vault root. Get overlays them so a failed rules write never makes a
	// moved hidden item public; the next successful write persists them.
	pending map[string]map[string]Mark
}

func NewStore(path string) *Store { return &Store{path: path} }

// rulesLocked returns root's rules with pending hidden marks applied. The
// caller holds s.mu.
func (s *Store) rulesLocked(f rulesFile, root string) SiteRules {
	r := f.Vaults[root]
	rules := make(map[string]Mark, len(r.Rules)+len(s.pending[root]))
	for k, m := range r.Rules {
		rules[k] = m
	}
	for k, m := range s.pending[root] {
		rules[k] = m
	}
	r.Rules = rules
	return r
}

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
	return s.rulesLocked(s.load(), vaultRoot)
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
	if err := s.saveLocked(f); err != nil {
		return err
	}
	delete(s.pending, vaultRoot)
	return nil
}

func (s *Store) saveLocked(f rulesFile) error {
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(s.path, data)
}

// RewriteHidden carries every hidden mark at oldPrefix or under it
// (oldPrefix/...) to the same place under newPrefix, after a rename, move or
// (with copy) duplicate. Public marks are never carried: a moved public item
// becomes private. A hidden mark wins over any mark already at the
// destination. When the rules file cannot be written the carried marks stay
// in memory (Get reports them) and the error is returned, so the moved item
// is still hidden while the process runs.
func (s *Store) RewriteHidden(vaultRoot, oldPrefix, newPrefix string, copy bool) error {
	if oldPrefix == newPrefix || !validKey(oldPrefix) || !validKey(newPrefix) {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.load()
	r := s.rulesLocked(f, vaultRoot)
	carried := map[string]Mark{}
	for k, m := range r.Rules {
		if m != MarkHidden {
			continue
		}
		var rest string
		switch {
		case k == oldPrefix:
		case strings.HasPrefix(k, oldPrefix+"/"):
			rest = k[len(oldPrefix):]
		default:
			continue
		}
		carried[newPrefix+rest] = MarkHidden
		if !copy {
			delete(r.Rules, k)
		}
	}
	if len(carried) == 0 {
		return nil
	}
	for k, m := range carried {
		r.Rules[k] = m
	}
	// Remember the carried marks before touching the disk.
	if s.pending == nil {
		s.pending = map[string]map[string]Mark{}
	}
	if s.pending[vaultRoot] == nil {
		s.pending[vaultRoot] = map[string]Mark{}
	}
	for k, m := range carried {
		s.pending[vaultRoot][k] = m
	}
	f.Vaults[vaultRoot] = r
	if err := s.saveLocked(f); err != nil {
		return err
	}
	delete(s.pending, vaultRoot)
	return nil
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
