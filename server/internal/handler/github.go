package handler

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ── Response shapes ─────────────────────────────────────────────────────────

type GitHubInstallationResponse struct {
	ID               string  `json:"id"`
	WorkspaceID      string  `json:"workspace_id"`
	InstallationID   int64   `json:"installation_id"`
	AccountLogin     string  `json:"account_login"`
	AccountType      string  `json:"account_type"`
	AccountAvatarURL *string `json:"account_avatar_url"`
	CreatedAt        string  `json:"created_at"`
}

type GitHubPullRequestResponse struct {
	ID              string  `json:"id"`
	WorkspaceID     string  `json:"workspace_id"`
	RepoOwner       string  `json:"repo_owner"`
	RepoName        string  `json:"repo_name"`
	Number          int32   `json:"number"`
	Title           string  `json:"title"`
	State           string  `json:"state"`
	HtmlURL         string  `json:"html_url"`
	Branch          *string `json:"branch"`
	AuthorLogin     *string `json:"author_login"`
	AuthorAvatarURL *string `json:"author_avatar_url"`
	MergedAt        *string `json:"merged_at"`
	ClosedAt        *string `json:"closed_at"`
	PRCreatedAt     string  `json:"pr_created_at"`
	PRUpdatedAt     string  `json:"pr_updated_at"`
	// Mergeable state mirrors GitHub's `mergeable_state` field. We only
	// surface `clean`/`dirty` in the UI today; other values (`blocked`,
	// `behind`, `unstable`, `unknown`) round-trip but render as unknown.
	MergeableState *string `json:"mergeable_state"`
	// ChecksConclusion is the aggregated state of the latest CI check
	// suites for the PR's current head SHA. One of "passed", "failed",
	// "pending", or nil when no completed suite has been observed.
	ChecksConclusion *string `json:"checks_conclusion"`
	// Per-suite counts that drive the card's segmented progress bar.
	// Always present on list rows; bare upsert broadcasts default to 0
	// and the frontend hides the bar when total == 0.
	ChecksPassed  int64 `json:"checks_passed"`
	ChecksFailed  int64 `json:"checks_failed"`
	ChecksPending int64 `json:"checks_pending"`
	// Diff stats (lines added/removed and file count) sourced from the
	// `pull_request` webhook payload. Legacy rows that pre-date this
	// field default to 0; the frontend treats total == 0 as "unknown"
	// and hides the stats row.
	Additions    int32 `json:"additions"`
	Deletions    int32 `json:"deletions"`
	ChangedFiles int32 `json:"changed_files"`
}

type GitHubConnectResponse struct {
	URL       string `json:"url"`
	Configured bool  `json:"configured"`
}

func githubInstallationToResponse(i db.GithubInstallation) GitHubInstallationResponse {
	return GitHubInstallationResponse{
		ID:               uuidToString(i.ID),
		WorkspaceID:      uuidToString(i.WorkspaceID),
		InstallationID:   i.InstallationID,
		AccountLogin:     i.AccountLogin,
		AccountType:      i.AccountType,
		AccountAvatarURL: textToPtr(i.AccountAvatarUrl),
		CreatedAt:        timestampToString(i.CreatedAt),
	}
}

func githubPullRequestToResponse(p db.GithubPullRequest) GitHubPullRequestResponse {
	return GitHubPullRequestResponse{
		ID:              uuidToString(p.ID),
		WorkspaceID:     uuidToString(p.WorkspaceID),
		RepoOwner:       p.RepoOwner,
		RepoName:        p.RepoName,
		Number:          p.PrNumber,
		Title:           p.Title,
		State:           p.State,
		HtmlURL:         p.HtmlUrl,
		Branch:          textToPtr(p.Branch),
		AuthorLogin:     textToPtr(p.AuthorLogin),
		AuthorAvatarURL: textToPtr(p.AuthorAvatarUrl),
		MergedAt:        timestampToPtr(p.MergedAt),
		ClosedAt:        timestampToPtr(p.ClosedAt),
		PRCreatedAt:     timestampToString(p.PrCreatedAt),
		PRUpdatedAt:     timestampToString(p.PrUpdatedAt),
		MergeableState:  textToPtr(p.MergeableState),
		// A bare PR row has no aggregated check counts — webhook
		// broadcasts of a single PR fall through here and the frontend
		// re-queries the list for fresh counts.
		ChecksConclusion: nil,
		Additions:        p.Additions,
		Deletions:        p.Deletions,
		ChangedFiles:     p.ChangedFiles,
	}
}

