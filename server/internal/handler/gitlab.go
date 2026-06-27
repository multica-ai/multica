package handler

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ── Config helpers ───────────────────────────────────────────────────────────

func gitlabBaseURL() string { return strings.TrimRight(os.Getenv("GITLAB_URL"), "/") }
func gitlabAPIURL() string  { return gitlabBaseURL() + "/api/v4" }

func isGitLabConfigured() bool {
	return os.Getenv("GITLAB_URL") != "" &&
		os.Getenv("GITLAB_APP_ID") != "" &&
		os.Getenv("GITLAB_APP_SECRET") != "" &&
		os.Getenv("GITLAB_WEBHOOK_SECRET") != ""
}

func gitlabWebhookSecret() string { return strings.TrimSpace(os.Getenv("GITLAB_WEBHOOK_SECRET")) }

// signGitLabState and verifyGitLabState mirror the GitHub state-token pattern,
// using GITLAB_WEBHOOK_SECRET as the HMAC key.
func signGitLabState(workspaceID string) (string, error) {
	secret := gitlabWebhookSecret()
	if secret == "" {
		return "", errors.New("gitlab webhook secret not configured")
	}
	nonceBytes := make([]byte, 12)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", err
	}
	nonce := hex.EncodeToString(nonceBytes)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(workspaceID))
	mac.Write([]byte("."))
	mac.Write([]byte(nonce))
	sig := hex.EncodeToString(mac.Sum(nil))
	return workspaceID + "." + nonce + "." + sig, nil
}

func verifyGitLabState(token string) (string, bool) {
	secret := gitlabWebhookSecret()
	if secret == "" {
		return "", false
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", false
	}
	workspaceID, nonce, sig := parts[0], parts[1], parts[2]
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(workspaceID))
	mac.Write([]byte("."))
	mac.Write([]byte(nonce))
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(sig)) {
		return "", false
	}
	return workspaceID, true
}

// ── Response types ───────────────────────────────────────────────────────────

type GitLabConnectionResponse struct {
	ID            string  `json:"id"`
	WorkspaceID   string  `json:"workspace_id"`
	Namespace     string  `json:"namespace"`
	NamespaceType string  `json:"namespace_type"`
	AvatarURL     *string `json:"avatar_url"`
	CreatedAt     string  `json:"created_at"`
}

type GitLabMergeRequestResponse struct {
	ID              string  `json:"id"`
	WorkspaceID     string  `json:"workspace_id"`
	ProjectPath     string  `json:"project_path"`
	MRIID           int32   `json:"mr_iid"`
	Title           string  `json:"title"`
	State           string  `json:"state"`
	HtmlURL         string  `json:"html_url"`
	SourceBranch    *string `json:"source_branch"`
	AuthorUsername  *string `json:"author_username"`
	AuthorAvatarURL *string `json:"author_avatar_url"`
	MergedAt        *string `json:"merged_at"`
	ClosedAt        *string `json:"closed_at"`
	MRCreatedAt     string  `json:"mr_created_at"`
	MRUpdatedAt     string  `json:"mr_updated_at"`
}

type ListGitLabConnectionsResponse struct {
	Connections []GitLabConnectionResponse `json:"connections"`
	Configured  bool                       `json:"configured"`
	CanManage   bool                       `json:"can_manage"`
}

func gitlabConnectionToResponse(c db.GitlabConnection) GitLabConnectionResponse {
	return GitLabConnectionResponse{
		ID:            uuidToString(c.ID),
		WorkspaceID:   uuidToString(c.WorkspaceID),
		Namespace:     c.Namespace,
		NamespaceType: c.NamespaceType,
		AvatarURL:     textToPtr(c.AvatarUrl),
		CreatedAt:     timestampToString(c.CreatedAt),
	}
}

