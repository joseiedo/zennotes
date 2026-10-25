package httpserver

import (
	"log"
	"net/http"
	"strings"

	"github.com/ZenNotes/zennotes/apps/server/internal/publicsite"
	"github.com/ZenNotes/zennotes/apps/server/internal/vault"
)

const (
	maxWorkflowRequestBytes         = 128 << 20
	maxWorkflowMetadataRequestBytes = 64 << 10
)

func (s *Server) listWorkflows(w http.ResponseWriter, _ *http.Request) {
	files, err := s.currentVault().ListWorkflows()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, files)
}

func (s *Server) writeWorkflow(w http.ResponseWriter, r *http.Request) {
	cfg := s.currentConfig()
	r.Body = http.MaxBytesReader(w, r.Body, cfg.MaxNoteBytes+jsonEnvelopeBytes)
	var input vault.WriteWorkflowInput
	if err := readJSON(r, &input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	file, err := s.currentVault().WriteWorkflow(input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, file)
}

func (s *Server) deleteWorkflow(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxWorkflowMetadataRequestBytes)
	var request struct {
		SourcePath string `json:"sourcePath"`
	}
	if err := readJSON(r, &request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.currentVault().DeleteWorkflow(request.SourcePath); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) applyWorkflow(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxWorkflowRequestBytes)
	var input vault.PreparedWorkflowRun
	if err := readJSON(r, &input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cfg := s.currentConfig()
	for _, change := range input.Changes {
		// Only what the run WRITES counts against the limit. Before is the
		// note's bytes already on disk: counting those made an oversized note
		// impossible to shrink, move, or trash from the web client, 413 on
		// every apply, while desktop applied the identical run.
		if cfg.MaxNoteBytes > 0 && change.After != nil && int64(len(*change.After)) > cfg.MaxNoteBytes {
			http.Error(w, "workflow note exceeds the configured note size limit", http.StatusRequestEntityTooLarge)
			return
		}
	}
	receipt, err := s.applyWorkflowRun(input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, receipt)
}

// applyWorkflowRun applies a prepared run. With the public site on it keeps
// the run from publishing anything: the client sends moves as plain file
// changes (delete the old path, create the new one), so when a run removes a
// note that was not public, every note it creates at a public path is hidden.
// A run that only creates notes publishes them like creating them by hand.
// publicGate is held across the apply and the rules update, as for in-app
// moves (see public_moves.go).
func (s *Server) applyWorkflowRun(input vault.PreparedWorkflowRun) (vault.WorkflowRunReceipt, error) {
	v := s.currentVault()
	if s.publicRules == nil {
		return v.ApplyPreparedWorkflow(input)
	}
	s.publicGate.Lock()
	defer s.publicGate.Unlock()
	policy := publicsite.NewPolicy(s.publicRules.Get(v.Root()))
	removedPrivate := false
	var created []string
	for _, change := range input.Changes {
		rel := vaultRel(change.Path)
		switch {
		case change.Before != nil && change.After == nil && !policy.IsPublic(rel):
			removedPrivate = true
		case change.Before == nil && change.After != nil && policy.IsPublic(rel):
			created = append(created, rel)
		}
	}
	receipt, err := v.ApplyPreparedWorkflow(input)
	if err != nil || !removedPrivate || len(created) == 0 {
		return receipt, err
	}
	if rerr := s.publicRules.HidePaths(v.Root(), created); rerr != nil {
		log.Printf("public site: could not persist hidden marks for notes moved by a workflow (kept in memory until restart): %v", rerr)
		return receipt, httpStatusError{code: http.StatusInternalServerError, msg: "workflow applied, but the public site rules could not be saved; the moved notes stay hidden until restart — check the server log"}
	}
	return receipt, nil
}

func (s *Server) undoWorkflowRun(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxWorkflowMetadataRequestBytes)
	var request struct {
		RunID string `json:"runId"`
	}
	if err := readJSON(r, &request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	result, err := s.currentVault().UndoWorkflowRun(request.RunID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) listWorkflowRuns(w http.ResponseWriter, _ *http.Request) {
	runs, err := s.currentVault().ListWorkflowRuns()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, runs)
}

func (s *Server) deleteWorkflowRuns(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxWorkflowMetadataRequestBytes)
	var request struct {
		WorkflowID string `json:"workflowId"`
	}
	if err := readJSON(r, &request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	workflowID := strings.TrimSpace(request.WorkflowID)
	if workflowID == "" {
		http.Error(w, "workflowId is required", http.StatusBadRequest)
		return
	}
	removed, err := s.currentVault().DeleteWorkflowRuns(workflowID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, removed)
}
