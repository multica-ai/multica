package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/daemon/processtree"
)

type graphCredentialResponse struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	ServiceURL      string `json:"service_url"`
	RepositoryCount int    `json:"repository_count"`
	UpdatedAt       string `json:"updated_at"`
}
type graphAuthRequest struct {
	Mode         string `json:"auth_mode"`
	CredentialID string `json:"credential_id"`
	RepoURL      string `json:"repo_url"`
	Branch       string `json:"branch"`
}

func (h *Handler) ListCodeGraphCredentials(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	ws := workspaceIDFromURL(r, "id")
	if !h.requireCodeGraphWriter(w, r, ws) {
		return
	}
	rows, err := h.DB.Query(r.Context(), `SELECT c.id::text,c.name,c.service_url,
 (SELECT count(*) FROM code_graph_repository g WHERE g.workspace_id=c.workspace_id AND g.credential_id=c.id),c.updated_at
 FROM code_graph_git_credential c WHERE c.workspace_id=$1 ORDER BY c.name,c.id`, parseUUID(ws))
	if err != nil {
		writeError(w, 500, "Cannot list Git credentials")
		return
	}
	defer rows.Close()
	items := []graphCredentialResponse{}
	for rows.Next() {
		var item graphCredentialResponse
		var updated pgtype.Timestamptz
		if rows.Scan(&item.ID, &item.Name, &item.ServiceURL, &item.RepositoryCount, &updated) != nil {
			writeError(w, 500, "Cannot read Git credentials")
			return
		}
		item.UpdatedAt = timestampToString(updated)
		items = append(items, item)
	}
	if rows.Err() != nil {
		writeError(w, 500, "Cannot read Git credentials")
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "configured": h.VCSSecretBox != nil})
}