func gitlabMRToResponse(mr db.GitlabMergeRequest) GitLabMergeRequestResponse {
	return GitLabMergeRequestResponse{
		ID:              uuidToString(mr.ID),
		WorkspaceID:     uuidToString(mr.WorkspaceID),
		ProjectPath:     mr.ProjectPath,
		MRIID:           mr.MrIid,
		Title:           mr.Title,
		State:           mr.State,
		HtmlURL:         mr.HtmlUrl,
		SourceBranch:    textToPtr(mr.SourceBranch),
		AuthorUsername:  textToPtr(mr.AuthorUsername),
		AuthorAvatarURL: textToPtr(mr.AuthorAvatarUrl),
		MergedAt:        timestampToPtr(mr.MergedAt),
		ClosedAt:        timestampToPtr(mr.ClosedAt),
		MRCreatedAt:     timestampToString(mr.MrCreatedAt),
		MRUpdatedAt:     timestampToString(mr.MrUpdatedAt),
	}
}

// ── Webhook ──────────────────────────────────────────────────────────────────

// HandleGitLabWebhook (POST /api/webhooks/gitlab) verifies X-Gitlab-Token and
// routes Merge Request Hook events to handleGitLabMergeRequestEvent.
func (h *Handler) HandleGitLabWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "cannot read body")
		return
	}
	secret := gitlabWebhookSecret()
	if secret == "" {
		writeError(w, http.StatusServiceUnavailable, "gitlab integration not configured")
		return
	}
	if r.Header.Get("X-Gitlab-Token") != secret {
		writeError(w, http.StatusUnauthorized, "invalid webhook token")
		return
	}
	if r.Header.Get("X-Gitlab-Event") != "Merge Request Hook" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	h.handleGitLabMergeRequestEvent(r.Context(), body)
	w.WriteHeader(http.StatusNoContent)
}

// gitlabMRPayload is the subset of GitLab's merge_request webhook we consume.
type gitlabMRPayload struct {
	ObjectKind       string `json:"object_kind"`
	ObjectAttributes struct {
		IID          int32   `json:"iid"`
		Title        string  `json:"title"`
		Description  string  `json:"description"`
		State        string  `json:"state"`
		Action       string  `json:"action"`
		URL          string  `json:"url"`
		SourceBranch string  `json:"source_branch"`
		MergedAt     *string `json:"merged_at"`
		UpdatedAt    string  `json:"updated_at"`
		CreatedAt    string  `json:"created_at"`
	} `json:"object_attributes"`
	Project struct {
		PathWithNamespace string `json:"path_with_namespace"`
		Namespace         string `json:"namespace"`
	} `json:"project"`
	User struct {
		Username  string `json:"username"`
		AvatarURL string `json:"avatar_url"`
	} `json:"user"`
}

