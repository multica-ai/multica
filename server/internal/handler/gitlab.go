package handler

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/service"
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
		gitlabStateHMACKey() != ""
}

// gitlabLegacyWebhookSecret is the optional deploy-wide fallback used only for
// connections that predate per-connection secrets (empty webhook_secret column).
// New connections never rely on it; prefer rotating from the UI.
func gitlabLegacyWebhookSecret() string {
	return strings.TrimSpace(os.Getenv("GITLAB_WEBHOOK_SECRET"))
}

// gitlabStateHMACKey is the server-only key for OAuth CSRF state tokens.
// Prefer GITLAB_WEBHOOK_SECRET when set so existing deploys keep verifying
// in-flight OAuth state after upgrade. Fall back to GITLAB_SECRET_KEY for
// new deploys that no longer set the webhook env var.
func gitlabStateHMACKey() string {
	if k := gitlabLegacyWebhookSecret(); k != "" {
		return k
	}
	return strings.TrimSpace(os.Getenv("GITLAB_SECRET_KEY"))
}

const gitlabWebhookSecretPrefix = "glwh_"

// generateGitLabWebhookSecret returns a cryptographically random secret token
// for the GitLab "Secret token" webhook field. Format mirrors autopilot tokens:
// "glwh_" + URL-safe base64(32 bytes, no padding).
func generateGitLabWebhookSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("rand: %w", err)
	}
	return gitlabWebhookSecretPrefix + base64.RawURLEncoding.EncodeToString(b), nil
}

// gitlabWebhookTokenMatches reports whether the X-Gitlab-Token matches this
// connection. Prefer the per-connection secret; if it is empty (legacy row),
// accept the deploy-wide GITLAB_WEBHOOK_SECRET as a temporary fallback.
func gitlabWebhookTokenMatches(conn db.GitlabConnection, token string) bool {
	if conn.WebhookSecret != "" {
		return hmac.Equal([]byte(conn.WebhookSecret), []byte(token))
	}
	env := gitlabLegacyWebhookSecret()
	return env != "" && hmac.Equal([]byte(env), []byte(token))
}

// signGitLabState and verifyGitLabState mirror the GitHub state-token pattern.
// Token format: "{payload}|{namespace}.{nonce}.{sig}"
// namespace may be empty (login flow); payload is workspaceID or "login".
func signGitLabState(payload, namespace string) (string, error) {
	secret := gitlabStateHMACKey()
	if secret == "" {
		return "", errors.New("gitlab state hmac key not configured")
	}
	nonceBytes := make([]byte, 12)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", err
	}
	nonce := hex.EncodeToString(nonceBytes)
	combined := payload + "|" + namespace
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(combined))
	mac.Write([]byte("."))
	mac.Write([]byte(nonce))
	sig := hex.EncodeToString(mac.Sum(nil))
	return combined + "." + nonce + "." + sig, nil
}

func verifyGitLabState(token string) (payload, namespace string, ok bool) {
	secret := gitlabStateHMACKey()
	if secret == "" {
		return "", "", false
	}

	// Split from the right so payload/namespace can contain '.' safely.
	lastDot := strings.LastIndex(token, ".")
	if lastDot < 0 {
		return "", "", false
	}
	beforeSig, sig := token[:lastDot], token[lastDot+1:]
	secondDot := strings.LastIndex(beforeSig, ".")
	if secondDot < 0 {
		return "", "", false
	}
	combined, nonce := beforeSig[:secondDot], beforeSig[secondDot+1:]

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(combined))
	mac.Write([]byte("."))
	mac.Write([]byte(nonce))
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(sig)) {
		return "", "", false
	}
	p, ns, _ := strings.Cut(combined, "|")
	return p, ns, true
}

// ── Response types ───────────────────────────────────────────────────────────

type GitLabConnectionResponse struct {
	ID            string  `json:"id"`
	WorkspaceID   string  `json:"workspace_id"`
	Namespace     string  `json:"namespace"`
	NamespaceType string  `json:"namespace_type"`
	AvatarURL     *string `json:"avatar_url"`
	CreatedAt     string  `json:"created_at"`
	// WebhookSecret is the per-connection X-Gitlab-Token value. Only set for
	// owners/admins who need to paste it into GitLab; omitted for members.
	WebhookSecret *string `json:"webhook_secret,omitempty"`
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

type GitLabIssueResponse struct {
	GlIssueIID         int32   `json:"gl_issue_iid"`
	ProjectPath        string  `json:"project_path"`
	URL                string  `json:"url"`
	GlAssigneeUsername *string `json:"gl_assignee_username"`
}

func gitlabConnectionToResponse(c db.GitlabConnection, includeSecret bool) GitLabConnectionResponse {
	resp := GitLabConnectionResponse{
		ID:            uuidToString(c.ID),
		WorkspaceID:   uuidToString(c.WorkspaceID),
		Namespace:     c.Namespace,
		NamespaceType: c.NamespaceType,
		AvatarURL:     textToPtr(c.AvatarUrl),
		CreatedAt:     timestampToString(c.CreatedAt),
	}
	if includeSecret && c.WebhookSecret != "" {
		s := c.WebhookSecret
		resp.WebhookSecret = &s
	}
	return resp
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

// peekGitLabNamespace extracts the project namespace from a raw webhook body
// without fully parsing the event, used for the global namespace-based routing path.
func peekGitLabNamespace(body []byte) string {
	var peek struct {
		Project struct {
			Namespace string `json:"namespace"`
		} `json:"project"`
	}
	_ = json.Unmarshal(body, &peek)
	return peek.Project.Namespace
}

// readGitLabWebhookBody reads the request body with a size cap.
func readGitLabWebhookBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "cannot read body")
		return nil, false
	}
	return body, true
}