func (h *Handler) SaveCodeGraphCredential(w http.ResponseWriter, r *http.Request) {
	ws := workspaceIDFromURL(r, "id")
	if !h.requireCodeGraphWriter(w, r, ws) {
		return
	}
	if h.VCSSecretBox == nil {
		writeError(w, 503, "Git credential encryption is not configured")
		return
	}
	var req struct {
		Name       string `json:"name"`
		ServiceURL string `json:"service_url"`
		Token      string `json:"token"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 32768)).Decode(&req) != nil {
		writeError(w, 400, "Invalid Git credential")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Token = strings.TrimSpace(req.Token)
	u, err := url.Parse(strings.TrimSpace(req.ServiceURL))
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(req.ServiceURL, "\r\n") || len(req.ServiceURL) > 2000 ||
		req.Name == "" || len(req.Name) > 120 || req.Token == "" || len(req.Token) > 16000 || strings.ContainsAny(req.Token, "\r\n") {
		writeError(w, 400, "Name, HTTPS Git service URL and Token are required")
		return
	}
	service := strings.TrimRight(u.String(), "/")
	encrypted, err := h.sealVCSSecret(req.Token)
	if err != nil {
		writeError(w, 500, "Cannot encrypt Git credential")
		return
	}
	var id string
	if chi.URLParam(r, "credentialId") == "" {
		err = h.DB.QueryRow(r.Context(), `INSERT INTO code_graph_git_credential(workspace_id,name,service_url,token_encrypted,created_by,updated_by)
 VALUES($1,$2,$3,$4,$5,$5) RETURNING id::text`, parseUUID(ws), req.Name, service, encrypted, parseUUID(requestUserID(r))).Scan(&id)
	} else {
		cid, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "credentialId"), "credential id")
		if !ok {
			return
		}
		// Service identity stays fixed on rotation so existing bindings cannot leak tokens to a different host.
		err = h.DB.QueryRow(r.Context(), `UPDATE code_graph_git_credential SET name=$3,token_encrypted=$4,updated_by=$5,updated_at=now()
 WHERE id=$1 AND workspace_id=$2 AND service_url=$6 RETURNING id::text`, cid, parseUUID(ws), req.Name, encrypted, parseUUID(requestUserID(r)), service).Scan(&id)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 409, "Credential unavailable or Git service changed; create a new credential instead")
		return
	}
	if err != nil {
		writeError(w, 500, "Cannot save Git credential")
		return
	}
	writeJSON(w, 200, map[string]string{"id": id})
}

func (h *Handler) DeleteCodeGraphCredential(w http.ResponseWriter, r *http.Request) {
	ws := workspaceIDFromURL(r, "id")
	if !h.requireCodeGraphWriter(w, r, ws) {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "credentialId"), "credential id")
	if !ok {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "Cannot delete Git credential")
		return
	}
	defer tx.Rollback(r.Context())
	var found string
	err = tx.QueryRow(r.Context(), `SELECT id::text FROM code_graph_git_credential WHERE id=$1 AND workspace_id=$2 FOR UPDATE`, id, parseUUID(ws)).Scan(&found)
	if err != nil {
		writeError(w, 404, "Git credential unavailable")
		return
	}
	var count int
	if tx.QueryRow(r.Context(), `SELECT count(*) FROM code_graph_repository WHERE workspace_id=$1 AND credential_id=$2`, parseUUID(ws), id).Scan(&count) != nil {
		writeError(w, 500, "Cannot check credential references")
		return
	}
	if count > 0 {
		writeError(w, 409, "Git credential is still used by repositories; change their credentials first")
		return
	}
	if _, err = tx.Exec(r.Context(), `DELETE FROM code_graph_git_credential WHERE id=$1 AND workspace_id=$2`, id, parseUUID(ws)); err != nil {
		writeError(w, 500, "Cannot delete Git credential")
		return
	}
	if tx.Commit(r.Context()) != nil {
		writeError(w, 500, "Cannot delete Git credential")
		return
	}
	w.WriteHeader(204)
}

func (h *Handler) resolveGraphAuth(ctx context.Context, ws pgtype.UUID, req graphAuthRequest) (*codeGraphGitAuth, error) {
	repo, err := url.Parse(req.RepoURL)
	if err != nil || repo.Host == "" || repo.User != nil || (repo.Scheme != "https" && repo.Scheme != "http") || repo.RawQuery != "" || repo.Fragment != "" {
		return nil, errors.New("Use an HTTP(S) repository URL without embedded credentials")
	}
	if req.Mode == "anonymous" {
		if req.CredentialID != "" {
			return nil, errors.New("Anonymous mode cannot use a credential")
		}
		return nil, nil
	}
	if req.Mode != "token" || req.CredentialID == "" {
		return nil, errors.New("Select a Git credential or choose anonymous access")
	}
	cid, err := parseCredentialUUID(req.CredentialID)
	if err != nil {
		return nil, err
	}
	if h.VCSSecretBox == nil {
		return nil, errors.New("Git credential encryption is not configured")
	}
	var service, user, enc string
	if h.DB.QueryRow(ctx, `SELECT service_url,username,token_encrypted FROM code_graph_git_credential WHERE id=$1 AND workspace_id=$2`, cid, ws).Scan(&service, &user, &enc) != nil {
		return nil, errors.New("Git credential unavailable in this workspace")
	}
	origin, err := url.Parse(service)
	if err != nil || repo.Scheme != "https" || origin.Scheme != "https" || !strings.EqualFold(origin.Host, repo.Host) ||
		!(strings.TrimRight(origin.Path, "/") == "" || strings.HasPrefix(repo.Path, strings.TrimRight(origin.Path, "/")+"/")) {
		return nil, errors.New("Git credential does not match this repository service")
	}
	token, err := h.openVCSSecret(enc)
	if err != nil || token == "" {
		return nil, errors.New("Git credential cannot be decrypted; replace its Token")
	}
	return &codeGraphGitAuth{username: user, password: token}, nil
}

func parseCredentialUUID(s string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if id.Scan(s) != nil || !id.Valid {
		return id, errors.New("Invalid credential id")
	}
	return id, nil
}

func (h *Handler) verifyGraphAuth(ctx context.Context, ws pgtype.UUID, req graphAuthRequest) error {
	if req.Branch == "" || len(req.Branch) > 200 || strings.HasPrefix(req.Branch, "-") || strings.ContainsAny(req.Branch, "\r\n*?[\\") {
		return errors.New("Enter a valid branch name")
	}
	auth, err := h.resolveGraphAuth(ctx, ws, req)
	if err != nil {
		return err
	}
	env, cleanup, _, err := graphGitAuthEnvironment(auth)
	if err != nil {
		return errors.New("Cannot prepare Git authentication")
	}
	defer cleanup()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-c", "credential.helper=", "-c", "http.extraHeader=", "-c", "http.followRedirects=false", "-c", "http.sslVerify=true", "ls-remote", "--exit-code", "--heads", "--", req.RepoURL, "refs/heads/"+req.Branch)
	cmd.Env = append(os.Environ(), env...)
	// Never return raw Git output: servers can echo credentials or arbitrary text.
	if _, err = processtree.CombinedOutput(ctx, cmd, 5*time.Second); err != nil {
		if ctx.Err() != nil {
			return errors.New("Git access check timed out; check network connectivity")
		}
		return errors.New("Cannot read repository and branch; check the Token, repository permissions and branch name")
	}
	return nil
}

func (h *Handler) VerifyCodeGraphGitAccess(w http.ResponseWriter, r *http.Request) {
	ws := workspaceIDFromURL(r, "id")
	if !h.requireCodeGraphWriter(w, r, ws) {
		return
	}
	var req graphAuthRequest
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&req) != nil {
		writeError(w, 400, "Invalid repository access request")
		return
	}
	if err := h.verifyGraphAuth(r.Context(), parseUUID(ws), req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"readable": true})
}

func (h *Handler) UpdateCodeGraphAuth(w http.ResponseWriter, r *http.Request) {
	ws := workspaceIDFromURL(r, "id")
	if !h.requireCodeGraphWriter(w, r, ws) {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "codeGraphId"), "graph id")
	if !ok {
		return
	}
	var req graphAuthRequest
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&req) != nil {
		writeError(w, 400, "Invalid repository authentication")
		return
	}
	if h.DB.QueryRow(r.Context(), `SELECT repo_url,branch FROM code_graph_repository WHERE id=$1 AND workspace_id=$2`, id, parseUUID(ws)).Scan(&req.RepoURL, &req.Branch) != nil {
		writeError(w, 404, "CodeGraph unavailable")
		return
	}
	if err := h.verifyGraphAuth(r.Context(), parseUUID(ws), req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "Cannot update Git authentication")
		return
	}
	defer tx.Rollback(r.Context())
	var cid any
	if req.Mode == "token" {
		parsed, _ := parseCredentialUUID(req.CredentialID)
		cid = parsed
		var locked string
		if tx.QueryRow(r.Context(), `SELECT id::text FROM code_graph_git_credential WHERE id=$1 AND workspace_id=$2 FOR UPDATE`, parsed, parseUUID(ws)).Scan(&locked) != nil {
			writeError(w, 409, "Git credential changed; try again")
			return
		}
	}
	if _, err = tx.Exec(r.Context(), `UPDATE code_graph_repository SET auth_mode=$3,credential_id=$4,updated_at=now() WHERE id=$1 AND workspace_id=$2`, id, parseUUID(ws), req.Mode, cid); err != nil {
		writeError(w, 500, "Cannot update Git authentication")
		return
	}
	if tx.Commit(r.Context()) != nil {
		writeError(w, 500, "Cannot update Git authentication")
		return
	}
	writeJSON(w, 200, map[string]bool{"saved": true})
}