func (h *Handler) handleGitLabMergeRequestEvent(ctx context.Context, body []byte) {
	var p gitlabMRPayload
	if err := json.Unmarshal(body, &p); err != nil {
		slog.Error("gitlab: failed to parse MR payload", "err", err)
		return
	}

	namespace := p.Project.Namespace
	projectPath := p.Project.PathWithNamespace

	// Resolve workspace via connection namespace.
	conn, err := h.resolveGitLabConnectionByNamespace(ctx, namespace)
	if err != nil {
		slog.Warn("gitlab: no connection for namespace", "namespace", namespace)
		return
	}
	workspaceID := uuidToString(conn.WorkspaceID)

	// Parse timestamps.
	mrCreatedAt, err := time.Parse(time.RFC3339, p.ObjectAttributes.CreatedAt)
	if err != nil {
		mrCreatedAt = time.Now()
	}
	mrUpdatedAt, err := time.Parse(time.RFC3339, p.ObjectAttributes.UpdatedAt)
	if err != nil {
		mrUpdatedAt = time.Now()
	}

	// Normalize state: GitLab sends "opened" for open MRs.
	state := p.ObjectAttributes.State
	if state == "opened" {
		state = "open"
	}

	var mergedAt pgtype.Timestamptz
	if p.ObjectAttributes.MergedAt != nil && *p.ObjectAttributes.MergedAt != "" {
		if t, err := time.Parse(time.RFC3339, *p.ObjectAttributes.MergedAt); err == nil {
			mergedAt = pgtype.Timestamptz{Time: t, Valid: true}
		}
	}

	avatarURL := p.User.AvatarURL

	mr, err := h.Queries.UpsertGitLabMergeRequest(ctx, db.UpsertGitLabMergeRequestParams{
		WorkspaceID:     conn.WorkspaceID,
		ConnectionID:    conn.ID,
		ProjectPath:     projectPath,
		MrIid:           p.ObjectAttributes.IID,
		Title:           p.ObjectAttributes.Title,
		State:           state,
		HtmlUrl:         p.ObjectAttributes.URL,
		SourceBranch:    pgtype.Text{String: p.ObjectAttributes.SourceBranch, Valid: p.ObjectAttributes.SourceBranch != ""},
		AuthorUsername:  pgtype.Text{String: p.User.Username, Valid: p.User.Username != ""},
		AuthorAvatarUrl: pgtype.Text{String: avatarURL, Valid: avatarURL != ""},
		MergedAt:        mergedAt,
		ClosedAt:        pgtype.Timestamptz{},
		MrCreatedAt:     pgtype.Timestamptz{Time: mrCreatedAt, Valid: true},
		MrUpdatedAt:     pgtype.Timestamptz{Time: mrUpdatedAt, Valid: true},
	})
	if err != nil {
		slog.Error("gitlab: failed to upsert MR", "err", err, "project", projectPath, "iid", p.ObjectAttributes.IID)
		return
	}

	// Extract and link closing identifiers.
	closingIdents := map[string]struct{}{}
	for _, c := range extractClosingIdentifiers(p.ObjectAttributes.Title, p.ObjectAttributes.Description) {
		closingIdents[c] = struct{}{}
	}

	// Get workspace issue prefix for identifier lookup.
	prefix := h.getIssuePrefix(ctx, conn.WorkspaceID)

	for _, ident := range extractIdentifiers(p.ObjectAttributes.Title, p.ObjectAttributes.Description) {
		issue, found := h.lookupIssueByIdentifier(ctx, conn.WorkspaceID, prefix, ident)
		if !found {
			continue
		}
		_, hasCloseIntent := closingIdents[ident]
		if err := h.Queries.LinkIssueToMergeRequest(ctx, db.LinkIssueToMergeRequestParams{
			IssueID:        issue.ID,
			MergeRequestID: mr.ID,
			CloseIntent:    hasCloseIntent,
		}); err != nil {
			slog.Warn("gitlab: failed to link issue to MR", "issue", issue.ID, "mr", mr.ID, "err", err)
		}
	}

	// Auto-advance issues when MR merges with close intent and no open MRs remain.
	// Gate on action=="merge" to avoid re-running on subsequent update webhooks
	// for an already-merged MR.
	if state == "merged" && p.ObjectAttributes.Action == "merge" {
		h.maybeAdvanceIssuesOnGitLabMerge(ctx, mr, workspaceID)
	}

	// Publish realtime event.
	h.publish(protocol.EventGitLabMergeRequestUpdated, workspaceID, "system", "", map[string]any{
		"merge_request": gitlabMRToResponse(mr),
	})
}

func (h *Handler) maybeAdvanceIssuesOnGitLabMerge(ctx context.Context, mr db.GitlabMergeRequest, workspaceID string) {
	issueIDs, err := h.Queries.ListIssueIDsForMergeRequest(ctx, mr.ID)
	if err != nil {
		return
	}
	for _, issueID := range issueIDs {
		agg, err := h.Queries.GetIssueMergeRequestCloseAggregate(ctx, issueID)
		if err != nil {
			continue
		}
		if agg.OpenCount == 0 && agg.MergedWithCloseIntentCount > 0 {
			issue, err := h.Queries.GetIssue(ctx, issueID)
			if err != nil {
				continue
			}
			h.advanceIssueToDone(ctx, issue, workspaceID, "gitlab_mr_merged")
		}
	}
}

// resolveGitLabConnectionByNamespace finds the first workspace connection whose
// namespace matches the project's top-level group/user.
func (h *Handler) resolveGitLabConnectionByNamespace(ctx context.Context, namespace string) (db.GitlabConnection, error) {
	return h.Queries.GetGitLabConnectionByNamespaceGlobal(ctx, namespace)
}

// ── Workspace OAuth ──────────────────────────────────────────────────────────

