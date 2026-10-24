package httpserver

import (
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
}

func (s *Server) publicSiteState(rules publicsite.SiteRules) (publicSiteResponse, error) {
	v := s.currentVault()
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
	var in publicsite.SiteRules
	if err := readJSON(r, &in); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	root := s.currentVault().Root()
	if err := s.publicRules.Put(root, in); err != nil {
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