// HandleGitLabWebhook (POST /api/webhooks/gitlab) verifies X-Gitlab-Token against
// the resolved connection's per-connection secret (with a legacy env fallback)
// and routes events by matching the project namespace.
// Kept for backwards compatibility with already-registered project webhooks.
func (h *Handler) HandleGitLabWebhook(w http.ResponseWriter, r *http.Request) {
	body, ok := readGitLabWebhookBody(w, r)
	if !ok {
		return
	}
	token := r.Header.Get("X-Gitlab-Token")
	namespace := peekGitLabNamespace(body)
	conn, err := h.resolveGitLabConnectionByNamespace(r.Context(), namespace)
	if err != nil {
		// Unknown namespace: still reject bad tokens so probes cannot free-ride.
		// If a legacy deploy-wide secret is set and matches, return 204 (same
		// as the old global-secret path for unmatched namespaces).
		if env := gitlabLegacyWebhookSecret(); env == "" || !hmac.Equal([]byte(env), []byte(token)) {
			writeError(w, http.StatusUnauthorized, "invalid webhook token")
			return
		}
		slog.Warn("gitlab: no connection for namespace", "namespace", namespace)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !gitlabWebhookTokenMatches(conn, token) {
		writeError(w, http.StatusUnauthorized, "invalid webhook token")
		return
	}
	h.dispatchGitLabWebhookEvent(r.Context(), r.Header.Get("X-Gitlab-Event"), conn, body)
	w.WriteHeader(http.StatusNoContent)
}

// HandleGitLabWebhookForWorkspace (POST /api/webhooks/gitlab/{workspaceId}) verifies
// X-Gitlab-Token against a connection in that workspace and routes the event
// without a namespace lookup. Use this URL in GitLab project webhook settings so
// multiple projects across different workspaces each get their own endpoint.
func (h *Handler) HandleGitLabWebhookForWorkspace(w http.ResponseWriter, r *http.Request) {
	body, ok := readGitLabWebhookBody(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "workspaceId"), "workspace id")
	if !ok {
		return
	}
	token := r.Header.Get("X-Gitlab-Token")
	conns, err := h.Queries.ListGitLabConnectionsByWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve connection")
		return
	}
	if len(conns) == 0 {
		// No connection yet — reject unless legacy env secret still matches
		// (operator may have registered the webhook before connecting OAuth).
		if env := gitlabLegacyWebhookSecret(); env == "" || !hmac.Equal([]byte(env), []byte(token)) {
			writeError(w, http.StatusUnauthorized, "invalid webhook token")
			return
		}
		slog.Warn("gitlab: no connection for workspace",
			"workspace_id", uuidToString(wsUUID),
			"event", r.Header.Get("X-Gitlab-Event"),
		)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var conn *db.GitlabConnection
	for i := range conns {
		if gitlabWebhookTokenMatches(conns[i], token) {
			conn = &conns[i]
			break
		}
	}
	if conn == nil {
		writeError(w, http.StatusUnauthorized, "invalid webhook token")
		return
	}
	slog.Info("gitlab: webhook accepted",
		"workspace_id", uuidToString(wsUUID),
		"connection_id", uuidToString(conn.ID),
		"namespace", conn.Namespace,
		"event", r.Header.Get("X-Gitlab-Event"),
	)
	h.dispatchGitLabWebhookEvent(r.Context(), r.Header.Get("X-Gitlab-Event"), *conn, body)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) dispatchGitLabWebhookEvent(ctx context.Context, event string, conn db.GitlabConnection, body []byte) {
	switch event {
	case "Merge Request Hook":
		h.handleGitLabMergeRequestEvent(ctx, conn, body)
	case "Issue Hook":
		h.handleGitLabIssueEvent(ctx, conn, body)
	case "Note Hook":
		h.handleGitLabNoteEvent(ctx, conn, body)
	}
}

