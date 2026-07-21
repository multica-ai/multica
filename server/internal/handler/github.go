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
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// githubAPIBase is the base URL for GitHub's REST API. Mutable so tests can
// point fetchInstallationAccount at an httptest server without touching the
// real GitHub.
var githubAPIBase = "https://api.github.com"

// ── Response shapes ─────────────────────────────────────────────────────────

// GitHubInstallationResponse is the JSON shape returned by the installation
// list endpoint and broadcast on installation-related WS events.
//
// InstallationID is admin-only: the numeric GitHub installation_id is the
// management handle used by the Connect/Disconnect flows, so non-admin
// members receive responses with the field omitted. The list handler gates
// it by role; realtime broadcasts always omit it because the WS fanout has
// no per-recipient view (admins re-query the list endpoint on invalidation
// to recover the management handle).
type GitHubInstallationResponse struct {
	ID               string  `json:"id"`
	WorkspaceID      string  `json:"workspace_id"`
	InstallationID   *int64  `json:"installation_id,omitempty"`
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
	URL        string `json:"url"`
	Configured bool   `json:"configured"`
}

func githubInstallationToResponse(i db.GithubInstallation) GitHubInstallationResponse {
	instID := i.InstallationID
	return GitHubInstallationResponse{
		ID:               uuidToString(i.ID),
		WorkspaceID:      uuidToString(i.WorkspaceID),
		InstallationID:   &instID,
		AccountLogin:     i.AccountLogin,
		AccountType:      i.AccountType,
		AccountAvatarURL: textToPtr(i.AccountAvatarUrl),
		CreatedAt:        timestampToString(i.CreatedAt),
	}
}

// githubInstallationToBroadcast returns the same shape as the list endpoint's
// per-role response with the numeric `installation_id` stripped. Realtime
// events fan out to every WS client subscribed to the workspace, so the
// payload must match the weakest-role view — admin/owner clients re-query
// the list endpoint to recover the management handle. The frontend uses
// these events only to invalidate the installations query, so it does not
// read `installation_id` off the broadcast.
func githubInstallationToBroadcast(i db.GithubInstallation) GitHubInstallationResponse {
	resp := githubInstallationToResponse(i)
	resp.InstallationID = nil
	return resp
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
// the new Settings → GitHub tab in the web app (RFC MUL-2414 §4.1). The
// previous destination was the catch-all Settings page, which after the
// GitHub-tab split would land users on the default profile tab instead of
// the place that shows the connection they just completed.
func (h *Handler) GitHubSetupCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	installationIDStr := q.Get("installation_id")
	state := q.Get("state")
	frontend := strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN"))
	if frontend == "" {
		frontend = "http://localhost:3000"
	}
	settingsURL := strings.TrimRight(frontend, "/") + "/settings?tab=github"

	if installationIDStr == "" || state == "" {
		http.Redirect(w, r, settingsURL+"&github_error=missing_params", http.StatusFound)
		return
	}
	workspaceID, ok := verifyState(state)
	if !ok {
		http.Redirect(w, r, settingsURL+"&github_error=invalid_state", http.StatusFound)
		return
	}
	installationID, err := strconv.ParseInt(installationIDStr, 10, 64)
	if err != nil {
		http.Redirect(w, r, settingsURL+"&github_error=bad_installation_id", http.StatusFound)
		return
	}
	wsUUID, err := parseStrictUUID(workspaceID)
	if err != nil {
		http.Redirect(w, r, settingsURL+"&github_error=bad_workspace", http.StatusFound)
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
		http.Redirect(w, r, settingsURL+"&github_error=persist_failed", http.StatusFound)
		return
	}
	inst, err = h.consumePendingGitHubInstallation(r.Context(), inst)
	if err != nil {
		slog.Error("github: failed to apply pending installation metadata", "err", err, "installation_id", installationID)
		http.Redirect(w, r, settingsURL+"&github_error=persist_failed", http.StatusFound)
		return
	}
	h.publish(protocol.EventGitHubInstallationCreated, workspaceID, "system", "", map[string]any{
		"installation": githubInstallationToBroadcast(inst),
	})
	http.Redirect(w, r, settingsURL+"&github_connected=1", http.StatusFound)
}

