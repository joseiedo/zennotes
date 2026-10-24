package httpserver

import (
	"log"
	"net/http"
	"path"
	"strings"

	"github.com/ZenNotes/zennotes/apps/server/internal/vault"
)

// Public-site rules are keyed by path, so a rename, move or duplicate done in
// ZenNotes must carry "hidden" marks to the new path, or a hidden item inside
// a public folder would become public under its new name. Public marks are
// never carried: a moved public item becomes private (it then shows up in the
// settings panel's missing paths).
//
// Ordering, chosen so that content can never be exposed:
//   - publicGate is held (write) across the vault operation AND the rules
//     update; every public request decides visibility and reads content under
//     the read side, so no public request ever sees the moved item before its
//     hidden mark exists at the new path.
//   - the destination path is only known after the operation (names are made
//     unique), so the rules are rewritten right after it — also when the
//     operation fails after the file was already moved (the vault still
//     reports the destination then).
//   - if the rules file cannot be written, the store keeps the carried marks in
//     memory (Get reports them), the failure is logged and the request answers
//     500, so the item stays hidden.

// vaultRel canonicalises a client-supplied vault-relative path the same way
// vault.SafeJoin resolves it, so it matches rule keys.
func vaultRel(rel string) string {
	cleaned := path.Clean("/" + strings.TrimPrefix(strings.ReplaceAll(rel, "\\", "/"), "/"))
	return strings.TrimPrefix(cleaned, "/")
}

// publicMove runs op — a vault operation moving (or, with copy, duplicating)
// content from one vault-relative path to another — and carries hidden marks
// from `from` to `to`. op returns to == "" when nothing was moved. With the
// public site off it just runs op.
func (s *Server) publicMove(copy bool, op func(v *vault.Vault) (from, to string, err error)) error {
	v := s.currentVault()
	if s.publicRules == nil {
		_, _, err := op(v)
		return err
	}
	s.publicGate.Lock()
	defer s.publicGate.Unlock()
	from, to, err := op(v)
	if from != "" && to != "" && from != to {
		if rerr := s.publicRules.RewriteHidden(v.Root(), from, to, copy); rerr != nil {
			log.Printf("public site: could not persist hidden marks moved from %q to %q (kept in memory until restart): %v", from, to, rerr)
			if err == nil {
				err = httpStatusError{code: http.StatusInternalServerError, msg: "moved, but the public site rules could not be saved; the item stays hidden until restart — check the server log"}
			}
		}
	}
	return err
}

// folderRel is the vault-relative path of a folder subpath, "" if invalid.
func folderRel(v *vault.Vault, folder vault.NoteFolder, subpath string) string {
	rel, err := v.FolderVaultPath(folder, subpath)
	if err != nil || rel == "." {
		return ""
	}
	return rel
}

// noteMove adapts a NoteMeta-returning vault call to publicMove.
func (s *Server) noteMove(copy bool, rel string, call func(v *vault.Vault) (vault.NoteMeta, error)) (vault.NoteMeta, error) {
	var meta vault.NoteMeta
	err := s.publicMove(copy, func(v *vault.Vault) (string, string, error) {
		var err error
		meta, err = call(v)
		return vaultRel(rel), meta.Path, err
	})
	return meta, err
}

// assetMove adapts an AssetMeta-returning vault call to publicMove.
func (s *Server) assetMove(copy bool, rel string, call func(v *vault.Vault) (vault.AssetMeta, error)) (vault.AssetMeta, error) {
	var meta vault.AssetMeta
	err := s.publicMove(copy, func(v *vault.Vault) (string, string, error) {
		var err error
		meta, err = call(v)
		return vaultRel(rel), meta.Path, err
	})
	return meta, err
}