func issuePullRequestRowToResponse(p db.ListPullRequestsByIssueRow) GitHubPullRequestResponse {
	return GitHubPullRequestResponse{
		ID:               uuidToString(p.ID),
		WorkspaceID:      uuidToString(p.WorkspaceID),
		RepoOwner:        p.RepoOwner,
		RepoName:         p.RepoName,
		Number:           p.PrNumber,
		Title:            p.Title,
		State:            p.State,
		HtmlURL:          p.HtmlUrl,
		Branch:           textToPtr(p.Branch),
		AuthorLogin:      textToPtr(p.AuthorLogin),
		AuthorAvatarURL:  textToPtr(p.AuthorAvatarUrl),
		MergedAt:         timestampToPtr(p.MergedAt),
		ClosedAt:         timestampToPtr(p.ClosedAt),
		PRCreatedAt:      timestampToString(p.PrCreatedAt),
		PRUpdatedAt:      timestampToString(p.PrUpdatedAt),
		MergeableState:   textToPtr(p.MergeableState),
		ChecksConclusion: aggregateChecksConclusion(p.ChecksFailed, p.ChecksPassed, p.ChecksPending, p.ChecksTotal),
		ChecksPassed:     p.ChecksPassed,
		ChecksFailed:     p.ChecksFailed,
		ChecksPending:    p.ChecksPending,
		Additions:        p.Additions,
		Deletions:        p.Deletions,
		ChangedFiles:     p.ChangedFiles,
	}
}

// aggregateChecksConclusion collapses the per-PR check_suite counts into a
// single status surfaced to the UI:
//   - any failed-class suite wins ("failed");
//   - any not-yet-completed suite makes the PR "pending";
//   - all completed and in the passed-class is "passed";
//   - no observed suite at all is nil (rendered as "no checks" / hidden).
func aggregateChecksConclusion(failed, passed, pending, total int64) *string {
	if total == 0 {
		return nil
	}
	var v string
	switch {
	case failed > 0:
		v = "failed"
	case pending > 0:
		v = "pending"
	case passed > 0:
		v = "passed"
	default:
		return nil
	}
	return &v
}

// ── Connect / state token ───────────────────────────────────────────────────

// githubAppSlug returns the GitHub App slug used to build the install URL.
// Empty when the integration is not configured for this deployment.
func githubAppSlug() string { return strings.TrimSpace(os.Getenv("GITHUB_APP_SLUG")) }

// githubWebhookSecret is shared by webhook verification and state-token signing.
// We reuse the webhook secret as the state HMAC key so operators only need to
// configure one value.
func githubWebhookSecret() string { return strings.TrimSpace(os.Getenv("GITHUB_WEBHOOK_SECRET")) }

// isGitHubConfigured returns true only when BOTH the install slug and the
// webhook secret are set. The Connect button uses this single flag, so the
// frontend never offers a flow that the backend would reject.
func isGitHubConfigured() bool { return githubAppSlug() != "" && githubWebhookSecret() != "" }