func (h *Handler) consumePendingGitHubInstallation(ctx context.Context, inst db.GithubInstallation) (db.GithubInstallation, error) {
	pending, err := h.Queries.GetPendingGitHubInstallation(ctx, inst.InstallationID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return inst, nil
		}
		return inst, err
	}
	refreshed, err := h.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:      inst.WorkspaceID,
		InstallationID:   inst.InstallationID,
		AccountLogin:     pending.AccountLogin,
		AccountType:      coalesce(pending.AccountType, "User"),
		AccountAvatarUrl: pending.AccountAvatarUrl,
		ConnectedByID:    inst.ConnectedByID,
	})
	if err != nil {
		return inst, err
	}
	if err := h.Queries.DeletePendingGitHubInstallation(ctx, inst.InstallationID); err != nil {
		return inst, err
	}
	return refreshed, nil
}

// fetchInstallationAccount tries to enrich the installation row with the
// account name + avatar from GitHub.
//
// GitHub's `GET /app/installations/{id}` endpoint requires GitHub App
// authentication (a JWT signed with the App's RSA private key). When the
// operator has configured GITHUB_APP_ID and GITHUB_APP_PRIVATE_KEY, we
// sign a short-lived JWT and use it; on any failure (env not set, key
// malformed, GitHub returns non-200) we fall back to the "unknown"
// placeholder. The next `installation` webhook delivery from GitHub will
// upsert the row with the real account info — see handleInstallationEvent.
//
// The HTTP call is synchronous (no independent timeout — that's a pre-
// existing wart of the install path), but we deliberately do NOT let a
// failure abort the setup callback: a network blip here just leaves the
// "unknown" placeholder in place, and the frontend re-queries on the
// realtime broadcast emitted by the webhook handler, so the UI converges
// without a manual refresh.
func fetchInstallationAccount(ctx context.Context, installationID int64) (login, accountType string, avatar *string) {
	login = "unknown"
	accountType = "User"
	avatar = nil
	endpoint := fmt.Sprintf("%s/app/installations/%d", strings.TrimRight(githubAPIBase, "/"), installationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token, err := signGitHubAppJWT(time.Now()); err != nil {
		// Misconfigured private key is operator-actionable — log so the
		// install path doesn't silently fall back to "unknown" forever
		// without leaving a breadcrumb.
		slog.Warn("github: sign App JWT failed", "err", err)
	} else if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
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

// signGitHubAppJWT mints the short-lived RS256 JWT GitHub requires for
// App-authenticated REST calls (see fetchInstallationAccount). Returns
// ("", nil) when the operator hasn't configured the App identity — that's
// a soft "App auth not available" signal, not an error, so callers can
// fall through to their unauthenticated path. A malformed
// GITHUB_APP_PRIVATE_KEY surfaces as an error so the operator notices.
//
// `now` is injected for deterministic tests; production callers pass
// time.Now().
func signGitHubAppJWT(now time.Time) (string, error) {
	appID := strings.TrimSpace(os.Getenv("GITHUB_APP_ID"))
	pemKey := strings.TrimSpace(os.Getenv("GITHUB_APP_PRIVATE_KEY"))
	if appID == "" || pemKey == "" {
		return "", nil
	}
	key, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(pemKey))
	if err != nil {
		return "", fmt.Errorf("parse GITHUB_APP_PRIVATE_KEY: %w", err)
	}
	// GitHub allows JWTs valid for up to 10 minutes. We back-date `iat`
	// by 60 seconds to absorb modest clock skew between us and GitHub
	// (otherwise an "iat in the future" verdict from GitHub fails the
	// request) and cap `exp` at 9 minutes ahead to stay inside the cap
	// even with the same skew applied.
	claims := jwt.MapClaims{
		"iat": now.Add(-60 * time.Second).Unix(),
		"exp": now.Add(9 * time.Minute).Unix(),
		"iss": appID,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signed, err := token.SignedString(key)
	if err != nil {
		return "", fmt.Errorf("sign App JWT: %w", err)
	}
	return signed, nil
}

// ── Listing / disconnect ────────────────────────────────────────────────────

// ListGitHubInstallations returns the workspace's connected GitHub
// installations to any workspace member. Connect/disconnect remain
// admin-only at the router level, so the response carries a `can_manage`
// hint and strips the numeric `installation_id` for non-admin callers —
// they get visibility into "is GitHub wired up, and by whom?" without the
// management handle.
func (h *Handler) ListGitHubInstallations(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	member, _ := middleware.MemberFromContext(r.Context())
	canManage := roleAllowed(member.Role, "owner", "admin")

	rows, err := h.Queries.ListGitHubInstallationsByWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list installations")
		return
	}
	out := make([]GitHubInstallationResponse, 0, len(rows))
	for _, row := range rows {
		resp := githubInstallationToResponse(row)
		if !canManage {
			resp.InstallationID = nil
		}
		out = append(out, resp)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"installations": out,
		"configured":    isGitHubConfigured(),
		"can_manage":    canManage,
	})
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

// closingIdentifierRe extracts identifiers that appear immediately after a
// GitHub-style closing keyword ("close[sd]?", "fix(e[sd])?", "resolve[sd]?"),
// optionally separated by a colon and whitespace. Matching is intentionally
// strict on adjacency — "Fix MUL-1" closes MUL-1, but "Fix login MUL-1"
// does not. This mirrors GitHub's own closing-keyword grammar and is the
// gate the webhook uses to decide whether to auto-advance an issue to
// `done` after a PR merges. References like "Follow up in MUL-2" and bare
// title prefixes like "MUL-1: ..." link the PR (via identifierRe) but
// never auto-close.
var closingIdentifierRe = regexp.MustCompile(
	`(?i)\b(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?)[:\s]+([a-z][a-z0-9]{1,9})-(\d+)\b`,
)

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

func githubInstallationAccountFromPayload(p ghInstallationPayload) (login, accountType string, avatar *string, ok bool) {
	login = strings.TrimSpace(p.Installation.Account.Login)
	if login == "" {
		return "", "", nil, false
	}
	accountType = coalesce(p.Installation.Account.Type, "User")
	avatar = strPtrOrNil(p.Installation.Account.AvatarURL)
	return login, accountType, avatar, true
}

func (h *Handler) handleInstallationEvent(ctx context.Context, body []byte) {
	var p ghInstallationPayload
	if err := json.Unmarshal(body, &p); err != nil {
		slog.Warn("github: bad installation payload", "err", err)
		return
	}
	switch p.Action {
	case "deleted", "suspend":
		// User removed/suspended the App on GitHub — trust in this
		// installation_id is gone entirely, so drop every workspace binding.
		// We DELETE … RETURNING so each broadcast can be scoped to its
		// workspace; events without WorkspaceID are dropped by the realtime
		// listener and would leave already-open Settings tabs stale.
		deleted, err := h.Queries.DeleteGitHubInstallationByInstallationID(ctx, p.Installation.ID)
		if err != nil {
			slog.Warn("github: delete installation failed", "err", err, "installation_id", p.Installation.ID)
			return
		}
		if err := h.Queries.DeletePendingGitHubInstallation(ctx, p.Installation.ID); err != nil {
			slog.Warn("github: delete pending installation failed", "err", err, "installation_id", p.Installation.ID)
		}
		// Broadcast the internal row id only — the numeric installation_id is
		// a management handle that non-admin members are not allowed to see.
		// The frontend invalidates the installations query on this event and
		// does not read the broadcast payload directly. One broadcast per
		// deleted binding so every affected workspace's Settings tab refreshes.
		for _, row := range deleted {
			h.publish(protocol.EventGitHubInstallationDeleted, uuidToString(row.WorkspaceID), "system", "", map[string]any{
				"id": uuidToString(row.ID),
			})
		}
	case "created", "new_permissions_accepted", "unsuspend":
		login, accountType, avatar, ok := githubInstallationAccountFromPayload(p)
		if !ok {
			slog.Warn("github: installation payload missing account login", "installation_id", p.Installation.ID)
			return
		}

		// We don't know which workspace(s) this maps to from the webhook
		// alone. If no setup callback has created a workspace binding yet,
		// keep the account metadata and let the callback consume it after it
		// creates github_installation.
		existing, err := h.Queries.ListGitHubInstallationsByInstallationID(ctx, p.Installation.ID)
		if err != nil {
			slog.Warn("github: lookup installation failed", "err", err, "installation_id", p.Installation.ID)
			return
		}
		if len(existing) == 0 {
			if _, err := h.Queries.UpsertPendingGitHubInstallation(ctx, db.UpsertPendingGitHubInstallationParams{
				InstallationID:   p.Installation.ID,
				AccountLogin:     login,
				AccountType:      accountType,
				AccountAvatarUrl: ptrToText(avatar),
			}); err != nil {
				slog.Warn("github: store pending installation failed", "err", err, "installation_id", p.Installation.ID)
			}
			return
		}
		// Refresh the account display metadata across every workspace binding;
		// workspace_id and connected_by_id are left untouched.
		refreshed, err := h.Queries.UpdateGitHubInstallationAccountByInstallationID(ctx, db.UpdateGitHubInstallationAccountByInstallationIDParams{
			InstallationID:   p.Installation.ID,
			AccountLogin:     login,
			AccountType:      accountType,
			AccountAvatarUrl: ptrToText(avatar),
		})
		if err != nil {
			slog.Warn("github: refresh installation failed", "err", err)
			return
		}
		if err := h.Queries.DeletePendingGitHubInstallation(ctx, p.Installation.ID); err != nil {
			slog.Warn("github: delete pending installation failed", "err", err, "installation_id", p.Installation.ID)
		}
		// Broadcast so any open Settings → GitHub tab re-queries the
		// installations list. Without this, a row created by the setup
		// callback with the "unknown" placeholder (e.g. because GitHub
		// App JWT auth wasn't configured, or this webhook arrived after
		// the user already loaded the page) would stay visibly stale
		// until the user manually refreshes. One broadcast per bound workspace.
		for _, inst := range refreshed {
			h.publish(protocol.EventGitHubInstallationCreated, uuidToString(inst.WorkspaceID), "system", "", map[string]any{
				"installation": githubInstallationToBroadcast(inst),
			})
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
	Changes    *ghPRChanges `json:"changes"`
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
	insts, err := h.Queries.ListGitHubInstallationsByInstallationID(ctx, p.Installation.ID)
	if err != nil {
		slog.Warn("github: lookup installation failed", "err", err)
		return
	}
	if len(insts) == 0 {
		// Webhook from an installation we never wired up — nothing we
		// can attribute to a workspace, so drop it silently.
		return
	}
	// #4855 lets one GitHub App installation bind to several workspaces. A
	// repo's events belong to every bound workspace, so fan the delivery out:
	// each workspace independently mirrors the PR and auto-links it against its
	// own issues (its own prefix + github toggle). Repo scope is whatever GitHub
	// authorized the installation for; we deliberately don't gate on the
	// workspace.repos registry — that list is "code the agent clones", not a
	// webhook subscription (MUL-4343).
	for _, inst := range insts {
		h.mirrorPullRequestForWorkspace(ctx, inst.WorkspaceID, inst.InstallationID, &p)
	}
}

// mirrorPullRequestForWorkspace mirrors a pull_request webhook into a single
// workspace: it upserts the PR row, replays any check_suite events that
// arrived before the PR was mirrored, auto-links referenced issues (gated by
// the workspace's github toggles), advances issues on terminal events, and
// broadcasts the change. Invoked once per workspace bound to the delivering
// installation.
func (h *Handler) mirrorPullRequestForWorkspace(ctx context.Context, wsID pgtype.UUID, installationID int64, p *ghPullRequestPayload) {
	state := derivePRState(p.PullRequest.State, p.PullRequest.Draft, p.PullRequest.Merged)
	mergeable, clearMergeable := derivePRMergeableState(p.Action, p.PullRequest.MergeableState, baseRefChanged(p.Changes))
	pr, err := h.Queries.UpsertGitHubPullRequest(ctx, db.UpsertGitHubPullRequestParams{
		WorkspaceID:         wsID,
		InstallationID:      installationID,
		RepoOwner:           p.Repository.Owner.Login,
		RepoName:            p.Repository.Name,
		PrNumber:            p.PullRequest.Number,
		Title:               p.PullRequest.Title,
		State:               state,
		HtmlUrl:             p.PullRequest.HTMLURL,
		Branch:              ptrToText(strPtrOrNil(p.PullRequest.Head.Ref)),
		AuthorLogin:         ptrToText(strPtrOrNil(p.PullRequest.User.Login)),
		AuthorAvatarUrl:     ptrToText(strPtrOrNil(p.PullRequest.User.AvatarURL)),
		MergedAt:            parseGHTime(p.PullRequest.MergedAt),
		ClosedAt:            parseGHTime(p.PullRequest.ClosedAt),
		PrCreatedAt:         parseGHTimeRequired(p.PullRequest.CreatedAt),
		PrUpdatedAt:         parseGHTimeRequired(p.PullRequest.UpdatedAt),
		HeadSha:             p.PullRequest.Head.SHA,
		MergeableState:      mergeable,
		ClearMergeableState: pgtype.Bool{Bool: clearMergeable, Valid: true},
		Additions:           p.PullRequest.Additions,
		Deletions:           p.PullRequest.Deletions,
		ChangedFiles:        p.PullRequest.ChangedFiles,
	})
	if err != nil {
		slog.Warn("github: upsert pr failed", "err", err)
		return
	}

	// Drain any check_suite events that arrived before this PR row was
	// mirrored (out-of-order webhook delivery). Each drained row is
	// replayed through the same upsert path used by live check_suite
	// events; the DrainPending… query removes them atomically so a
	// concurrent PR upsert can't double-apply.
	h.replayPendingCheckSuitesForPR(ctx, pr, wsID)

	workspaceID := uuidToString(wsID)
	resp := githubPullRequestToResponse(pr)

	// Auto-link: scan title/body/branch for issue identifiers, look them
	// up in this workspace, attach the link rows. Idempotent (ON CONFLICT
	// upserts the close_intent flag — see LinkIssueToPullRequest) so
	// re-firing the webhook doesn't duplicate.
	//
	// RFC MUL-2414 §4.8: the PR mirror upsert above always runs (so re-enabling
	// GitHub features restores history without backfill), but the link rows
	// are a "new side-effect" and must be gated by the workspace's auto-link
	// flag (which itself short-circuits when the master `github_enabled`
	// switch is off).
	linkedIssueIDs := make([]string, 0)
	if h.workspaceAutoLinkPRsEnabled(ctx, wsID) {
		idents := extractIdentifiers(p.PullRequest.Title, p.PullRequest.Body, p.PullRequest.Head.Ref)
		// closingIdents is the subset of identifiers that this PR explicitly
		// declared via a closing keyword ("Closes/Fixes/Resolves MUL-X").
		// Linking still happens for every mention (idents above), but the
		// link row's close_intent column — and therefore whether the
		// auto-advance gate eventually fires — is only set for keyword-
		// declared identifiers. Bare title prefixes and branch-name
		// references are link-only.
		closingIdents := map[string]struct{}{}
		for _, c := range extractClosingIdentifiers(p.PullRequest.Title, p.PullRequest.Body) {
			closingIdents[c] = struct{}{}
		}
		// qualifyingIdents are the identifiers that genuinely tie this PR to an
		// issue: a title prefix, a branch-name reference, or a body closing
		// keyword. Any identifier that is linked but NOT in this set was matched
		// only by a bare mention in the PR body ("Related MUL-1", "Follow up in
		// MUL-1"). Those links are still recorded (auto-link stays generous so
		// close_intent can be tracked across edits) but are flagged
		// reference_only and hidden from the issue's PR list — a passing mention
		// should not surface the PR as a working PR for that issue (MUL-3739).
		qualifyingIdents := map[string]struct{}{}
		for _, id := range extractIdentifiers(p.PullRequest.Title, p.PullRequest.Head.Ref) {
			qualifyingIdents[id] = struct{}{}
		}
		for c := range closingIdents {
			qualifyingIdents[c] = struct{}{}
		}
		// close_intent should follow the PR title/body while the PR is still
		// editable before its terminal close event. Once GitHub has delivered
		// a terminal event, later edit/synchronize webhooks must not rewrite
		// the merge-time close decision.
		preserveCloseIntent := p.Action != "closed" && (state == "merged" || state == "closed")
		prefix := h.getIssuePrefix(ctx, wsID)
		// reevalIssues collects each issue whose link row we just touched so
		// we can re-run the auto-advance gate against the persisted aggregate
		// after every link upsert in this event. Driving the gate off
		// persisted state (instead of "did *this* webhook declare closing
		// intent?") is what fixes the multi-PR sibling case: a PR with
		// `Closes MUL-1` merges first while a link-only sibling is still
		// open, then the sibling closes later — its webhook has no closing
		// keyword, but the earlier link row carries close_intent=true, so
		// MUL-1 still advances.
		reevalIssues := make([]db.Issue, 0, len(idents))
		for _, id := range idents {
			issue, ok := h.lookupIssueByIdentifier(ctx, wsID, prefix, id)
			if !ok {
				continue
			}
			_, declared := closingIdents[id]
			closeIntent := declared && !preserveCloseIntent
			_, qualifies := qualifyingIdents[id]
			referenceOnly := !qualifies
			if err := h.Queries.LinkIssueToPullRequest(ctx, db.LinkIssueToPullRequestParams{
				IssueID:             issue.ID,
				PullRequestID:       pr.ID,
				CloseIntent:         closeIntent,
				ReferenceOnly:       referenceOnly,
				PreserveCloseIntent: preserveCloseIntent,
				LinkedByType:        strToText("system"),
				LinkedByID:          pgtype.UUID{},
			}); err != nil {
				slog.Warn("github: link failed", "err", err)
				continue
			}
			linkedIssueIDs = append(linkedIssueIDs, uuidToString(issue.ID))
			reevalIssues = append(reevalIssues, issue)
		}

		// A terminal PR event (`merged` or `closed`) may be the moment the
		// last in-flight sibling resolves. We re-evaluate every issue we
		// just linked once both the PR row and the link row are persisted,
		// so the aggregate query sees the freshest state. We advance the
		// issue to done when:
		//   1. the issue isn't already terminal (`done` / `cancelled`);
		//   2. no linked PR is still `open` / `draft`;
		//   3. at least one merged linked PR declared close_intent (a
		//      "Closes/Fixes/Resolves" keyword on its link row).
		// Rule (3) is what prevents "Follow up in MUL-2" / "Unblocks MUL-3"
		// references from being treated the same as "Closes MUL-1", and
		// also prevents an "all closed-without-merge" sequence from
		// silently auto-closing the issue — if nothing carrying closing
		// intent was ever delivered, the user should decide manually.
		if state == "merged" || state == "closed" {
			for _, issue := range reevalIssues {
				if issue.Status == "done" || issue.Status == "cancelled" {
					continue
				}
				counts, err := h.Queries.GetIssuePullRequestCloseAggregate(ctx, issue.ID)
				if err != nil {
					slog.Warn("github: count linked pr states failed", "err", err, "issue_id", uuidToString(issue.ID))
					continue
				}
				if counts.OpenCount == 0 && counts.MergedWithCloseIntentCount > 0 {
					h.advanceIssueToDone(ctx, issue, workspaceID)
				}
			}
		}
	}

	// Broadcast PR change to the workspace so any open issue detail page
	// re-queries its PR list.
	h.publish(protocol.EventPullRequestUpdated, workspaceID, "system", "", map[string]any{
		"pull_request":     resp,
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
// references. We persist all non-terminal actions (`requested`, `rerequested`)
// as well as `completed`: a `requested`/`rerequested` event has status
// `queued`/`in_progress` and an empty conclusion, which the aggregation query
// counts as pending. Without persisting them, the per-PR `checks_pending`
// count stays at 0 while CI is mid-run and the PR card falls through to
// "checks not reported yet" until the first suite finishes.
//
// The suite payload may reference multiple PRs (e.g. the same head SHA is
// open against several base branches), so we iterate. A reference whose PR
// hasn't been mirrored locally is stashed in `github_pending_check_suite`
// and replayed when the matching `pull_request` event upserts the PR row.
func (h *Handler) handleCheckSuiteEvent(ctx context.Context, body []byte) {
	var p ghCheckSuitePayload
	if err := json.Unmarshal(body, &p); err != nil {
		slog.Warn("github: bad check_suite payload", "err", err)
		return
	}
	if p.Installation.ID == 0 {
		return
	}
	insts, err := h.Queries.ListGitHubInstallationsByInstallationID(ctx, p.Installation.ID)
	if err != nil {
		slog.Warn("github: lookup installation failed", "err", err)
		return
	}
	if len(insts) == 0 {
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

	// Fan out to every workspace bound to this installation: each records the
	// suite against its own mirror of the PR (see handlePullRequestEvent /
	// MUL-4343).
	for _, inst := range insts {
		h.recordCheckSuiteForWorkspace(ctx, inst.WorkspaceID, &p, updatedAt)
	}
}

// recordCheckSuiteForWorkspace records a check_suite webhook against one
// workspace's mirror of each referenced PR. A reference whose PR hasn't been
// mirrored in this workspace yet is stashed and replayed when the matching
// pull_request event upserts the row. Invoked once per workspace bound to the
// delivering installation.
func (h *Handler) recordCheckSuiteForWorkspace(ctx context.Context, wsID pgtype.UUID, p *ghCheckSuitePayload, updatedAt pgtype.Timestamptz) {
	affectedIssues := map[string]struct{}{}
	recorded := false
	for _, prRef := range p.CheckSuite.PullRequests {
		// Scope the lookup to the repo's workspace. The (workspace_id,
		// repo_owner, repo_name, pr_number) tuple is the real uniqueness key:
		// a bare (owner, repo, number) lookup could return a row from a
		// different workspace that also tracks this repo and land the suite
		// on the wrong PR.
		pr, err := h.Queries.GetGitHubPullRequest(ctx, db.GetGitHubPullRequestParams{
			WorkspaceID: wsID,
			RepoOwner:   p.Repository.Owner.Login,
			RepoName:    p.Repository.Name,
			PrNumber:    prRef.Number,
		})
		if err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				slog.Warn("github: lookup pr for check_suite failed", "err", err)
				continue
			}
			// Out-of-order delivery: the suite reached us before the
			// `pull_request` webhook that mirrors the PR row. Stash the
			// event keyed by (workspace, repo, pr_number, suite_id); the
			// PR upsert path will drain and replay it.
			if err := h.Queries.UpsertPendingCheckSuite(ctx, db.UpsertPendingCheckSuiteParams{
				WorkspaceID:    wsID,
				InstallationID: p.Installation.ID,
				RepoOwner:      p.Repository.Owner.Login,
				RepoName:       p.Repository.Name,
				PrNumber:       prRef.Number,
				SuiteID:        p.CheckSuite.ID,
				HeadSha:        p.CheckSuite.HeadSHA,
				AppID:          p.CheckSuite.App.ID,
				Conclusion:     strToText(p.CheckSuite.Conclusion),
				Status:         p.CheckSuite.Status,
				SuiteUpdatedAt: updatedAt,
			}); err != nil {
				slog.Warn("github: stash pending check_suite failed",
					"err", err, "suite_id", p.CheckSuite.ID)
			}
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
		recorded = true
		issues, err := h.Queries.ListIssueIDsForPullRequest(ctx, pr.ID)
		if err == nil {
			for _, id := range issues {
				affectedIssues[uuidToString(id)] = struct{}{}
			}
		}
	}

	if !recorded {
		return
	}
	// Broadcast on the existing event so the issue page just re-queries the PR
	// list. We don't pass a single pull_request payload here because a suite can
	// touch several and the listener already invalidates by issue.
	linked := make([]string, 0, len(affectedIssues))
	for id := range affectedIssues {
		linked = append(linked, id)
	}
	h.publish(protocol.EventPullRequestUpdated, uuidToString(wsID), "system", "", map[string]any{
		"linked_issue_ids": linked,
	})
}

// replayPendingCheckSuitesForPR drains the stash table for one PR (any
// rows left there by a check_suite event that arrived before the PR row
// was mirrored) and re-applies each event through the normal upsert
// path. Safe to call on every PR upsert: the drain is a single
// DELETE … RETURNING, so when there is nothing to replay the helper is
// a no-op round-trip.
func (h *Handler) replayPendingCheckSuitesForPR(ctx context.Context, pr db.GithubPullRequest, workspaceID pgtype.UUID) {
	pending, err := h.Queries.DrainPendingCheckSuitesForPR(ctx, db.DrainPendingCheckSuitesForPRParams{
		WorkspaceID: workspaceID,
		RepoOwner:   pr.RepoOwner,
		RepoName:    pr.RepoName,
		PrNumber:    pr.PrNumber,
	})
	if err != nil {
		slog.Warn("github: drain pending check_suites failed",
			"err", err, "pr_id", uuidToString(pr.ID))
		return
	}
	for _, row := range pending {
		if err := h.Queries.UpsertPullRequestCheckSuite(ctx, db.UpsertPullRequestCheckSuiteParams{
			PrID:       pr.ID,
			SuiteID:    row.SuiteID,
			HeadSha:    row.HeadSha,
			AppID:      row.AppID,
			Conclusion: row.Conclusion,
			Status:     row.Status,
			UpdatedAt:  row.SuiteUpdatedAt,
		}); err != nil {
			slog.Warn("github: replay pending check_suite failed",
				"err", err, "pr_id", uuidToString(pr.ID),
				"suite_id", row.SuiteID)
		}
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

// extractClosingIdentifiers pulls every "PREFIX-NUMBER" identifier that
// appears immediately after a GitHub-style closing keyword in the supplied
// fields, deduplicating in input order. Identifiers in branch names are
// intentionally excluded — callers should pass only title and body — because
// branch names are not natural-language fields and treating "mul-1/fix-login"
// as a close declaration would silently re-open the bug this gate is meant
// to fix.
func extractClosingIdentifiers(parts ...string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, src := range parts {
		for _, m := range closingIdentifierRe.FindAllStringSubmatch(src, -1) {
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
// workspaceAutoLinkPRsEnabled reports whether the workspace allows the
// GitHub webhook to create issue ↔ PR link rows. Defaults to true so that
// workspaces predating RFC MUL-2414 keep the historical "auto-link on"
// behavior, and short-circuits to false whenever the master GitHub switch
// is explicitly off — mirroring the precedence used on the client side.
func (h *Handler) workspaceAutoLinkPRsEnabled(ctx context.Context, workspaceID pgtype.UUID) bool {
	ws, err := h.Queries.GetWorkspace(ctx, workspaceID)
	if err != nil || len(ws.Settings) == 0 {
		return true
	}
	var s struct {
		GitHubEnabled            *bool `json:"github_enabled"`
		GitHubAutoLinkPRsEnabled *bool `json:"github_auto_link_prs_enabled"`
	}
	if err := json.Unmarshal(ws.Settings, &s); err != nil {
		return true
	}
	if s.GitHubEnabled != nil && !*s.GitHubEnabled {
		return false
	}
	if s.GitHubAutoLinkPRsEnabled == nil {
		return true
	}
	return *s.GitHubAutoLinkPRsEnabled
}

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
		ID:          issue.ID,
		Status:      "done",
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		slog.Warn("github: advance issue to done failed", "err", err)
		return
	}

	// Fire the platform parent-notification path on the same transition the
	// HTTP UpdateIssue / BatchUpdateIssues paths use. A merged PR is one of
	// the most common ways a sub-issue actually reaches `done`, and skipping
	// it here would leave the parent silent for the dominant completion path.
	// notifyParentOfChildDone re-checks every guard (prev != done, parent
	// exists, parent not terminal), so calling it unconditionally is safe.
	h.notifyParentOfChildDone(ctx, issue, updated)

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
