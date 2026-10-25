package httpserver

import (
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/ZenNotes/zennotes/apps/server/internal/publicsite"
)

type publicSiteResponse struct {
	HomeNote       string                     `json:"homeNote"`
	Rules          map[string]publicsite.Mark `json:"rules"`
	MissingPaths   []string                   `json:"missingPaths"`
	HomeNotePublic bool                       `json:"homeNotePublic"`
	// Version must be sent back with a PUT; a stale one gets 409.
	Version string `json:"version"`
}

func (s *Server) publicSiteState(rules publicsite.SiteRules) (publicSiteResponse, error) {
	v := s.currentVault()
	version := s.publicRules.Version(v.Root())
	notes, err := v.ListNotes()
	if err != nil {
		return publicSiteResponse{}, err
	}
	assets, err := v.ListAssets()
	if err != nil {
		return publicSiteResponse{}, err
	}
	if rules.Rules == nil {
		rules.Rules = map[string]publicsite.Mark{}
	}
	policy := publicsite.NewPolicy(rules)

	paths := make([]string, 0, len(notes)+len(assets))
	homePublic := false
	for _, n := range notes {
		paths = append(paths, n.Path)
		if rules.HomeNote != "" && n.Path == rules.HomeNote && s.isServedPublicPage(policy, n) {
			homePublic = true
		}
	}
	for _, a := range assets {
		paths = append(paths, a.Path)
	}

	missing := []string{}
	for key := range rules.Rules {
		found := false
		for _, p := range paths {
			if p == key || strings.HasPrefix(p, key+"/") {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)

	return publicSiteResponse{
		HomeNote:       rules.HomeNote,
		Rules:          rules.Rules,
		MissingPaths:   missing,
		HomeNotePublic: homePublic,
		Version:        version,
	}, nil
}

func (s *Server) getPublicSite(w http.ResponseWriter, _ *http.Request) {
	resp, err := s.publicSiteState(s.publicRules.Get(s.currentVault().Root()))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) putPublicSite(w http.ResponseWriter, r *http.Request) {
	var in struct {
		publicsite.SiteRules
		Version string `json:"version"`
	}
	if err := readJSON(r, &in); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	root := s.currentVault().Root()
	if err := s.publicRules.PutIfVersion(root, in.SiteRules, in.Version); err != nil {
		if errors.Is(err, publicsite.ErrStaleRules) {
			http.Error(w, "the public site rules changed since this page loaded them (e.g. a rename elsewhere); reload and try again", http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	resp, err := s.publicSiteState(s.publicRules.Get(root))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}