// gitlabIssuePayload is the subset of GitLab's Issue Hook webhook we consume.
type gitlabIssuePayload struct {
	ObjectKind       string `json:"object_kind"`
	ObjectAttributes struct {
		IID         int32  `json:"iid"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Action      string `json:"action"`
	} `json:"object_attributes"`
	Project  gitlabWebhookProject `json:"project"`
	Labels   []struct {
		Title string `json:"title"`
	} `json:"labels"`
	Assignees []struct {
		Username string `json:"username"`
	} `json:"assignees"`
}

// gitlabWebhookProject is the project object nested in GitLab webhooks.
// URL fields vary slightly by GitLab version; we accept the common aliases.
type gitlabWebhookProject struct {
	ID                int64  `json:"id"`
	PathWithNamespace string `json:"path_with_namespace"`
	Namespace         string `json:"namespace"`
	WebURL            string `json:"web_url"`
	GitHTTPURL        string `json:"git_http_url"`
	GitSSHURL         string `json:"git_ssh_url"`
	// Older / alternate field names GitLab has used on project objects.
	HTTPURL string `json:"http_url"`
	SSHURL  string `json:"ssh_url"`
	URL     string `json:"url"`
}

// gitlabNotePayload is the subset of GitLab's Note Hook webhook we consume.
type gitlabNotePayload struct {
	ObjectKind       string `json:"object_kind"`
	ObjectAttributes struct {
		NoteableType string `json:"noteable_type"`
		System       bool   `json:"system"`
		ID           int64  `json:"id"`
		Note         string `json:"note"`
	} `json:"object_attributes"`
	Project struct {
		PathWithNamespace string `json:"path_with_namespace"`
		Namespace         string `json:"namespace"`
	} `json:"project"`
	Issue struct {
		IID int32 `json:"iid"`
	} `json:"issue"`
	User gitlabNoteUser `json:"user"`
}

// gitlabNoteUser is the author identity on a GitLab Note Hook.
type gitlabNoteUser struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
}

func (h *Handler) handleGitLabNoteEvent(ctx context.Context, conn db.GitlabConnection, body []byte) {
	var p gitlabNotePayload
	if err := json.Unmarshal(body, &p); err != nil {
		slog.Error("gitlab: failed to parse note payload", "err", err)
		return
	}

	// Only handle issue comments; skip system notes.
	if p.ObjectAttributes.NoteableType != "Issue" || p.ObjectAttributes.System {
		return
	}
	// Skip notes Multica-mediated dual-write tools mark with the relay
	// sentinel (appended in code, never relying on model-authored text).
	if strings.Contains(p.ObjectAttributes.Note, gitlabNoteRelaySentinel) {
		return
	}

	projectPath := p.Project.PathWithNamespace

	if ws, err := h.Queries.GetWorkspace(ctx, conn.WorkspaceID); err == nil && !workspaceGitLabCommentSyncEnabled(ws.Settings) {
		return
	}

	// Find the linked gitlab_issue row.
	row, err := h.Queries.GetGitLabIssueByProjectAndIID(ctx, db.GetGitLabIssueByProjectAndIIDParams{
		WorkspaceID: conn.WorkspaceID,
		ProjectPath: projectPath,
		GlIssueIid:  p.Issue.IID,
	})
	if err != nil {
		// Issue not synced — skip.
		return
	}

	noteID := pgtype.Int8{Int64: p.ObjectAttributes.ID, Valid: true}

	// Echo prevention: skip if this note_id already exists.
	if _, err := h.Queries.GetCommentByGitLabNoteID(ctx, noteID); err == nil {
		return
	}

	issue, err := h.Queries.GetIssue(ctx, row.IssueID)
	if err != nil {
		slog.Warn("gitlab: issue not found for note", "issue_id", uuidToString(row.IssueID))
		return
	}

	// Dual-write: Multica comment first, then GitLab with the same body.
	// Link the note id onto the existing Multica row instead of duplicating.
	noteBody := strings.TrimSpace(p.ObjectAttributes.Note)
	if noteBody != "" {
		if existing, err := h.Queries.FindUnlinkedCommentByIssueAndContent(ctx, db.FindUnlinkedCommentByIssueAndContentParams{
			IssueID: issue.ID,
			Content: noteBody,
		}); err == nil {
			if err := h.Queries.SetCommentGitLabNoteID(ctx, db.SetCommentGitLabNoteIDParams{
				ID:           existing.ID,
				GitlabNoteID: noteID,
			}); err != nil {
				slog.Warn("gitlab: failed to link dual-write note id", "comment_id", uuidToString(existing.ID), "err", err)
			}
			return
		} else if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("gitlab: dual-write comment lookup failed", "err", err)
		}
	}

	// Prefer a persona agent matching the GitLab author (name + avatar) so the
	// existing agent comment UI framework renders them correctly. Fall back to
	// the connection owner with a text attribution prefix if we cannot mint one.
	authorType := "member"
	authorID, ok := h.gitlabCreatorID(ctx, conn)
	content := p.ObjectAttributes.Note
	if agent, created, agentOK := h.ensureGitLabUserAgent(ctx, conn, p.User); agentOK {
		authorType = "agent"
		authorID = agent.ID
		if created {
			h.publish(protocol.EventAgentCreated, uuidToString(conn.WorkspaceID), "system", "", map[string]any{
				"agent": broadcastAgentResponse(agentToResponse(agent)),
			})
		}
	} else {
		// Degraded path: no runtime / no owner — keep previous text attribution.
		if p.User.Username != "" {
			content = "**" + p.User.Username + "** (GitLab):\n" + p.ObjectAttributes.Note
		}
		if !ok {
			slog.Error("gitlab: no author available for note comment", "workspace", uuidToString(conn.WorkspaceID))
			return
		}
	}

	comment, err := h.Queries.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		AuthorType:  authorType,
		AuthorID:    authorID,
		Content:     content,
		Type:        "comment",
	})
	if err != nil {
		slog.Error("gitlab: failed to create comment from note", "err", err)
		return
	}

	// Store the note_id for echo loop prevention.
	if err := h.Queries.SetCommentGitLabNoteID(ctx, db.SetCommentGitLabNoteIDParams{
		ID:           comment.ID,
		GitlabNoteID: noteID,
	}); err != nil {
		slog.Warn("gitlab: failed to set gitlab_note_id on comment", "err", err)
	}

	h.publish(protocol.EventCommentCreated, uuidToString(issue.WorkspaceID), authorType, uuidToString(authorID), map[string]any{
		"comment": commentToResponse(comment, nil, nil),
	})
}

// ensureGitLabUserAgent finds or creates a workspace agent persona for a GitLab
// comment author so Multica can attribute the comment via author_type=agent
// (name + avatar resolved by the existing agent list / useActorName path).
// Returns the agent, whether it was newly created, and ok=false when no
// persona can be minted (missing identity, no owner, or no runtime).
func (h *Handler) ensureGitLabUserAgent(
	ctx context.Context,
	conn db.GitlabConnection,
	user gitlabNoteUser,
) (db.Agent, bool, bool) {
	systemKey := gitlabUserSystemKey(user.ID, user.Username)
	if systemKey == "" {
		return db.Agent{}, false, false
	}

	if existing, err := h.Queries.GetAgentBySystemKey(ctx, db.GetAgentBySystemKeyParams{
		WorkspaceID: conn.WorkspaceID,
		SystemKey:   pgtype.Text{String: systemKey, Valid: true},
	}); err == nil {
		updated := h.syncGitLabPersonaAgent(ctx, existing, user.Name, user.Username, user.AvatarURL)
		return updated, false, true
	} else if !errors.Is(err, pgx.ErrNoRows) {
		slog.Warn("gitlab: lookup persona agent failed", "system_key", systemKey, "err", err)
		return db.Agent{}, false, false
	}

	ownerID, ok := h.gitlabCreatorID(ctx, conn)
	if !ok {
		return db.Agent{}, false, false
	}

	runtimes, err := h.Queries.ListAgentRuntimes(ctx, conn.WorkspaceID)
	if err != nil || len(runtimes) == 0 {
		slog.Warn("gitlab: no runtime available for persona agent",
			"workspace", uuidToString(conn.WorkspaceID), "err", err)
		return db.Agent{}, false, false
	}
	runtime := runtimes[0]

	displayName := strings.TrimSpace(user.Name)
	if displayName == "" {
		displayName = strings.TrimSpace(user.Username)
	}
	if displayName == "" {
		displayName = "GitLab user"
	}

	// Prefer a clean display name; fall back to unique variants on name clash.
	candidates := []string{
		displayName,
		fmt.Sprintf("%s (@%s)", displayName, strings.TrimSpace(user.Username)),
		fmt.Sprintf("%s (GitLab)", displayName),
		fmt.Sprintf("GitLab %s", systemKey),
	}
	// Dedup empty / identical candidates.
	seen := map[string]struct{}{}
	uniqueNames := make([]string, 0, len(candidates))
	for _, n := range candidates {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		uniqueNames = append(uniqueNames, n)
	}

	desc := "GitLab identity for comment attribution"
	if u := strings.TrimSpace(user.Username); u != "" {
		desc = fmt.Sprintf("GitLab user @%s — mirrored for comment attribution", u)
	}
	avatar := pgtype.Text{String: strings.TrimSpace(user.AvatarURL), Valid: strings.TrimSpace(user.AvatarURL) != ""}

	var created db.Agent
	var createErr error
	for _, name := range uniqueNames {
		created, createErr = h.Queries.CreateGitLabPersonaAgent(ctx, db.CreateGitLabPersonaAgentParams{
			WorkspaceID: conn.WorkspaceID,
			Name:        name,
			Description: desc,
			AvatarUrl:   avatar,
			RuntimeMode: runtime.RuntimeMode,
			RuntimeID:   runtime.ID,
			OwnerID:     ownerID,
			SystemKey:   pgtype.Text{String: systemKey, Valid: true},
		})
		if createErr == nil {
			break
		}
		var pgErr *pgconn.PgError
		if errors.As(createErr, &pgErr) && pgErr.Code == "23505" {
			// Name clash or concurrent system_key insert — if the persona now
			// exists under our system_key, reuse it; otherwise try next name.
			if existing, err := h.Queries.GetAgentBySystemKey(ctx, db.GetAgentBySystemKeyParams{
				WorkspaceID: conn.WorkspaceID,
				SystemKey:   pgtype.Text{String: systemKey, Valid: true},
			}); err == nil {
				updated := h.syncGitLabPersonaAgent(ctx, existing, user.Name, user.Username, user.AvatarURL)
				return updated, false, true
			}
			continue
		}
		slog.Error("gitlab: failed to create persona agent", "name", name, "err", createErr)
		return db.Agent{}, false, false
	}
	if createErr != nil {
		slog.Error("gitlab: exhausted persona agent name candidates", "system_key", systemKey, "err", createErr)
		return db.Agent{}, false, false
	}

	// Workspace-wide public_to target so every member can resolve the persona
	// in ListAgents / useActorName (avatar + display name on comments).
	if err := h.Queries.CreateAgentInvocationTarget(ctx, db.CreateAgentInvocationTargetParams{
		AgentID:    created.ID,
		TargetType: invocationTargetWorkspace,
		TargetID:   conn.WorkspaceID,
		CreatedBy:  ownerID,
	}); err != nil {
		slog.Warn("gitlab: failed to set persona agent workspace target", "agent_id", uuidToString(created.ID), "err", err)
	}

	return created, true, true
}

// gitlabUserSystemKey builds a stable system_key for a GitLab author. Prefer
// numeric user id; fall back to username when id is missing from the payload.
func gitlabUserSystemKey(id int64, username string) string {
	if id > 0 {
		return fmt.Sprintf("gitlab:%d", id)
	}
	username = strings.TrimSpace(username)
	if username == "" {
		return ""
	}
	return "gitlab:u:" + strings.ToLower(username)
}

// syncGitLabPersonaAgent refreshes name/avatar when the GitLab profile changes.
// Name updates are best-effort: conflicts keep the existing unique name.
func (h *Handler) syncGitLabPersonaAgent(ctx context.Context, agent db.Agent, name, username, avatarURL string) db.Agent {
	displayName := strings.TrimSpace(name)
	if displayName == "" {
		displayName = strings.TrimSpace(username)
	}
	params := db.UpdateAgentParams{ID: agent.ID}
	changed := false
	if displayName != "" && displayName != agent.Name {
		params.Name = pgtype.Text{String: displayName, Valid: true}
		changed = true
	}
	avatarURL = strings.TrimSpace(avatarURL)
	if avatarURL != "" && (!agent.AvatarUrl.Valid || agent.AvatarUrl.String != avatarURL) {
		params.AvatarUrl = pgtype.Text{String: avatarURL, Valid: true}
		changed = true
	}
	if !changed {
		return agent
	}
	updated, err := h.Queries.UpdateAgent(ctx, params)
	if err != nil {
		// Name uniqueness clash or transient error — keep the existing persona.
		slog.Debug("gitlab: persona agent sync skipped", "agent_id", uuidToString(agent.ID), "err", err)
		return agent
	}
	return updated
}

// refreshGitLabToken exchanges the stored refresh token for a new access token,
// encrypts and persists both, and returns the plain new access token.
func (h *Handler) refreshGitLabToken(ctx context.Context, conn db.GitlabConnection) (string, error) {
	if !conn.RefreshToken.Valid || conn.RefreshToken.String == "" {
		return "", errors.New("no refresh token stored")
	}
	resp, err := http.PostForm(gitlabBaseURL()+"/oauth/token", url.Values{
		"client_id":     {os.Getenv("GITLAB_APP_ID")},
		"client_secret": {os.Getenv("GITLAB_APP_SECRET")},
		"refresh_token": {conn.RefreshToken.String},
		"grant_type":    {"refresh_token"},
	})
	if err != nil {
		return "", fmt.Errorf("refresh request failed: %w", err)
	}
	defer resp.Body.Close()
	tok, expiresAt, err := parseGitLabTokenResponse(resp.Body)
	if err != nil {
		return "", fmt.Errorf("refresh response parse: %w", err)
	}
	sealed, err := h.GitLabBox.Seal([]byte(tok.AccessToken))
	if err != nil {
		return "", fmt.Errorf("encrypt refreshed token: %w", err)
	}
	if dbErr := h.Queries.UpdateGitLabConnectionTokens(ctx, db.UpdateGitLabConnectionTokensParams{
		ID:             conn.ID,
		AccessToken:    base64.StdEncoding.EncodeToString(sealed),
		RefreshToken:   pgtype.Text{String: tok.RefreshToken, Valid: tok.RefreshToken != ""},
		TokenExpiresAt: expiresAt,
	}); dbErr != nil {
		slog.Warn("gitlab: failed to persist refreshed token", "err", dbErr)
	}
	return tok.AccessToken, nil
}

// gitlabNoteRelaySentinel marks a GitLab note as Multica-originated dual-write
// so the Note Hook does not re-import it. Appended only by Multica code paths
// that post to GitLab (not free-form model/skill text). Multica no longer
// auto-relays Multica comments to GitLab; agents post with their own tokens
// and should use AppendGitLabNoteRelaySentinel when they dual-write.
const gitlabNoteRelaySentinel = "<!-- multica:gitlab-relay -->"

// AppendGitLabNoteRelaySentinel appends the echo-prevention sentinel to a
// GitLab note body. Call this in any Multica-controlled code that posts a
// note already recorded (or about to be recorded) as a Multica comment.
func AppendGitLabNoteRelaySentinel(body string) string {
	body = strings.TrimRight(body, "\n")
	if strings.Contains(body, gitlabNoteRelaySentinel) {
		return body
	}
	return body + "\n\n" + gitlabNoteRelaySentinel
}

// containsLabel reports whether the labels slice contains a label with the given title.
func containsLabel(labels []struct {
	Title string `json:"title"`
}, title string) bool {
	for _, l := range labels {
		if l.Title == title {
			return true
		}
	}
	return false
}

// hasGitLabIssueSyncTrigger reports whether the issue should be imported into
// Multica. True when the configured sync label is present exactly, or when any
// label uses the "{syncLabel}::{agentName}" form (e.g. "agent::Implementer"
// with sync label "agent"). A prefixed label alone is enough to create the
// Multica issue; agent assignment is best-effort and may leave it unassigned.
func hasGitLabIssueSyncTrigger(labels []struct {
	Title string `json:"title"`
}, syncLabel string) bool {
	if syncLabel == "" {
		return false
	}
	if containsLabel(labels, syncLabel) {
		return true
	}
	prefix := syncLabel + "::"
	for _, l := range labels {
		title := strings.TrimSpace(l.Title)
		if strings.HasPrefix(title, prefix) && len(title) > len(prefix) {
			return true
		}
	}
	return false
}

// gitlabLabelAgentNameCandidates returns possible Multica agent names encoded
// in GitLab labels. Each label title is a candidate as-is; if it uses the
// "{syncLabel}::{name}" form, the suffix is also a candidate (so "agent::Coder"
// resolves against an agent named "Coder" when the sync label is "agent").
func gitlabLabelAgentNameCandidates(labels []struct {
	Title string `json:"title"`
}, syncLabel string) []string {
	prefix := ""
	if syncLabel != "" {
		prefix = syncLabel + "::"
	}
	var out []string
	seen := map[string]struct{}{}
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		key := strings.ToLower(s)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, s)
	}
	for _, l := range labels {
		title := strings.TrimSpace(l.Title)
		if title == "" {
			continue
		}
		add(title)
		if prefix != "" && strings.HasPrefix(title, prefix) {
			add(strings.TrimPrefix(title, prefix))
		}
	}
	return out
}

// matchAgentByGitLabLabels picks a workspace user-agent whose name uniquely
// matches a GitLab label (case-insensitive). Returns ok=false when zero or
// multiple distinct agents match — create still proceeds unassigned.
func matchAgentByGitLabLabels(agents []db.Agent, labels []struct {
	Title string `json:"title"`
}, syncLabel string) (db.Agent, bool) {
	candidates := gitlabLabelAgentNameCandidates(labels, syncLabel)
	if len(candidates) == 0 {
		return db.Agent{}, false
	}

	// kind=user only — skip GitLab persona / system agents.
	byLower := make(map[string][]db.Agent)
	for _, a := range agents {
		if a.Kind != "" && a.Kind != "user" {
			continue
		}
		if a.ArchivedAt.Valid {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(a.Name))
		if key == "" {
			continue
		}
		byLower[key] = append(byLower[key], a)
	}

	matched := make(map[string]db.Agent) // agent id → agent
	for _, cand := range candidates {
		group := byLower[strings.ToLower(cand)]
		if len(group) == 0 {
			continue
		}
		// Prefer exact-case match when multiple agents only differ by case.
		var pick db.Agent
		if len(group) == 1 {
			pick = group[0]
		} else {
			exact := false
			for _, a := range group {
				if a.Name == cand {
					pick = a
					exact = true
					break
				}
			}
			if !exact {
				// Ambiguous case-only collision for this candidate — skip it.
				continue
			}
		}
		matched[uuidToString(pick.ID)] = pick
	}

	if len(matched) != 1 {
		return db.Agent{}, false
	}
	for _, a := range matched {
		return a, true
	}
	return db.Agent{}, false
}

func (h *Handler) handleGitLabIssueEvent(ctx context.Context, conn db.GitlabConnection, body []byte) {
	var p gitlabIssuePayload
	if err := json.Unmarshal(body, &p); err != nil {
		slog.Error("gitlab: failed to parse issue payload", "err", err)
		return
	}

	projectPath := p.Project.PathWithNamespace
	action := p.ObjectAttributes.Action
	workspaceID := uuidToString(conn.WorkspaceID)
	glIID := p.ObjectAttributes.IID

	syncLabel := defaultGitLabIssueSyncLabel
	if ws, err := h.Queries.GetWorkspace(ctx, conn.WorkspaceID); err == nil {
		syncLabel = workspaceGitLabIssueSyncLabel(ws.Settings)
	}
	hasSyncLabel := hasGitLabIssueSyncTrigger(p.Labels, syncLabel)

	assigneeUsername := ""
	if len(p.Assignees) > 0 {
		assigneeUsername = p.Assignees[0].Username
	}

	// Look up existing gitlab_issue row.
	row, rowErr := h.Queries.GetGitLabIssueByProjectAndIID(ctx, db.GetGitLabIssueByProjectAndIIDParams{
		WorkspaceID: conn.WorkspaceID,
		ProjectPath: projectPath,
		GlIssueIid:  glIID,
	})
	rowExists := rowErr == nil

	slog.Info("gitlab: issue hook",
		"workspace", workspaceID,
		"project", projectPath,
		"gl_iid", glIID,
		"action", action,
		"sync_label", syncLabel,
		"has_sync_label", hasSyncLabel,
		"already_linked", rowExists,
		"title", p.ObjectAttributes.Title,
		"label_count", len(p.Labels),
	)

	// Creating and field-syncing Multica issues requires the configured sync
	// trigger: the bare label (default "agent") or "{label}::{agentName}".
	// Removing the trigger does not delete the Multica issue — the link stays
	// and close/reopen status transitions still apply when a row exists.
	if hasSyncLabel {
		if !rowExists && (action == "open" || action == "update") {
			// Create Multica issue.
			creatorID, ok := h.gitlabCreatorID(ctx, conn)
			if !ok {
				slog.Error("gitlab: no creator available, skipping issue creation",
					"workspace", workspaceID, "project", projectPath, "gl_iid", glIID)
				return
			}

			// Prefer the Multica project whose github_repo resource matches this
			// GitLab repo (one-repo-per-project). Unmatched webhooks still create
			// a workspace-level issue with no project.
			projectID := h.resolveMulticaProjectForGitLabRepo(ctx, conn.WorkspaceID, p.Project)

			// Assign a Multica agent when a GitLab label uniquely matches an
			// agent name (exact title or "{syncLabel}::{name}"). Ambiguous or
			// missing matches leave the issue unassigned.
			createParams := service.IssueCreateParams{
				WorkspaceID:    conn.WorkspaceID,
				Title:          p.ObjectAttributes.Title,
				Description:    pgtype.Text{String: p.ObjectAttributes.Description, Valid: p.ObjectAttributes.Description != ""},
				Status:         "todo",
				Priority:       "none",
				CreatorType:    "member",
				CreatorID:      creatorID,
				ProjectID:      projectID,
				AllowDuplicate: true,
			}
			var assignedAgentID string
			if agents, err := h.Queries.ListAgents(ctx, conn.WorkspaceID); err != nil {
				slog.Warn("gitlab: list agents for label assign failed",
					"workspace", workspaceID, "err", err)
			} else if agent, ok := matchAgentByGitLabLabels(agents, p.Labels, syncLabel); ok {
				createParams.AssigneeType = pgtype.Text{String: "agent", Valid: true}
				createParams.AssigneeID = agent.ID
				assignedAgentID = uuidToString(agent.ID)
			}

			res, err := h.IssueService.Create(ctx, createParams, service.IssueCreateOpts{
				AnalyticsAgentID: assignedAgentID,
			})
			if err != nil {
				slog.Error("gitlab: failed to create issue",
					"err", err, "workspace", workspaceID, "project", projectPath, "gl_iid", glIID)
				return
			}

			glRow, err := h.Queries.InsertGitLabIssue(ctx, db.InsertGitLabIssueParams{
				WorkspaceID:        conn.WorkspaceID,
				ConnectionID:       conn.ID,
				ProjectPath:        projectPath,
				GlIssueIid:         glIID,
				GlProjectID:        p.Project.ID,
				IssueID:            res.Issue.ID,
				GlAssigneeUsername: pgtype.Text{String: assigneeUsername, Valid: assigneeUsername != ""},
			})
			if err != nil {
				slog.Error("gitlab: failed to insert gitlab_issue row",
					"err", err,
					"workspace", workspaceID,
					"project", projectPath,
					"gl_iid", glIID,
					"issue_id", uuidToString(res.Issue.ID),
				)
				return
			}
			row = glRow
			rowExists = true
			slog.Info("gitlab: created multica issue from sync label",
				"workspace", workspaceID,
				"project", projectPath,
				"gl_iid", glIID,
				"issue_id", uuidToString(res.Issue.ID),
				"issue_number", res.Issue.Number,
				"multica_project_id", uuidToPtr(projectID),
				"title", p.ObjectAttributes.Title,
				"sync_label", syncLabel,
				"assignee_agent_id", assignedAgentID,
			)

		} else if rowExists {
			// Sync description.
			if err := h.Queries.UpdateIssueDescription(ctx, db.UpdateIssueDescriptionParams{
				ID:          row.IssueID,
				Description: pgtype.Text{String: p.ObjectAttributes.Description, Valid: p.ObjectAttributes.Description != ""},
			}); err != nil {
				slog.Warn("gitlab: failed to sync description", "err", err, "issue_id", uuidToString(row.IssueID))
			}
			// Sync assignee.
			if err := h.Queries.UpdateGitLabIssueAssignee(ctx, db.UpdateGitLabIssueAssigneeParams{
				ID:                 row.ID,
				GlAssigneeUsername: pgtype.Text{String: assigneeUsername, Valid: assigneeUsername != ""},
			}); err != nil {
				slog.Warn("gitlab: failed to sync assignee", "err", err, "issue_id", uuidToString(row.IssueID))
			}
			slog.Info("gitlab: synced existing linked issue",
				"workspace", workspaceID,
				"project", projectPath,
				"gl_iid", glIID,
				"issue_id", uuidToString(row.IssueID),
				"action", action,
			)
		} else {
			slog.Info("gitlab: sync label present but action does not create",
				"workspace", workspaceID,
				"project", projectPath,
				"gl_iid", glIID,
				"action", action,
				"sync_label", syncLabel,
			)
		}
	} else if !rowExists {
		slog.Info("gitlab: issue hook ignored (no sync label, not linked)",
			"workspace", workspaceID,
			"project", projectPath,
			"gl_iid", glIID,
			"action", action,
			"sync_label", syncLabel,
		)
	}

	// Status transitions — applied after the create/sync block.
	if rowExists {
		issue, err := h.Queries.GetIssue(ctx, row.IssueID)
		if err != nil {
			slog.Warn("gitlab: issue not found for status transition", "issue_id", uuidToString(row.IssueID))
			return
		}
		switch action {
		case "close":
			h.advanceIssueToDone(ctx, issue, workspaceID, "gitlab_issue_closed")
		case "reopen":
			updated, err := h.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
				ID:          issue.ID,
				Status:      "in_progress",
				WorkspaceID: issue.WorkspaceID,
			})
			if err != nil {
				slog.Warn("gitlab: failed to reopen issue", "err", err)
				return
			}
			prefix := h.getIssuePrefix(ctx, issue.WorkspaceID)
			h.publish(protocol.EventIssueUpdated, workspaceID, "system", "", map[string]any{
				"issue":          issueToResponse(updated, prefix),
				"status_changed": true,
				"prev_status":    issue.Status,
				"source":         "gitlab_issue_reopened",
			})
		}
	}
}

// gitlabCreatorID returns the user UUID to use as creator for webhook-triggered
// issue creation. Prefers the connection's connected_by_id; falls back to the
// first workspace member.
func (h *Handler) gitlabCreatorID(ctx context.Context, conn db.GitlabConnection) (pgtype.UUID, bool) {
	if conn.ConnectedByID.Valid {
		return conn.ConnectedByID, true
	}
	members, err := h.Queries.ListMembers(ctx, conn.WorkspaceID)
	if err != nil || len(members) == 0 {
		return pgtype.UUID{}, false
	}
	return members[0].UserID, true
}

// resolveMulticaProjectForGitLabRepo finds a Multica project in the workspace
// whose attached github_repo resource matches the GitLab project from a
// webhook. Matching is by repo identity (host+path, ignoring scheme/.git) and
// falls back to path_with_namespace alone so operators who paste either the
// HTTPS or SSH clone URL still resolve correctly.
//
// Returns an invalid UUID when no project matches (issue is created at
// workspace scope). When multiple projects match, the earliest-created
// resource wins and a warning is logged — the product assumes one repo per
// Multica project.
func (h *Handler) resolveMulticaProjectForGitLabRepo(ctx context.Context, workspaceID pgtype.UUID, glProject gitlabWebhookProject) pgtype.UUID {
	resources, err := h.Queries.ListGithubRepoProjectResourcesByWorkspace(ctx, workspaceID)
	if err != nil {
		slog.Warn("gitlab: failed to list project resources for repo match",
			"err", err, "workspace", uuidToString(workspaceID))
		return pgtype.UUID{}
	}
	if len(resources) == 0 {
		return pgtype.UUID{}
	}

	candidates := gitlabProjectCandidateURLs(glProject)
	pathKey := normalizeGitRepoPath(glProject.PathWithNamespace)

	var matched pgtype.UUID
	var matchedURL string
	for _, res := range resources {
		repoURL, ok := githubRepoURLFromResourceRef(res.ResourceRef)
		if !ok {
			continue
		}
		if !gitRepoMatchesGitLabProject(repoURL, pathKey, candidates) {
			continue
		}
		if matched.Valid {
			slog.Warn("gitlab: multiple multica projects match gitlab repo; using first",
				"workspace", uuidToString(workspaceID),
				"path_with_namespace", glProject.PathWithNamespace,
				"chosen_project_id", uuidToString(matched),
				"also_matched_project_id", uuidToString(res.ProjectID),
				"resource_url", repoURL,
			)
			continue
		}
		matched = res.ProjectID
		matchedURL = repoURL
	}
	if matched.Valid {
		slog.Info("gitlab: resolved multica project from repo resource",
			"workspace", uuidToString(workspaceID),
			"path_with_namespace", glProject.PathWithNamespace,
			"multica_project_id", uuidToString(matched),
			"resource_url", matchedURL,
		)
	}
	return matched
}

// githubRepoURLFromResourceRef extracts resource_ref.url for a github_repo row.
func githubRepoURLFromResourceRef(ref []byte) (string, bool) {
	var payload struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(ref, &payload); err != nil {
		return "", false
	}
	url := strings.TrimSpace(payload.URL)
	if url == "" {
		return "", false
	}
	return url, true
}

// gitlabProjectCandidateURLs collects every clone/web URL present on the
// webhook project object, plus a synthetic URL from GITLAB_URL + path when
// the instance base URL is configured.
func gitlabProjectCandidateURLs(p gitlabWebhookProject) []string {
	raw := []string{
		p.GitHTTPURL,
		p.GitSSHURL,
		p.WebURL,
		p.HTTPURL,
		p.SSHURL,
		p.URL,
	}
	if base := gitlabBaseURL(); base != "" && p.PathWithNamespace != "" {
		raw = append(raw, strings.TrimRight(base, "/")+"/"+strings.Trim(p.PathWithNamespace, "/"))
		raw = append(raw, strings.TrimRight(base, "/")+"/"+strings.Trim(p.PathWithNamespace, "/")+".git")
	}
	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, s := range raw {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// gitRepoMatchesGitLabProject reports whether a Multica project resource URL
// points at the same GitLab repository described by pathKey (normalized
// path_with_namespace) and/or the candidate clone/web URLs from the webhook.
func gitRepoMatchesGitLabProject(resourceURL, pathKey string, candidateURLs []string) bool {
	rHost, rPath := splitGitRemote(resourceURL)
	if rPath == "" {
		return false
	}
	for _, c := range candidateURLs {
		cHost, cPath := splitGitRemote(c)
		if cPath == "" {
			continue
		}
		if rPath != cPath {
			continue
		}
		// Same path: require host agreement when both sides have one.
		if cHost != "" && rHost != "" && cHost != rHost {
			continue
		}
		return true
	}
	// Path-only fallback when the webhook only carried path_with_namespace
	// (or URLs failed to parse). Safe under the one-repo-per-project model.
	return pathKey != "" && rPath == pathKey
}

// splitGitRemote extracts a comparable host + path from a git remote URL.
// Supports https://, http://, ssh://, git://, and scp-like git@host:path forms.
// Path is lowercased and stripped of a trailing ".git". Host is lowercased and
// port-stripped (Hostname()) so https://host/path matches git@host:path.
func splitGitRemote(raw string) (host, path string) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimRight(raw, "/")
	if raw == "" {
		return "", ""
	}

	if u, err := url.Parse(raw); err == nil && u.Scheme != "" && u.Host != "" {
		host = strings.ToLower(u.Hostname())
		path = normalizeGitRepoPath(u.Path)
		return host, path
	}

	// scp-like: [user@]host:path
	s := raw
	if i := strings.Index(s, "@"); i >= 0 && !strings.Contains(s[:i], "://") {
		s = s[i+1:]
	}
	if i := strings.Index(s, ":"); i > 0 {
		host = strings.ToLower(s[:i])
		// host may include a non-standard port written as host:port/path after
		// scp conversion failures — keep only hostname before any residual '/'.
		if j := strings.Index(host, "/"); j >= 0 {
			host = host[:j]
		}
		path = normalizeGitRepoPath(s[i+1:])
		return host, path
	}

	// Bare path (e.g. path_with_namespace alone).
	return "", normalizeGitRepoPath(raw)
}

// normalizeGitRepoPath lowercases a repo path, trims slashes, and drops a
// trailing ".git" suffix.
func normalizeGitRepoPath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.Trim(p, "/")
	p = strings.TrimSuffix(p, ".git")
	p = strings.Trim(p, "/")
	return strings.ToLower(p)
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

func (h *Handler) handleGitLabMergeRequestEvent(ctx context.Context, conn db.GitlabConnection, body []byte) {
	var p gitlabMRPayload
	if err := json.Unmarshal(body, &p); err != nil {
		slog.Error("gitlab: failed to parse MR payload", "err", err)
		return
	}

	projectPath := p.Project.PathWithNamespace
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

// workspaceGitLabCommentSyncEnabled returns true (GitLab→Multica comment import
// on) unless the workspace has explicitly set gitlab_comment_sync_enabled=false
// in its settings JSON. Multica never auto-posts Multica comments to GitLab.
func workspaceGitLabCommentSyncEnabled(settings []byte) bool {
	if len(settings) == 0 {
		return true
	}
	var s struct {
		CommentSync *bool `json:"gitlab_comment_sync_enabled"`
	}
	if err := json.Unmarshal(settings, &s); err != nil || s.CommentSync == nil {
		return true
	}
	return *s.CommentSync
}

// defaultGitLabIssueSyncLabel is used when workspace settings omit or blank
// gitlab_issue_sync_label. Historical installs always matched the "agent" label.
const defaultGitLabIssueSyncLabel = "agent"

// workspaceGitLabIssueSyncLabel returns the GitLab label title that triggers
// Multica issue creation. Defaults to "agent" when unset or empty.
func workspaceGitLabIssueSyncLabel(settings []byte) string {
	if len(settings) == 0 {
		return defaultGitLabIssueSyncLabel
	}
	var s struct {
		Label *string `json:"gitlab_issue_sync_label"`
	}
	if err := json.Unmarshal(settings, &s); err != nil || s.Label == nil {
		return defaultGitLabIssueSyncLabel
	}
	label := strings.TrimSpace(*s.Label)
	if label == "" {
		return defaultGitLabIssueSyncLabel
	}
	return label
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
	namespace := strings.TrimSpace(r.URL.Query().Get("ns"))
	state, err := signGitLabState(workspaceID, namespace)
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
	workspaceID, nsFromState, ok := verifyGitLabState(state)
	if !ok {
		http.Redirect(w, r, settingsURL+"&gitlab_error=invalid_state", http.StatusFound)
		return
	}
	wsUUID, err := parseStrictUUID(workspaceID)
	if err != nil {
		http.Redirect(w, r, settingsURL+"&gitlab_error=bad_workspace", http.StatusFound)
		return
	}

	tokenResp, expiresAt, err := gitlabExchangeCode(r.Context(), code)
	if err != nil {
		slog.Error("gitlab: token exchange failed", "err", err)
		http.Redirect(w, r, settingsURL+"&gitlab_error=token_exchange_failed", http.StatusFound)
		return
	}

	userInfo, err := gitlabFetchUser(r.Context(), tokenResp.AccessToken)
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
	sealed, err := h.GitLabBox.Seal([]byte(tokenResp.AccessToken))
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

	resolvedNamespace := nsFromState
	resolvedType := "group"
	if resolvedNamespace == "" {
		resolvedNamespace = userInfo.Namespace
		resolvedType = userInfo.NamespaceType
	}
	webhookSecret, err := generateGitLabWebhookSecret()
	if err != nil {
		slog.Error("gitlab: failed to generate webhook secret", "err", err)
		http.Redirect(w, r, settingsURL+"&gitlab_error=persist_failed", http.StatusFound)
		return
	}

	conn, err := h.Queries.CreateGitLabConnection(r.Context(), db.CreateGitLabConnectionParams{
		WorkspaceID:    wsUUID,
		Namespace:      resolvedNamespace,
		NamespaceType:  resolvedType,
		AvatarUrl:      pgtype.Text{String: userInfo.AvatarURL, Valid: resolvedType == "user" && userInfo.AvatarURL != ""},
		AccessToken:    base64.StdEncoding.EncodeToString(sealed),
		RefreshToken:   pgtype.Text{String: tokenResp.RefreshToken, Valid: tokenResp.RefreshToken != ""},
		TokenExpiresAt: expiresAt,
		ConnectedByID:  connectedBy,
		WebhookSecret:  webhookSecret,
	})
	if err != nil {
		slog.Error("gitlab: failed to persist connection", "err", err)
		http.Redirect(w, r, settingsURL+"&gitlab_error=persist_failed", http.StatusFound)
		return
	}

	h.publish(protocol.EventGitLabConnectionCreated, workspaceID, "system", "", map[string]any{
		// Never broadcast the webhook secret over the WS fanout.
		"connection": gitlabConnectionToResponse(conn, false),
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
	// Lazy-issue secrets for legacy rows so admins can copy a token without
	// re-connecting OAuth. Only managers trigger generation (avoids racing
	// writes from every member list poll). Prefer seeding from the legacy env
	// secret when set so existing GitLab webhooks keep working after upgrade;
	// otherwise mint a fresh token.
	if canManage {
		for i := range conns {
			if conns[i].WebhookSecret != "" {
				continue
			}
			secret := gitlabLegacyWebhookSecret()
			if secret == "" {
				var gerr error
				secret, gerr = generateGitLabWebhookSecret()
				if gerr != nil {
					slog.Error("gitlab: failed to generate webhook secret for legacy connection",
						"connection_id", uuidToString(conns[i].ID), "err", gerr)
					continue
				}
			}
			updated, serr := h.Queries.SetGitLabConnectionWebhookSecret(r.Context(), db.SetGitLabConnectionWebhookSecretParams{
				ID:            conns[i].ID,
				WebhookSecret: secret,
				WorkspaceID:   wsUUID,
			})
			if serr != nil {
				slog.Error("gitlab: failed to persist webhook secret for legacy connection",
					"connection_id", uuidToString(conns[i].ID), "err", serr)
				continue
			}
			conns[i] = updated
		}
	}
	resp := make([]GitLabConnectionResponse, len(conns))
	for i, c := range conns {
		resp[i] = gitlabConnectionToResponse(c, canManage)
	}
	writeJSON(w, http.StatusOK, ListGitLabConnectionsResponse{
		Connections: resp,
		Configured:  isGitLabConfigured(),
		CanManage:   canManage,
	})
}

// RotateGitLabConnectionWebhookSecret (POST /api/workspaces/{id}/gitlab/connections/{connectionId}/rotate-webhook-secret)
// issues a fresh per-connection secret. The previous secret stops working immediately.
func (h *Handler) RotateGitLabConnectionWebhookSecret(w http.ResponseWriter, r *http.Request) {
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
	secret, err := generateGitLabWebhookSecret()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate webhook secret")
		return
	}
	conn, err := h.Queries.SetGitLabConnectionWebhookSecret(r.Context(), db.SetGitLabConnectionWebhookSecretParams{
		ID:            connUUID,
		WebhookSecret: secret,
		WorkspaceID:   wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "connection not found")
		return
	}
	writeJSON(w, http.StatusOK, gitlabConnectionToResponse(conn, true))
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

type gitlabTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

func parseGitLabTokenResponse(r io.Reader) (gitlabTokenResponse, pgtype.Timestamptz, error) {
	var body gitlabTokenResponse
	if err := json.NewDecoder(r).Decode(&body); err != nil {
		return body, pgtype.Timestamptz{}, err
	}
	if body.AccessToken == "" {
		return body, pgtype.Timestamptz{}, errors.New("empty access_token in response")
	}
	exp := pgtype.Timestamptz{}
	if body.ExpiresIn > 0 {
		exp = pgtype.Timestamptz{Time: time.Now().Add(time.Duration(body.ExpiresIn) * time.Second), Valid: true}
	}
	return body, exp, nil
}

func gitlabExchangeCode(ctx context.Context, code string) (tok gitlabTokenResponse, expiresAt pgtype.Timestamptz, err error) {
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
		return tok, pgtype.Timestamptz{}, err
	}
	defer resp.Body.Close()
	return parseGitLabTokenResponse(resp.Body)
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

// LinkGitLabIssueForIssue (PUT /api/issues/{id}/gitlab-issue) manually links a
// GitLab issue to a Multica issue by fetching the GitLab project ID from the API.
func (h *Handler) LinkGitLabIssueForIssue(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}

	var body struct {
		ProjectPath string `json:"project_path"`
		GlIssueIID  int32  `json:"gl_issue_iid"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ProjectPath == "" || body.GlIssueIID == 0 {
		writeError(w, http.StatusBadRequest, "project_path and gl_issue_iid are required")
		return
	}

	conn, err := h.Queries.GetFirstGitLabConnectionByWorkspace(r.Context(), issue.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusNotFound, "no gitlab connection for workspace")
		return
	}

	// Decrypt access token, refreshing if expired.
	var plainToken string
	if conn.TokenExpiresAt.Valid && conn.TokenExpiresAt.Time.Before(time.Now()) {
		plainToken, err = h.refreshGitLabToken(r.Context(), conn)
		if err != nil {
			writeError(w, http.StatusBadGateway, "gitlab token refresh failed")
			return
		}
	} else {
		tokenBytes, decErr := base64.StdEncoding.DecodeString(conn.AccessToken)
		if decErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode token")
			return
		}
		plain, openErr := h.GitLabBox.Open(tokenBytes)
		if openErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to decrypt token")
			return
		}
		plainToken = string(plain)
	}

	// Fetch gl_project_id from GitLab.
	encodedPath := url.PathEscape(body.ProjectPath)
	glAPIURL := gitlabAPIURL() + fmt.Sprintf("/projects/%s/issues/%d", encodedPath, body.GlIssueIID)
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, glAPIURL, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build gitlab request")
		return
	}
	req.Header.Set("Authorization", "Bearer "+plainToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "gitlab api request failed")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		writeError(w, http.StatusNotFound, "gitlab issue not found")
		return
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		writeError(w, http.StatusBadGateway, "gitlab api returned error")
		return
	}
	var glIssue struct {
		ProjectID int64 `json:"project_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&glIssue); err != nil || glIssue.ProjectID == 0 {
		writeError(w, http.StatusBadGateway, "failed to parse gitlab issue")
		return
	}

	row, err := h.Queries.InsertGitLabIssue(r.Context(), db.InsertGitLabIssueParams{
		WorkspaceID:  issue.WorkspaceID,
		ConnectionID: conn.ID,
		ProjectPath:  body.ProjectPath,
		GlIssueIid:   body.GlIssueIID,
		GlProjectID:  glIssue.ProjectID,
		IssueID:      issue.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to link gitlab issue")
		return
	}

	issueURL := gitlabBaseURL() + "/" + row.ProjectPath + "/-/issues/" + strconv.Itoa(int(row.GlIssueIid))
	writeJSON(w, http.StatusOK, GitLabIssueResponse{
		GlIssueIID:  row.GlIssueIid,
		ProjectPath: row.ProjectPath,
		URL:         issueURL,
	})
}

// UnlinkGitLabIssueForIssue (DELETE /api/issues/{id}/gitlab-issue) removes the
// manual or auto-created link between a Multica issue and a GitLab issue.
func (h *Handler) UnlinkGitLabIssueForIssue(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if err := h.Queries.DeleteGitLabIssueByIssueID(r.Context(), db.DeleteGitLabIssueByIssueIDParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unlink gitlab issue")
		return
	}
	w.WriteHeader(http.StatusNoContent)
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

// GetGitLabIssueForIssue (GET /api/issues/{id}/gitlab-issue) returns the linked
// GitLab issue info for display in the sidebar, or 404 if none.
func (h *Handler) GetGitLabIssueForIssue(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issueUUID, ok := parseUUIDOrBadRequest(w, issueID, "issue id")
	if !ok {
		return
	}

	glIssue, err := h.Queries.GetGitLabIssueByIssueID(r.Context(), issueUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "no gitlab issue linked")
		return
	}

	issueURL := gitlabBaseURL() + "/" + glIssue.ProjectPath + "/-/issues/" + strconv.Itoa(int(glIssue.GlIssueIid))
	writeJSON(w, http.StatusOK, GitLabIssueResponse{
		GlIssueIID:         glIssue.GlIssueIid,
		ProjectPath:        glIssue.ProjectPath,
		URL:                issueURL,
		GlAssigneeUsername: textToPtr(glIssue.GlAssigneeUsername),
	})
}