// signState produces an opaque token that binds a workspace ID to the
// install flow so the setup callback can recover the workspace without
// trusting query params alone. Format: "<workspaceID>.<nonce>.<sigHex>".
func signState(workspaceID string) (string, error) {
	secret := githubWebhookSecret()
	if secret == "" {
		return "", errors.New("github integration is not configured")
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

func verifyState(token string) (string, bool) {
	secret := githubWebhookSecret()
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

// GitHubConnect (GET /api/workspaces/{id}/github/connect) returns the URL the
// browser should open to install the Multica GitHub App against the caller's
// repos. The state token binds the resulting setup callback to this workspace.
func (h *Handler) GitHubConnect(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	if _, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id"); !ok {
		return
	}
	if !isGitHubConfigured() {
		writeJSON(w, http.StatusOK, GitHubConnectResponse{Configured: false})
		return
	}
	slug := githubAppSlug()
	state, err := signState(workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to sign state")
		return
	}
	installURL := fmt.Sprintf(
		"https://github.com/apps/%s/installations/new?state=%s",
		url.PathEscape(slug),
		url.QueryEscape(state),
	)
	writeJSON(w, http.StatusOK, GitHubConnectResponse{URL: installURL, Configured: true})
}

// GitHubSetupCallback (GET /api/github/setup) handles the redirect GitHub
// sends after a user installs (or re-authorizes) the App. We expect
// ?installation_id=<id>&state=<signed token>. We persist the installation
// row (workspace ↔ installation_id mapping), then bounce the user back to
// the Settings → Integrations page in the web app.
func (h *Handler) GitHubSetupCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	installationIDStr := q.Get("installation_id")
	state := q.Get("state")
	frontend := strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN"))
	if frontend == "" {
		frontend = "http://localhost:3000"
	}
	settingsURL := strings.TrimRight(frontend, "/") + "/settings"

	if installationIDStr == "" || state == "" {
		http.Redirect(w, r, settingsURL+"?github_error=missing_params", http.StatusFound)
		return
	}
	workspaceID, ok := verifyState(state)
	if !ok {
		http.Redirect(w, r, settingsURL+"?github_error=invalid_state", http.StatusFound)
		return
	}
	installationID, err := strconv.ParseInt(installationIDStr, 10, 64)
	if err != nil {
		http.Redirect(w, r, settingsURL+"?github_error=bad_installation_id", http.StatusFound)
		return
	}
	wsUUID, err := parseStrictUUID(workspaceID)
	if err != nil {
		http.Redirect(w, r, settingsURL+"?github_error=bad_workspace", http.StatusFound)
		return
	}
	// Resolve the installation against GitHub's API to capture display info.
	// If the App auth is not configured we still create the row with the
	// minimum we know; webhook events will refresh it as soon as one fires.
	login, accountType, avatar := fetchInstallationAccount(r.Context(), installationID)

	// Best-effort capture of the connecting user (may be nil if the public
	// callback was hit without a session — e.g. user wasn't logged in to
	// Multica when they finished the GitHub install). Either way we save
	// the row so the workspace owner sees the connection on next reload.
	connectedBy := pgtype.UUID{}
	if userID := requestUserID(r); userID != "" {
		if u, err := parseStrictUUID(userID); err == nil {
			connectedBy = u
		}
	}

	inst, err := h.Queries.CreateGitHubInstallation(r.Context(), db.CreateGitHubInstallationParams{
		WorkspaceID:      wsUUID,
		InstallationID:   installationID,
		AccountLogin:     login,
		AccountType:      accountType,
		AccountAvatarUrl: ptrToText(avatar),
		ConnectedByID:    connectedBy,
	})
	if err != nil {
		slog.Error("github: failed to persist installation", "err", err, "installation_id", installationID)
		http.Redirect(w, r, settingsURL+"?github_error=persist_failed", http.StatusFound)
		return
	}
	h.publish(protocol.EventGitHubInstallationCreated, workspaceID, "system", "", map[string]any{
		"installation": githubInstallationToResponse(inst),
	})
	http.Redirect(w, r, settingsURL+"?github_connected=1", http.StatusFound)
}

// fetchInstallationAccount tries to enrich the installation row with the
// account name + avatar via GitHub's public API. We deliberately do NOT
// require GitHub App JWT auth here — the install endpoint is publicly
// readable for installations on public accounts, and on failure we fall
// back to placeholders that the next webhook will overwrite.
func fetchInstallationAccount(ctx context.Context, installationID int64) (login, accountType string, avatar *string) {
	login = "unknown"
	accountType = "User"
	avatar = nil
	url := fmt.Sprintf("https://api.github.com/app/installations/%d", installationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	var body struct {
		Account struct {
			Login     string `json:"login"`
			Type      string `json:"type"`
			AvatarURL string `json:"avatar_url"`
		} `json:"account"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return
	}
	if body.Account.Login != "" {
		login = body.Account.Login
	}
	if body.Account.Type != "" {
		accountType = body.Account.Type
	}
	if body.Account.AvatarURL != "" {
		v := body.Account.AvatarURL
		avatar = &v
	}
	return
}

// ── Listing / disconnect ────────────────────────────────────────────────────

func (h *Handler) ListGitHubInstallations(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	rows, err := h.Queries.ListGitHubInstallationsByWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list installations")
		return
	}
	out := make([]GitHubInstallationResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, githubInstallationToResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"installations": out, "configured": isGitHubConfigured()})
}

func (h *Handler) DeleteGitHubInstallation(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	id := chi.URLParam(r, "installationId")
	idUUID, ok := parseUUIDOrBadRequest(w, id, "installation id")
	if !ok {
		return
	}
	if err := h.Queries.DeleteGitHubInstallation(r.Context(), db.DeleteGitHubInstallationParams{
		ID:          idUUID,
		WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove installation")
		return
	}
	h.publish(protocol.EventGitHubInstallationDeleted, workspaceID, "system", "", map[string]any{
		"id": id,
	})
	w.WriteHeader(http.StatusNoContent)
}

// ── List PRs for an issue ───────────────────────────────────────────────────

func (h *Handler) ListPullRequestsForIssue(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	rows, err := h.Queries.ListPullRequestsByIssue(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list pull requests")
		return
	}
	out := make([]GitHubPullRequestResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, issuePullRequestRowToResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"pull_requests": out})
}

// ── Webhook ─────────────────────────────────────────────────────────────────

// identifierRe extracts identifiers like "MUL-1510" from text. Case-insensitive
// because branch names are conventionally lowercase but issue prefixes are
// uppercase. Word boundary on the left prevents matching inside email-style
// strings (e.g. "abc@MUL-1") and the digit anchor on the right rules out
// version numbers like "v1.2-3".
var identifierRe = regexp.MustCompile(`(?i)\b([a-z][a-z0-9]{1,9})-(\d+)\b`)

// HandleGitHubWebhook (POST /api/webhooks/github) is GitHub's destination for
// every event from a connected installation. We verify HMAC signature, route
// on X-GitHub-Event, and either upsert PR rows + auto-link to issues or
// remove the installation on uninstall.
func (h *Handler) HandleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20)) // 10 MiB cap
	if err != nil {
		writeError(w, http.StatusBadRequest, "read body failed")
		return
	}
	secret := githubWebhookSecret()
	if secret == "" {
		// Refusing to process webhooks at all is safer than treating an
		// unconfigured deployment as "all signatures valid".
		writeError(w, http.StatusServiceUnavailable, "github webhooks not configured")
		return
	}
	sigHeader := r.Header.Get("X-Hub-Signature-256")
	if !verifyWebhookSignature(secret, sigHeader, body) {
		writeError(w, http.StatusUnauthorized, "invalid signature")
		return
	}
	event := r.Header.Get("X-GitHub-Event")
	ctx := r.Context()
	switch event {
	case "ping":
		writeJSON(w, http.StatusOK, map[string]string{"ok": "pong"})
		return
	case "installation":
		h.handleInstallationEvent(ctx, body)
	case "pull_request":
		h.handlePullRequestEvent(ctx, body)
	case "check_suite":
		h.handleCheckSuiteEvent(ctx, body)
	default:
		// Acknowledge every event so GitHub doesn't mark the endpoint failing,
		// but ignore types we don't model.
	}
	w.WriteHeader(http.StatusAccepted)
}

func verifyWebhookSignature(secret, header string, body []byte) bool {
	const prefix = "sha256="
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	want, err := hex.DecodeString(strings.TrimPrefix(header, prefix))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), want)
}

type ghInstallationPayload struct {
	Action       string `json:"action"`
	Installation struct {
		ID      int64 `json:"id"`
		Account struct {
			Login     string `json:"login"`
			Type      string `json:"type"`
			AvatarURL string `json:"avatar_url"`
		} `json:"account"`
	} `json:"installation"`
}

func (h *Handler) handleInstallationEvent(ctx context.Context, body []byte) {
	var p ghInstallationPayload
	if err := json.Unmarshal(body, &p); err != nil {
		slog.Warn("github: bad installation payload", "err", err)
		return
	}
	switch p.Action {
	case "deleted", "suspend":
		// User removed the App on GitHub — drop our row so the workspace
		// stops trusting this installation_id. We DELETE … RETURNING so
		// the broadcast can be scoped to the right workspace; events
		// without WorkspaceID are dropped by the realtime listener and
		// would leave already-open Settings tabs stale.
		deleted, err := h.Queries.DeleteGitHubInstallationByInstallationID(ctx, p.Installation.ID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return // already gone — nothing to broadcast
			}
			slog.Warn("github: delete installation failed", "err", err, "installation_id", p.Installation.ID)
			return
		}
		h.publish(protocol.EventGitHubInstallationDeleted, uuidToString(deleted.WorkspaceID), "system", "", map[string]any{
			"installation_id": p.Installation.ID,
			"id":              uuidToString(deleted.ID),
		})
	case "created", "new_permissions_accepted", "unsuspend":
		// We don't know which workspace this maps to from the webhook
		// alone — the setup callback handler is what binds installation
		// to workspace, so we just refresh metadata if we already have
		// a row.
		existing, err := h.Queries.GetGitHubInstallationByInstallationID(ctx, p.Installation.ID)
		if err != nil {
			return
		}
		avatar := p.Installation.Account.AvatarURL
		_, err = h.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
			WorkspaceID:      existing.WorkspaceID,
			InstallationID:   p.Installation.ID,
			AccountLogin:     p.Installation.Account.Login,
			AccountType:      coalesce(p.Installation.Account.Type, "User"),
			AccountAvatarUrl: ptrToText(strPtrOrNil(avatar)),
			ConnectedByID:    existing.ConnectedByID,
		})
		if err != nil {
			slog.Warn("github: refresh installation failed", "err", err)
		}
	}
}

type ghPullRequestPayload struct {
	Action      string `json:"action"`
	PullRequest struct {
		Number         int32  `json:"number"`
		HTMLURL        string `json:"html_url"`
		Title          string `json:"title"`
		Body           string `json:"body"`
		State          string `json:"state"`
		Draft          bool   `json:"draft"`
		Merged         bool   `json:"merged"`
		MergedAt       string `json:"merged_at"`
		ClosedAt       string `json:"closed_at"`
		CreatedAt      string `json:"created_at"`
		UpdatedAt      string `json:"updated_at"`
		MergeableState string `json:"mergeable_state"`
		Additions      int32  `json:"additions"`
		Deletions      int32  `json:"deletions"`
		ChangedFiles   int32  `json:"changed_files"`
		Head           struct {
			Ref string `json:"ref"`
			SHA string `json:"sha"`
		} `json:"head"`
		User struct {
			Login     string `json:"login"`
			AvatarURL string `json:"avatar_url"`
		} `json:"user"`
	} `json:"pull_request"`
	Changes *ghPRChanges `json:"changes"`
	Repository struct {
		Name  string `json:"name"`
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
}

func (h *Handler) handlePullRequestEvent(ctx context.Context, body []byte) {
	var p ghPullRequestPayload
	if err := json.Unmarshal(body, &p); err != nil {
		slog.Warn("github: bad pull_request payload", "err", err)
		return
	}
	if p.Installation.ID == 0 {
		return
	}
	inst, err := h.Queries.GetGitHubInstallationByInstallationID(ctx, p.Installation.ID)
	if err != nil {
		// Webhook from an installation we never wired up — nothing we
		// can attribute to a workspace, so drop it silently.
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("github: lookup installation failed", "err", err)
		}
		return
	}

	state := derivePRState(p.PullRequest.State, p.PullRequest.Draft, p.PullRequest.Merged)
	mergeable, clearMergeable := derivePRMergeableState(p.Action, p.PullRequest.MergeableState, baseRefChanged(p.Changes))
	pr, err := h.Queries.UpsertGitHubPullRequest(ctx, db.UpsertGitHubPullRequestParams{
		WorkspaceID:           inst.WorkspaceID,
		InstallationID:        inst.InstallationID,
		RepoOwner:             p.Repository.Owner.Login,
		RepoName:              p.Repository.Name,
		PrNumber:              p.PullRequest.Number,
		Title:                 p.PullRequest.Title,
		State:                 state,
		HtmlUrl:               p.PullRequest.HTMLURL,
		Branch:                ptrToText(strPtrOrNil(p.PullRequest.Head.Ref)),
		AuthorLogin:           ptrToText(strPtrOrNil(p.PullRequest.User.Login)),
		AuthorAvatarUrl:       ptrToText(strPtrOrNil(p.PullRequest.User.AvatarURL)),
		MergedAt:              parseGHTime(p.PullRequest.MergedAt),
		ClosedAt:              parseGHTime(p.PullRequest.ClosedAt),
		PrCreatedAt:           parseGHTimeRequired(p.PullRequest.CreatedAt),
		PrUpdatedAt:           parseGHTimeRequired(p.PullRequest.UpdatedAt),
		HeadSha:               p.PullRequest.Head.SHA,
		MergeableState:        mergeable,
		ClearMergeableState:   pgtype.Bool{Bool: clearMergeable, Valid: true},
		Additions:             p.PullRequest.Additions,
		Deletions:             p.PullRequest.Deletions,
		ChangedFiles:          p.PullRequest.ChangedFiles,
	})
	if err != nil {
		slog.Warn("github: upsert pr failed", "err", err)
		return
	}

	workspaceID := uuidToString(inst.WorkspaceID)
	resp := githubPullRequestToResponse(pr)

	// Auto-link: scan title/body/branch for issue identifiers, look them
	// up in this workspace, attach the link rows. Idempotent (ON CONFLICT
	// DO NOTHING) so re-firing the webhook doesn't duplicate.
	idents := extractIdentifiers(p.PullRequest.Title, p.PullRequest.Body, p.PullRequest.Head.Ref)
	prefix := h.getIssuePrefix(ctx, inst.WorkspaceID)
	linkedIssueIDs := make([]string, 0, len(idents))
	for _, id := range idents {
		issue, ok := h.lookupIssueByIdentifier(ctx, inst.WorkspaceID, prefix, id)
		if !ok {
			continue
		}
		if err := h.Queries.LinkIssueToPullRequest(ctx, db.LinkIssueToPullRequestParams{
			IssueID:        issue.ID,
			PullRequestID:  pr.ID,
			LinkedByType:   strToText("system"),
			LinkedByID:     pgtype.UUID{},
		}); err != nil {
			slog.Warn("github: link failed", "err", err)
			continue
		}
		linkedIssueIDs = append(linkedIssueIDs, uuidToString(issue.ID))

		// A terminal PR event (`merged` or `closed`) may be the moment the
		// last in-flight sibling resolves, so we re-evaluate the issue on
		// both. We advance the issue to done when:
		//   1. the issue isn't already terminal (`done` / `cancelled`);
		//   2. no sibling PR is still `open` / `draft`;
		//   3. at least one linked PR (this one or a sibling) is `merged`.
		// Rule (3) prevents an "all closed-without-merge" sequence from
		// silently auto-closing the issue — if nothing was ever delivered,
		// the user should decide what to do manually.
		if (state == "merged" || state == "closed") && issue.Status != "done" && issue.Status != "cancelled" {
			counts, err := h.Queries.GetSiblingPullRequestStateCountsForIssue(ctx, db.GetSiblingPullRequestStateCountsForIssueParams{
				IssueID: issue.ID,
				ID:      pr.ID,
			})
			if err != nil {
				slog.Warn("github: count sibling pr states failed", "err", err, "issue_id", uuidToString(issue.ID))
				continue
			}
			anyMerged := state == "merged" || counts.MergedCount > 0
			if counts.OpenCount == 0 && anyMerged {
				h.advanceIssueToDone(ctx, issue, workspaceID)
			}
		}
	}

	// Broadcast PR change to the workspace so any open issue detail page
	// re-queries its PR list.
	h.publish(protocol.EventPullRequestUpdated, workspaceID, "system", "", map[string]any{
		"pull_request": resp,
		"linked_issue_ids": linkedIssueIDs,
	})
}

// ── check_suite webhook ────────────────────────────────────────────────────

type ghCheckSuitePayload struct {
	Action     string `json:"action"`
	CheckSuite struct {
		ID         int64  `json:"id"`
		HeadSHA    string `json:"head_sha"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		UpdatedAt  string `json:"updated_at"`
		App        struct {
			ID int64 `json:"id"`
		} `json:"app"`
		PullRequests []struct {
			Number int32 `json:"number"`
		} `json:"pull_requests"`
	} `json:"check_suite"`
	Repository struct {
		Name  string `json:"name"`
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
}

// handleCheckSuiteEvent records the CI suite state for each PR the suite
// references. MVP only persists terminal events (`completed`); GitHub sends
// `requested`/`rerequested` for some apps but those carry no useful
// conclusion and the RFC restricts us to suite-level aggregation.
//
// The suite payload may reference multiple PRs (e.g. the same head SHA is
// open against several base branches), so we iterate. A reference whose PR
// hasn't been mirrored locally is logged and skipped — auto-backfill from
// GitHub's REST API is a v2 enhancement.
func (h *Handler) handleCheckSuiteEvent(ctx context.Context, body []byte) {
	var p ghCheckSuitePayload
	if err := json.Unmarshal(body, &p); err != nil {
		slog.Warn("github: bad check_suite payload", "err", err)
		return
	}
	if p.Action != "completed" {
		// MVP scope: only completed suites carry a conclusion we can
		// surface. queued / in_progress events would feed a future
		// "real pending" display path.
		return
	}
	if p.Installation.ID == 0 {
		return
	}
	inst, err := h.Queries.GetGitHubInstallationByInstallationID(ctx, p.Installation.ID)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("github: lookup installation failed", "err", err)
		}
		return
	}
	if len(p.CheckSuite.PullRequests) == 0 {
		// Forks emit suites whose `pull_requests` array is empty for
		// the upstream repo. We have no way to attribute the result
		// without polling, so drop with a hint.
		slog.Info("github: check_suite has no associated PRs", "suite_id", p.CheckSuite.ID)
		return
	}
	updatedAt := parseGHTimeRequired(p.CheckSuite.UpdatedAt)

	affectedWorkspaces := map[string]struct{}{}
	affectedIssues := map[string]struct{}{}
	for _, prRef := range p.CheckSuite.PullRequests {
		// Scope the lookup to the installation's workspace. The
		// (workspace_id, repo_owner, repo_name, pr_number) tuple is the
		// real uniqueness key: if the same repo lived under a different
		// workspace historically, a bare (owner, repo, number) lookup
		// could return either row arbitrarily and land this suite on
		// the wrong PR (or skip the right one because the installation
		// ids no longer match).
		pr, err := h.Queries.GetGitHubPullRequest(ctx, db.GetGitHubPullRequestParams{
			WorkspaceID: inst.WorkspaceID,
			RepoOwner:   p.Repository.Owner.Login,
			RepoName:    p.Repository.Name,
			PrNumber:    prRef.Number,
		})
		if err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				slog.Warn("github: lookup pr for check_suite failed", "err", err)
			}
			slog.Info("github: check_suite for unknown PR — skipping",
				"repo", p.Repository.Owner.Login+"/"+p.Repository.Name,
				"pr", prRef.Number,
				"suite_id", p.CheckSuite.ID,
			)
			continue
		}
		if err := h.Queries.UpsertPullRequestCheckSuite(ctx, db.UpsertPullRequestCheckSuiteParams{
			PrID:       pr.ID,
			SuiteID:    p.CheckSuite.ID,
			HeadSha:    p.CheckSuite.HeadSHA,
			AppID:      p.CheckSuite.App.ID,
			Conclusion: strToText(p.CheckSuite.Conclusion),
			Status:     p.CheckSuite.Status,
			UpdatedAt:  updatedAt,
		}); err != nil {
			slog.Warn("github: upsert check_suite failed", "err", err, "suite_id", p.CheckSuite.ID)
			continue
		}
		affectedWorkspaces[uuidToString(pr.WorkspaceID)] = struct{}{}
		issues, err := h.Queries.ListIssueIDsForPullRequest(ctx, pr.ID)
		if err == nil {
			for _, id := range issues {
				affectedIssues[uuidToString(id)] = struct{}{}
			}
		}
	}

	// Broadcast on the existing event so the issue page just re-queries
	// the PR list. We don't pass a single pull_request payload here
	// because a suite can touch several and the listener already
	// invalidates by issue.
	for ws := range affectedWorkspaces {
		linked := make([]string, 0, len(affectedIssues))
		for id := range affectedIssues {
			linked = append(linked, id)
		}
		h.publish(protocol.EventPullRequestUpdated, ws, "system", "", map[string]any{
			"linked_issue_ids": linked,
		})
	}
}