// GitLabConnect (GET /api/workspaces/{id}/gitlab/connect) begins the OAuth flow.
func (h *Handler) GitLabConnect(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	if _, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id"); !ok {
		return
	}
	if !isGitLabConfigured() {
		writeJSON(w, http.StatusOK, map[string]bool{"configured": false})
		return
	}
	state, err := signGitLabState(workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to sign state")
		return
	}
	serverURL := strings.TrimRight(os.Getenv("MULTICA_PUBLIC_URL"), "/")
	if serverURL == "" {
		serverURL = strings.TrimRight(os.Getenv("FRONTEND_ORIGIN"), "/")
	}
	if serverURL == "" {
		serverURL = "http://localhost:3000"
	}
	callbackURL := serverURL + "/api/gitlab/setup"
	params := url.Values{
		"client_id":     {os.Getenv("GITLAB_APP_ID")},
		"redirect_uri":  {callbackURL},
		"response_type": {"code"},
		"scope":         {"api"},
		"state":         {state},
	}
	oauthURL := gitlabBaseURL() + "/oauth/authorize?" + params.Encode()
	http.Redirect(w, r, oauthURL, http.StatusFound)
}

// GitLabSetupCallback (GET /api/gitlab/setup) handles the OAuth redirect.
func (h *Handler) GitLabSetupCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	code := q.Get("code")
	state := q.Get("state")
	frontend := strings.TrimRight(os.Getenv("FRONTEND_ORIGIN"), "/")
	if frontend == "" {
		frontend = "http://localhost:3000"
	}
	settingsURL := frontend + "/settings?tab=gitlab"

	if code == "" || state == "" {
		http.Redirect(w, r, settingsURL+"&gitlab_error=missing_params", http.StatusFound)
		return
	}
	workspaceID, ok := verifyGitLabState(state)
	if !ok {
		http.Redirect(w, r, settingsURL+"&gitlab_error=invalid_state", http.StatusFound)
		return
	}
	wsUUID, err := parseStrictUUID(workspaceID)
	if err != nil {
		http.Redirect(w, r, settingsURL+"&gitlab_error=bad_workspace", http.StatusFound)
		return
	}

	token, expiresAt, err := gitlabExchangeCode(r.Context(), code)
	if err != nil {
		slog.Error("gitlab: token exchange failed", "err", err)
		http.Redirect(w, r, settingsURL+"&gitlab_error=token_exchange_failed", http.StatusFound)
		return
	}

	userInfo, err := gitlabFetchUser(r.Context(), token)
	if err != nil {
		slog.Error("gitlab: fetch user failed", "err", err)
		http.Redirect(w, r, settingsURL+"&gitlab_error=user_fetch_failed", http.StatusFound)
		return
	}

	// Encrypt token before storing.
	if h.GitLabBox == nil {
		http.Redirect(w, r, settingsURL+"&gitlab_error=not_configured", http.StatusFound)
		return
	}
	sealed, err := h.GitLabBox.Seal([]byte(token))
	if err != nil {
		http.Redirect(w, r, settingsURL+"&gitlab_error=encrypt_failed", http.StatusFound)
		return
	}

	// Best-effort capture of the connecting user (may be nil if the public
	// callback was hit without a session — X-User-ID is set by auth middleware
	// which is not applied to this public route). Either way we save the row
	// so the workspace owner sees the connection on next reload.
	connectedBy := pgtype.UUID{}
	if userID := requestUserID(r); userID != "" {
		if u, err := parseStrictUUID(userID); err == nil {
			connectedBy = u
		}
	}

	conn, err := h.Queries.CreateGitLabConnection(r.Context(), db.CreateGitLabConnectionParams{
		WorkspaceID:    wsUUID,
		Namespace:      userInfo.Namespace,
		NamespaceType:  userInfo.NamespaceType,
		AvatarUrl:      pgtype.Text{String: userInfo.AvatarURL, Valid: userInfo.AvatarURL != ""},
		AccessToken:    string(sealed),
		TokenExpiresAt: expiresAt,
		ConnectedByID:  connectedBy,
	})
	if err != nil {
		slog.Error("gitlab: failed to persist connection", "err", err)
		http.Redirect(w, r, settingsURL+"&gitlab_error=persist_failed", http.StatusFound)
		return
	}

	h.publish(protocol.EventGitLabConnectionCreated, workspaceID, "system", "", map[string]any{
		"connection": gitlabConnectionToResponse(conn),
	})
	http.Redirect(w, r, settingsURL+"&gitlab_connected=1", http.StatusFound)
}