// derivePRMergeableState resolves the upsert behaviour for the PR row's
// mergeable_state column on a `pull_request` webhook. It returns three
// states encoded as (value, clear):
//
//   - clear=true → force the column to NULL. State-changing actions (`opened`,
//     `synchronize`, `reopened`, or a base-branch swap) must blank the value
//     because GitHub re-computes mergeability asynchronously; the payload may
//     still carry the previous head's clean/dirty answer, and trusting it
//     would surface a stale verdict against the new head.
//   - clear=false, value valid → write the value. The event carried a
//     concrete verdict we should persist.
//   - clear=false, value invalid → preserve the existing column. Metadata
//     events (labeled/assigned/edited-without-base-swap) ship pull_request
//     payloads with mergeable_state empty even when the previous verdict is
//     still accurate, and silently overwriting clean/dirty with NULL would
//     drop information GitHub only refreshes lazily.
func derivePRMergeableState(action, payload string, baseRefChanged bool) (pgtype.Text, bool) {
	if action == "opened" || action == "synchronize" || action == "reopened" {
		return pgtype.Text{}, true
	}
	if action == "edited" && baseRefChanged {
		return pgtype.Text{}, true
	}
	if payload == "" {
		return pgtype.Text{}, false
	}
	return pgtype.Text{String: payload, Valid: true}, false
}

// ghPRChanges captures the only field of `pull_request.edited`'s `changes`
// payload we care about: a base-branch swap. Everything else (title, body)
// leaves mergeability intact.
type ghPRChanges struct {
	Base *struct {
		Ref *struct {
			From string `json:"from"`
		} `json:"ref"`
	} `json:"base"`
}

// baseRefChanged returns true when a pull_request.edited event indicates the
// PR's base branch was swapped. Only this kind of edit invalidates the
// existing mergeable_state.
func baseRefChanged(c *ghPRChanges) bool {
	return c != nil && c.Base != nil && c.Base.Ref != nil && c.Base.Ref.From != ""
}

func derivePRState(state string, draft, merged bool) string {
	if merged {
		return "merged"
	}
	if state == "closed" {
		return "closed"
	}
	if draft {
		return "draft"
	}
	return "open"
}

func parseGHTime(s string) pgtype.Timestamptz {
	if s == "" {
		return pgtype.Timestamptz{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func parseGHTimeRequired(s string) pgtype.Timestamptz {
	t := parseGHTime(s)
	if !t.Valid {
		return pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
	}
	return t
}

// extractIdentifiers pulls every "PREFIX-NUMBER" match across the supplied
// fields, deduplicating in input order.
func extractIdentifiers(parts ...string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, src := range parts {
		for _, m := range identifierRe.FindAllStringSubmatch(src, -1) {
			ident := strings.ToUpper(m[1]) + "-" + m[2]
			if _, dup := seen[ident]; dup {
				continue
			}
			seen[ident] = struct{}{}
			out = append(out, ident)
		}
	}
	return out
}

// lookupIssueByIdentifier looks up an issue in the given workspace by its
// "PREFIX-NUMBER" identifier. Returns the row + true if the prefix matches
// the workspace's configured prefix and the number resolves to a real issue.
func (h *Handler) lookupIssueByIdentifier(ctx context.Context, workspaceID pgtype.UUID, prefix, identifier string) (db.Issue, bool) {
	idx := strings.LastIndex(identifier, "-")
	if idx < 0 {
		return db.Issue{}, false
	}
	gotPrefix, numStr := identifier[:idx], identifier[idx+1:]
	if !strings.EqualFold(gotPrefix, prefix) {
		return db.Issue{}, false
	}
	n, err := strconv.Atoi(numStr)
	if err != nil {
		return db.Issue{}, false
	}
	issue, err := h.Queries.GetIssueByNumber(ctx, db.GetIssueByNumberParams{
		WorkspaceID: workspaceID,
		Number:      int32(n),
	})
	if err != nil {
		return db.Issue{}, false
	}
	return issue, true
}

func (h *Handler) advanceIssueToDone(ctx context.Context, issue db.Issue, workspaceID string) {
	updated, err := h.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID:     issue.ID,
		Status: "done",
	})
	if err != nil {
		slog.Warn("github: advance issue to done failed", "err", err)
		return
	}
	prefix := h.getIssuePrefix(ctx, issue.WorkspaceID)
	resp := issueToResponse(updated, prefix)
	h.publish(protocol.EventIssueUpdated, workspaceID, "system", "", map[string]any{
		"issue":          resp,
		"status_changed": true,
		"prev_status":    issue.Status,
		"creator_type":   issue.CreatorType,
		"creator_id":     uuidToString(issue.CreatorID),
		"source":         "github_pr_merged",
	})
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func parseStrictUUID(s string) (pgtype.UUID, error) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return pgtype.UUID{}, err
	}
	return u, nil
}

func coalesce(a, fallback string) string {
	if strings.TrimSpace(a) == "" {
		return fallback
	}
	return a
}

func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	v := s
	return &v
}