// ListGitLabConnections (GET /api/workspaces/{id}/gitlab/connections)
func (h *Handler) ListGitLabConnections(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	member, _ := middleware.MemberFromContext(r.Context())
	canManage := roleAllowed(member.Role, "owner", "admin")

	conns, err := h.Queries.ListGitLabConnectionsByWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list connections")
		return
	}
	resp := make([]GitLabConnectionResponse, len(conns))
	for i, c := range conns {
		resp[i] = gitlabConnectionToResponse(c)
	}
	writeJSON(w, http.StatusOK, ListGitLabConnectionsResponse{
		Connections: resp,
		Configured:  isGitLabConfigured(),
		CanManage:   canManage,
	})
}

// DeleteGitLabConnection (DELETE /api/workspaces/{id}/gitlab/connections/{connectionId})
func (h *Handler) DeleteGitLabConnection(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	connectionID := chi.URLParam(r, "connectionId")
	connUUID, ok := parseUUIDOrBadRequest(w, connectionID, "connection id")
	if !ok {
		return
	}
	if err := h.Queries.DeleteGitLabConnection(r.Context(), db.DeleteGitLabConnectionParams{
		ID:          connUUID,
		WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete connection")
		return
	}
	h.publish(protocol.EventGitLabConnectionDeleted, workspaceID, "system", "", map[string]any{
		"connection_id": connectionID,
	})
	w.WriteHeader(http.StatusNoContent)
}

// ── GitLab API helpers ───────────────────────────────────────────────────────

type gitlabUserInfo struct {
	Namespace     string
	NamespaceType string
	AvatarURL     string
}

func gitlabExchangeCode(ctx context.Context, code string) (token string, expiresAt pgtype.Timestamptz, err error) {
	serverURL := strings.TrimRight(os.Getenv("MULTICA_PUBLIC_URL"), "/")
	if serverURL == "" {
		serverURL = strings.TrimRight(os.Getenv("FRONTEND_ORIGIN"), "/")
	}
	callbackURL := serverURL + "/api/gitlab/setup"

	resp, err := http.PostForm(gitlabBaseURL()+"/oauth/token", url.Values{
		"client_id":     {os.Getenv("GITLAB_APP_ID")},
		"client_secret": {os.Getenv("GITLAB_APP_SECRET")},
		"code":          {code},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {callbackURL},
	})
	if err != nil {
		return "", pgtype.Timestamptz{}, err
	}
	defer resp.Body.Close()

	var body struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", pgtype.Timestamptz{}, err
	}
	if body.AccessToken == "" {
		return "", pgtype.Timestamptz{}, errors.New("empty access_token in response")
	}
	exp := pgtype.Timestamptz{}
	if body.ExpiresIn > 0 {
		exp = pgtype.Timestamptz{Time: time.Now().Add(time.Duration(body.ExpiresIn) * time.Second), Valid: true}
	}
	return body.AccessToken, exp, nil
}

func gitlabFetchUser(ctx context.Context, token string) (gitlabUserInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, gitlabAPIURL()+"/user", nil)
	if err != nil {
		return gitlabUserInfo{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return gitlabUserInfo{}, err
	}
	defer resp.Body.Close()

	var body struct {
		Username  string `json:"username"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return gitlabUserInfo{}, err
	}
	return gitlabUserInfo{
		Namespace:     body.Username,
		NamespaceType: "user",
		AvatarURL:     body.AvatarURL,
	}, nil
}

// ListMergeRequestsForIssue (GET /api/issues/{id}/merge-requests)
func (h *Handler) ListMergeRequestsForIssue(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issueUUID, ok := parseUUIDOrBadRequest(w, issueID, "issue id")
	if !ok {
		return
	}
	mrs, err := h.Queries.ListMergeRequestsByIssue(r.Context(), issueUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list merge requests")
		return
	}
	resp := make([]GitLabMergeRequestResponse, len(mrs))
	for i, mr := range mrs {
		resp[i] = gitlabMRToResponse(mr)
	}
	writeJSON(w, http.StatusOK, map[string]any{"merge_requests": resp})
}
