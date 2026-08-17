package handler

import (
	"bytes"
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
	"regexp"
	"sort"
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
// point App-authenticated calls at an httptest server without touching GitHub.
var githubAPIBase = "https://api.github.com"

// githubOAuthBase is the base URL for GitHub's web OAuth endpoints, which live
// on the website host rather than the API host. Mutable for the same reason as
// githubAPIBase.
var githubOAuthBase = "https://github.com"

const (
	githubReturnToGitHub       = "github"
	githubReturnToRepositories = "repositories"
	githubClaimStatePrefix     = "claim"
	githubAPIResponseLimit     = 4 << 20
)

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
	ID string `json:"id"`
	// Provider is the Git provider this PR was mirrored from: "github", "forgejo",
	// "gitea", or "gitlab". The frontend uses it to pick the host icon and
	// label (e.g. GitLab "merge request").
	Provider        string  `json:"provider"`
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
	// Mergeable state mirrors GitHub's REST `mergeable_state` field, retained
	// for compatibility. The card now reads the richer GraphQL fields below.
	MergeableState *string `json:"mergeable_state"`
	// ── GitHub API snapshot (MUL-5265, Plan C) ──────────────────────────────
	// These come from an authenticated GraphQL query, the single source of
	// truth. All are null / empty / 0 when no snapshot has landed (or the
	// GitHub App private key is unconfigured), so the card hides the CI / merge
	// region and degrades cleanly.
	//
	// Mergeable answers ONLY "is there a conflict": "mergeable" | "conflicting"
	// | "unknown" | null. A false "conflicting" must never be reported as
	// "not mergeable" — that verdict is MergeStateStatus's job.
	Mergeable *string `json:"mergeable"`
	// MergeStateStatus is GitHub's merge-state verdict, lowercased: "clean" |
	// "dirty" | "blocked" | "behind" | "unstable" | "draft" | "has_hooks" |
	// "unknown" | null. "Ready to merge" is derived ONLY from "clean".
	MergeStateStatus *string `json:"merge_state_status"`
	// SnapshotAvailable distinguishes a current API snapshot from both
	// "feature disabled / not fetched yet" and "a current snapshot whose
	// statusCheckRollup is null". Only the last case may render "no checks".
	// It is omitted for non-GitHub providers, which keep their webhook-derived
	// checks_conclusion compatibility path.
	SnapshotAvailable *bool `json:"snapshot_available,omitempty"`
	// ChecksRollup is GitHub's overall CI verdict, lowercased: "success" |
	// "failure" | "pending" | "error" | "expected" | null. null means
	// statusCheckRollup was null (no checks yet) and must NEVER render as
	// passed.
	ChecksRollup *string `json:"checks_rollup"`
	// ChecksConclusion is a coarse compat alias derived from the snapshot:
	// "passed" | "failed" | "pending" | null.
	ChecksConclusion *string `json:"checks_conclusion"`
	// Run-level counts for the PR's snapshot head. ChecksPending mirrors
	// ChecksRunning for older clients that still read the old key.
	ChecksTotal   int64 `json:"checks_total"`
	ChecksPassed  int64 `json:"checks_passed"`
	ChecksFailed  int64 `json:"checks_failed"`
	ChecksRunning int64 `json:"checks_running"`
	ChecksPending int64 `json:"checks_pending"`
	// FailedCheckNames names the failing checks so the card can point at them
	// (e.g. "✗ 2/7 · backend, e2e").
	FailedCheckNames []string `json:"failed_check_names"`
	// SnapshotStale is true when an open PR's last successful fetch is older
	// than the stale threshold (GitHub outage / revoked key): the card shows
	// last-known data greyed out rather than blank.
	SnapshotStale bool `json:"snapshot_stale"`
	// SnapshotFetchedAt is when the snapshot was last fetched (RFC3339), or null.
	SnapshotFetchedAt *string `json:"snapshot_fetched_at"`
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

type GitHubRepositoryResponse struct {
	ID            int64   `json:"id"`
	FullName      string  `json:"full_name"`
	HTMLURL       string  `json:"html_url"`
	CloneURL      string  `json:"clone_url"`
	Description   *string `json:"description"`
	Private       bool    `json:"private"`
	Archived      bool    `json:"archived"`
	DefaultBranch string  `json:"default_branch"`
}

type GitHubRepositoriesResponse struct {
	Repositories []GitHubRepositoryResponse `json:"repositories"`
	TotalCount   int64                      `json:"total_count"`
	NextPage     *int                       `json:"next_page"`
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

func githubPullRequestToResponse(p db.GithubPullRequest, snapshotEnabled bool) GitHubPullRequestResponse {
	snapshotAvailable := currentGitHubSnapshotAvailable(
		snapshotEnabled, p.HeadSha, p.SnapshotHeadSha, p.SnapshotFetchedAt,
	)
	return GitHubPullRequestResponse{
		ID:                uuidToString(p.ID),
		Provider:          "github",
		WorkspaceID:       uuidToString(p.WorkspaceID),
		RepoOwner:         p.RepoOwner,
		RepoName:          p.RepoName,
		Number:            p.PrNumber,
		Title:             p.Title,
		State:             p.State,
		HtmlURL:           p.HtmlUrl,
		Branch:            textToPtr(p.Branch),
		AuthorLogin:       textToPtr(p.AuthorLogin),
		AuthorAvatarURL:   textToPtr(p.AuthorAvatarUrl),
		MergedAt:          timestampToPtr(p.MergedAt),
		ClosedAt:          timestampToPtr(p.ClosedAt),
		PRCreatedAt:       timestampToString(p.PrCreatedAt),
		PRUpdatedAt:       timestampToString(p.PrUpdatedAt),
		MergeableState:    textToPtr(p.MergeableState),
		SnapshotAvailable: &snapshotAvailable,
		// A bare PR row has no aggregated check counts — webhook broadcasts of a
		// single PR fall through here and the frontend re-queries the list for
		// the full snapshot (mergeable / rollup / counts).
		ChecksConclusion: nil,
		FailedCheckNames: []string{},
		Additions:        p.Additions,
		Deletions:        p.Deletions,
		ChangedFiles:     p.ChangedFiles,
	}
}

// prSnapshotStaleThreshold is how old an open PR's last successful fetch may be
// before the card greys it out as stale. Healthy pipelines refresh open PRs at
// least every sweep interval (~10m), so crossing 30m means refreshes are not
// landing (GitHub outage, revoked key) and the shown data is last-known.
const prSnapshotStaleThreshold = 30 * time.Minute

func issuePullRequestRowToResponse(p db.ListPullRequestsByIssueRow, snapshotEnabled bool) GitHubPullRequestResponse {
	snapshotAvailable := currentGitHubSnapshotAvailable(
		snapshotEnabled, p.HeadSha, p.SnapshotHeadSha, p.SnapshotFetchedAt,
	)
	stale := false
	if snapshotAvailable && (p.State == "open" || p.State == "draft") {
		stale = time.Since(p.SnapshotFetchedAt.Time) > prSnapshotStaleThreshold
	}
	failedNames := p.FailedCheckNames
	if failedNames == nil {
		failedNames = []string{}
	}
	resp := GitHubPullRequestResponse{
		ID:                uuidToString(p.ID),
		Provider:          "github",
		WorkspaceID:       uuidToString(p.WorkspaceID),
		RepoOwner:         p.RepoOwner,
		RepoName:          p.RepoName,
		Number:            p.PrNumber,
		Title:             p.Title,
		State:             p.State,
		HtmlURL:           p.HtmlUrl,
		Branch:            textToPtr(p.Branch),
		AuthorLogin:       textToPtr(p.AuthorLogin),
		AuthorAvatarURL:   textToPtr(p.AuthorAvatarUrl),
		MergedAt:          timestampToPtr(p.MergedAt),
		ClosedAt:          timestampToPtr(p.ClosedAt),
		PRCreatedAt:       timestampToString(p.PrCreatedAt),
		PRUpdatedAt:       timestampToString(p.PrUpdatedAt),
		MergeableState:    textToPtr(p.MergeableState),
		SnapshotAvailable: &snapshotAvailable,
		FailedCheckNames:  []string{},
		SnapshotStale:     stale,
		Additions:         p.Additions,
		Deletions:         p.Deletions,
		ChangedFiles:      p.ChangedFiles,
	}
	if snapshotAvailable {
		resp.Mergeable = lowerTextPtr(p.ApiMergeable)
		resp.MergeStateStatus = lowerTextPtr(p.ApiMergeStateStatus)
		resp.ChecksRollup = lowerTextPtr(p.ChecksRollupState)
		resp.ChecksConclusion = rollupToConclusion(p.ChecksRollupState, p.ChecksFailed, p.ChecksRunning, p.ChecksPassed)
		resp.ChecksTotal = p.ChecksTotal
		resp.ChecksPassed = p.ChecksPassed
		resp.ChecksFailed = p.ChecksFailed
		resp.ChecksRunning = p.ChecksRunning
		resp.ChecksPending = p.ChecksRunning
		resp.FailedCheckNames = failedNames
		resp.SnapshotFetchedAt = timestampToPtr(p.SnapshotFetchedAt)
	}
	return resp
}

func currentGitHubSnapshotAvailable(
	enabled bool,
	headSHA string,
	snapshotHeadSHA string,
	fetchedAt pgtype.Timestamptz,
) bool {
	return enabled && fetchedAt.Valid && snapshotHeadSHA != "" && snapshotHeadSHA == headSHA
}

// aggregateChecksConclusion collapses per-PR commit-status counts into a
// single coarse status. Still used by the self-hosted VCS provider path
// (Forgejo / Gitea / GitLab), which mirrors commit statuses via webhook rather
// than fetching a GitHub-style API snapshot:
//   - any failed status wins ("failed");
//   - any not-yet-completed status makes the PR "pending";
//   - all completed and passed is "passed";
//   - no observed status at all is nil (rendered as "no checks" / hidden).
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

// lowerTextPtr returns a lowercased *string for a non-empty pgtype.Text, else
// nil. Used to expose GraphQL enums (MERGEABLE / CLEAN / SUCCESS …) to the API
// in the project's lowercase convention.
func lowerTextPtr(t pgtype.Text) *string {
	if !t.Valid || t.String == "" {
		return nil
	}
	v := strings.ToLower(t.String)
	return &v
}

// rollupToConclusion derives the coarse compat "checks_conclusion" from the
// GraphQL rollup, falling back to the run counts when the rollup enum is
// unfamiliar. A null/empty rollup means "no checks yet" → nil (never "passed").
func rollupToConclusion(rollup pgtype.Text, failed, running, passed int64) *string {
	if !rollup.Valid || rollup.String == "" {
		return nil
	}
	var v string
	switch strings.ToUpper(rollup.String) {
	case "FAILURE", "ERROR":
		v = "failed"
	case "PENDING", "EXPECTED":
		v = "pending"
	case "SUCCESS":
		v = "passed"
	default:
		switch {
		case failed > 0:
			v = "failed"
		case running > 0:
			v = "pending"
		case passed > 0:
			v = "passed"
		default:
			return nil
		}
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

// githubAppClientID / githubAppClientSecret are the App's user-authorization
// (user-to-server OAuth) credentials, found next to the App ID on the App's
// settings page. They exist for exactly one purpose here: proving at install
// time that whoever completed the flow actually controls the installation
// they are about to bind — see verifyGitHubInstallationOwnership.
func githubAppClientID() string { return strings.TrimSpace(os.Getenv("GITHUB_APP_CLIENT_ID")) }

func githubAppClientSecret() string {
	return strings.TrimSpace(os.Getenv("GITHUB_APP_CLIENT_SECRET"))
}

// isGitHubConfigured returns true only when the install slug, the webhook
// secret, AND the user-authorization credentials are all set. The Connect
// button uses this single flag, so the frontend never offers a flow that the
// backend would reject — and the setup callback rejects every install it
// cannot attribute to a GitHub user, so a deployment without the OAuth
// credentials cannot complete a connect at all.
func isGitHubConfigured() bool {
	return githubAppSlug() != "" &&
		githubWebhookSecret() != "" &&
		githubAppClientID() != "" &&
		githubAppClientSecret() != ""
}

// isGitHubRepositoryBrowseConfigured is deliberately separate from the
// install-flow flag. The install-flow credentials are enough to connect an
// installation, but browsing its repositories also requires App JWT
// credentials so the server can mint a short-lived installation token.
func isGitHubRepositoryBrowseConfigured() bool {
	return strings.TrimSpace(os.Getenv("GITHUB_APP_ID")) != "" &&
		strings.TrimSpace(os.Getenv("GITHUB_APP_PRIVATE_KEY")) != ""
}

// githubStateMaxAge bounds how long a signed install state stays usable. The
// state is a bearer credential for "bind an installation into THIS workspace",
// so it should outlive an unhurried GitHub install and nothing more. A leaked
// install URL (browser history, referrer, a pasted link) stops being useful
// once it expires.
const githubStateMaxAge = 15 * time.Minute

// githubStateClockSkew tolerates a state signed by a replica whose clock runs
// slightly ahead of the one verifying it.
const githubStateClockSkew = time.Minute

// signState produces an opaque token that binds a workspace ID to the
// install flow so the setup callback can recover the workspace without
// trusting query params alone.
// Format: "<workspaceID>.<returnTo>.<issuedAtUnix>.<nonce>.<sigHex>".
func signState(workspaceID string) (string, error) {
	return signStateForReturn(workspaceID, githubReturnToGitHub)
}

func signStateForReturn(workspaceID, returnTo string) (string, error) {
	return signStateForReturnAt(workspaceID, returnTo, time.Now())
}

// signStateForReturnAt takes `now` so tests can produce an aged token.
func signStateForReturnAt(workspaceID, returnTo string, now time.Time) (string, error) {
	secret := githubWebhookSecret()
	if secret == "" {
		return "", errors.New("github integration is not configured")
	}
	if !isAllowedGitHubReturnTo(returnTo) {
		return "", errors.New("invalid github return target")
	}
	nonceBytes := make([]byte, 12)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", err
	}
	payload := strings.Join([]string{
		workspaceID,
		returnTo,
		strconv.FormatInt(now.Unix(), 10),
		hex.EncodeToString(nonceBytes),
	}, ".")
	return payload + "." + signGitHubState(secret, payload), nil
}

// signGitHubClaimState seals the account login selected by the workspace
// administrator into a separate OAuth flow. Unlike the installation URL,
// this flow does not trust an installation_id from the browser: the callback
// resolves the sealed account against GitHub's user-authenticated installation
// list before binding anything to the workspace.
// Format: "claim.<workspaceID>.<returnTo>.<accountB64>.<issuedAtUnix>.<nonce>.<sigHex>".
func signGitHubClaimState(workspaceID, returnTo, accountLogin string) (string, error) {
	return signGitHubClaimStateAt(workspaceID, returnTo, accountLogin, time.Now())
}

func signGitHubClaimStateAt(workspaceID, returnTo, accountLogin string, now time.Time) (string, error) {
	secret := githubWebhookSecret()
	if secret == "" {
		return "", errors.New("github integration is not configured")
	}
	if !isAllowedGitHubReturnTo(returnTo) {
		return "", errors.New("invalid github return target")
	}
	accountLogin = strings.ToLower(strings.TrimSpace(accountLogin))
	if !isValidGitHubAccountLogin(accountLogin) {
		return "", errors.New("invalid github account login")
	}
	nonceBytes := make([]byte, 12)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", err
	}
	payload := strings.Join([]string{
		githubClaimStatePrefix,
		workspaceID,
		returnTo,
		base64.RawURLEncoding.EncodeToString([]byte(accountLogin)),
		strconv.FormatInt(now.Unix(), 10),
		hex.EncodeToString(nonceBytes),
	}, ".")
	return payload + "." + signGitHubState(secret, payload), nil
}

func signGitHubState(secret, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

func verifyState(token string) (string, bool) {
	workspaceID, _, ok := verifyStateWithReturn(token)
	return workspaceID, ok
}

func verifyStateWithReturn(token string) (workspaceID, returnTo string, ok bool) {
	return verifyStateWithReturnAt(token, time.Now())
}

func verifyStateWithReturnAt(token string, now time.Time) (workspaceID, returnTo string, ok bool) {
	secret := githubWebhookSecret()
	if secret == "" {
		return "", "", false
	}
	parts := strings.Split(token, ".")
	if len(parts) != 5 {
		return "", "", false
	}
	workspaceID, returnTo = parts[0], parts[1]
	if !isAllowedGitHubReturnTo(returnTo) {
		return "", "", false
	}
	payload := strings.Join(parts[:4], ".")
	if !hmac.Equal([]byte(signGitHubState(secret, payload)), []byte(parts[4])) {
		return "", "", false
	}
	issuedAt, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return "", "", false
	}
	age := now.Sub(time.Unix(issuedAt, 0))
	if age > githubStateMaxAge || age < -githubStateClockSkew {
		return "", "", false
	}
	return workspaceID, returnTo, true
}

func verifyGitHubClaimState(token string) (workspaceID, returnTo, accountLogin string, ok bool) {
	return verifyGitHubClaimStateAt(token, time.Now())
}

func verifyGitHubClaimStateAt(token string, now time.Time) (workspaceID, returnTo, accountLogin string, ok bool) {
	secret := githubWebhookSecret()
	if secret == "" {
		return "", "", "", false
	}
	parts := strings.Split(token, ".")
	if len(parts) != 7 || parts[0] != githubClaimStatePrefix {
		return "", "", "", false
	}
	payload := strings.Join(parts[:6], ".")
	if !hmac.Equal([]byte(signGitHubState(secret, payload)), []byte(parts[6])) {
		return "", "", "", false
	}
	workspaceID, returnTo = parts[1], parts[2]
	if !isAllowedGitHubReturnTo(returnTo) {
		return "", "", "", false
	}
	accountBytes, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil {
		return "", "", "", false
	}
	accountLogin = string(accountBytes)
	if !isValidGitHubAccountLogin(accountLogin) {
		return "", "", "", false
	}
	issuedAt, err := strconv.ParseInt(parts[4], 10, 64)
	if err != nil {
		return "", "", "", false
	}
	age := now.Sub(time.Unix(issuedAt, 0))
	if age > githubStateMaxAge || age < -githubStateClockSkew {
		return "", "", "", false
	}
	return workspaceID, returnTo, accountLogin, true
}

var githubAccountLoginRe = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,37}[a-z0-9])?$`)

func isValidGitHubAccountLogin(accountLogin string) bool {
	return githubAccountLoginRe.MatchString(strings.ToLower(strings.TrimSpace(accountLogin)))
}

func isAllowedGitHubReturnTo(returnTo string) bool {
	return returnTo == githubReturnToGitHub || returnTo == githubReturnToRepositories
}

func githubSettingsURL(frontend, returnTo string) string {
	if !isAllowedGitHubReturnTo(returnTo) {
		returnTo = githubReturnToGitHub
	}
	return strings.TrimRight(frontend, "/") + "/settings?tab=" + url.QueryEscape(returnTo)
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
	returnTo := strings.TrimSpace(r.URL.Query().Get("return_to"))
	if returnTo == "" {
		returnTo = githubReturnToGitHub
	}
	if !isAllowedGitHubReturnTo(returnTo) {
		writeError(w, http.StatusBadRequest, "invalid return target")
		return
	}
	slug := githubAppSlug()
	state, err := signStateForReturn(workspaceID, returnTo)
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

// GitHubClaim returns a user-authorization URL for binding a GitHub App
// installation that already exists on the named account. The account is
// sealed into state here, while the authenticated callback proves GitHub
// lists that exact account among the user's installations.
func (h *Handler) GitHubClaim(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	if _, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id"); !ok {
		return
	}
	if !isGitHubConfigured() {
		writeJSON(w, http.StatusOK, GitHubConnectResponse{Configured: false})
		return
	}
	accountLogin := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("account")))
	if !isValidGitHubAccountLogin(accountLogin) {
		writeError(w, http.StatusBadRequest, "invalid github account login")
		return
	}
	returnTo := strings.TrimSpace(r.URL.Query().Get("return_to"))
	if returnTo == "" {
		returnTo = githubReturnToGitHub
	}
	if !isAllowedGitHubReturnTo(returnTo) {
		writeError(w, http.StatusBadRequest, "invalid return target")
		return
	}
	state, err := signGitHubClaimState(workspaceID, returnTo, accountLogin)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to sign state")
		return
	}
	query := url.Values{}
	query.Set("client_id", githubAppClientID())
	query.Set("state", state)
	oauthURL := strings.TrimRight(githubOAuthBase, "/") + "/login/oauth/authorize?" + query.Encode()
	writeJSON(w, http.StatusOK, GitHubConnectResponse{URL: oauthURL, Configured: true})
}

// GitHubSetupCallback (GET /api/github/setup) handles the redirect GitHub
// sends after a user installs (or re-authorizes) the App. A fresh install
// carries installation_id plus a user-authorization code. The explicit claim
// flow for an already-installed account carries only code and a claim state.
// We prove the installation belongs to the person finishing the flow, persist
// the installation row (workspace ↔ installation_id mapping), then bounce the
// user back to the new Settings → GitHub tab in the web app (RFC MUL-2414
// §4.1). The previous destination was the catch-all Settings page, which
// after the GitHub-tab split would land users on the default profile tab
// instead of the place that shows the connection they just completed.
//
// The route is public (it is a browser redirect from github.com, so it
// carries no Multica credential we can rely on) and both `installation_id`
// and `code` are supplied by the caller. The state token authorizes only the
// *workspace* half of the mapping; the *installation* half is authorized by
// verifyGitHubInstallationOwnership below.
func (h *Handler) GitHubSetupCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	installationIDStr := q.Get("installation_id")
	state := q.Get("state")
	frontend := strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN"))
	if frontend == "" {
		frontend = "http://localhost:3000"
	}
	settingsURL := githubSettingsURL(frontend, githubReturnToGitHub)

	if state == "" {
		http.Redirect(w, r, settingsURL+"&github_error=missing_params", http.StatusFound)
		return
	}
	workspaceID, returnTo, claimAccount, claimStateOK := verifyGitHubClaimState(state)
	installStateOK := false
	if !claimStateOK {
		workspaceID, returnTo, installStateOK = verifyStateWithReturn(state)
	}
	if !claimStateOK && !installStateOK {
		http.Redirect(w, r, settingsURL+"&github_error=invalid_state", http.StatusFound)
		return
	}
	settingsURL = githubSettingsURL(frontend, returnTo)
	wsUUID, err := parseStrictUUID(workspaceID)
	if err != nil {
		http.Redirect(w, r, settingsURL+"&github_error=bad_workspace", http.StatusFound)
		return
	}

	var installationID int64
	var login, accountType string
	var avatar *string
	if claimStateOK {
		claimed, err := resolveGitHubInstallationForAccount(r.Context(), q.Get("code"), claimAccount)
		if err != nil {
			slog.Warn("github: refused existing installation claim",
				"err", err,
				"account_login", claimAccount,
				"workspace_id", workspaceID,
			)
			http.Redirect(w, r, settingsURL+"&github_error="+githubOwnershipErrorCode(err), http.StatusFound)
			return
		}
		installationID = claimed.ID
		login = claimed.Account.Login
		accountType = coalesce(claimed.Account.Type, "User")
		if claimed.Account.AvatarURL != "" {
			avatar = &claimed.Account.AvatarURL
		}
	} else {
		if installationIDStr == "" {
			http.Redirect(w, r, settingsURL+"&github_error=missing_params", http.StatusFound)
			return
		}
		installationID, err = strconv.ParseInt(installationIDStr, 10, 64)
		if err != nil {
			http.Redirect(w, r, settingsURL+"&github_error=bad_installation_id", http.StatusFound)
			return
		}
		// Ownership proof, before ANY persistence: without it the numeric
		// installation_id in the query is enough to bind an unrelated org's
		// installation into this workspace, which leaks that org's private
		// repository names and its whole PR event stream (MUL-6056).
		//
		// A callback for a binding this workspace already has grants nothing new,
		// so it skips the proof: GitHub only issues a user-authorization code when
		// the user authorizes the App, and the "redirect on update" callback that
		// fires when someone edits an existing installation's repository selection
		// carries none. Requiring one there would fail every update.
		if !h.workspaceHasGitHubInstallation(r.Context(), wsUUID, installationID) {
			if err := verifyGitHubInstallationOwnership(r.Context(), q.Get("code"), installationID); err != nil {
				slog.Warn("github: refused unverified installation bind",
					"err", err,
					"installation_id", installationID,
					"workspace_id", workspaceID,
				)
				http.Redirect(w, r, settingsURL+"&github_error="+githubOwnershipErrorCode(err), http.StatusFound)
				return
			}
		}

		// Resolve the installation against GitHub's API to capture display info.
		// If the App auth is not configured we still create the row with the
		// minimum we know; webhook events will refresh it as soon as one fires.
		login, accountType, avatar = fetchInstallationAccount(r.Context(), installationID)
	}

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
	redirectURL := settingsURL + "&github_connected=1"
	if returnTo == githubReturnToRepositories {
		redirectURL += "&github_installation=" + url.QueryEscape(uuidToString(inst.ID))
	}
	http.Redirect(w, r, redirectURL, http.StatusFound)
}

// ── Install ownership proof ─────────────────────────────────────────────────

// workspaceHasGitHubInstallation reports whether this workspace is already
// bound to this installation. Fail-closed: a lookup error answers "no", which
// costs a legitimate update the ownership proof it cannot produce, and that is
// the right way to be wrong.
func (h *Handler) workspaceHasGitHubInstallation(
	ctx context.Context,
	workspaceID pgtype.UUID,
	installationID int64,
) bool {
	rows, err := h.Queries.ListGitHubInstallationsByInstallationID(ctx, installationID)
	if err != nil {
		slog.Warn("github: existing installation lookup failed", "err", err, "installation_id", installationID)
		return false
	}
	for _, row := range rows {
		if row.WorkspaceID == workspaceID {
			return true
		}
	}
	return false
}

var (
	// errGitHubUserAuthorizationMissing means the setup redirect carried no
	// user-authorization `code`. Almost always operator-actionable: the App
	// needs "Request user authorization (OAuth) during installation" enabled.
	errGitHubUserAuthorizationMissing = errors.New("github: setup callback carried no user authorization code")
	// errGitHubInstallationNotAuthorized means GitHub told us this user
	// cannot see the installation they asked us to bind. This is the attack
	// signature, not a misconfiguration.
	errGitHubInstallationNotAuthorized        = errors.New("github: installation is not accessible to the installing user")
	errGitHubInstallationAccountNotAuthorized = errors.New("github: no accessible installation matches the requested account")
)

const (
	githubUserInstallationsPageSize = 100
	githubUserAuthorizationTimeout  = 30 * time.Second
	// githubUserInstallationsMaxPages caps the walk at 2000 installations so a
	// hostile or pathological account cannot hold the callback open forever.
	githubUserInstallationsMaxPages = 20
)

// verifyGitHubInstallationOwnership proves the person who completed the
// install actually controls `installationID`.
//
// Everything the setup callback receives is caller-controlled: the numeric
// installation id is a plain query param, and the state token only says which
// workspace to bind into — it never names an installation. The only
// trustworthy source for "does this person control that installation?" is
// GitHub, so we trade the user-authorization code GitHub appends to the setup
// redirect for a user-to-server token and ask GitHub which installations that
// user can actually see.
//
// This is deliberately fail-closed: no code, a refused exchange, or an
// unreachable API all abort the bind. Falling through on error would restore
// the exact hole this closes, and isGitHubConfigured() already hides Connect
// on deployments that cannot run this check.
func verifyGitHubInstallationOwnership(ctx context.Context, code string, installationID int64) error {
	clientID, clientSecret := githubAppClientID(), githubAppClientSecret()
	if clientID == "" || clientSecret == "" {
		return errors.New("github: user authorization credentials are not configured")
	}
	if strings.TrimSpace(code) == "" {
		return errGitHubUserAuthorizationMissing
	}
	ctx, cancel := context.WithTimeout(ctx, githubUserAuthorizationTimeout)
	defer cancel()
	client := &http.Client{Timeout: 15 * time.Second}
	userToken, err := exchangeGitHubUserCode(ctx, client, clientID, clientSecret, code)
	if err != nil {
		return err
	}
	// The token never leaves this function and is never stored; revoking it
	// mirrors how installation tokens are handled elsewhere in this file.
	defer revokeGitHubUserToken(client, clientID, clientSecret, userToken)
	return assertUserCanAccessInstallation(ctx, client, userToken, installationID)
}

type githubUserInstallation struct {
	ID      int64 `json:"id"`
	Account struct {
		Login     string `json:"login"`
		Type      string `json:"type"`
		AvatarURL string `json:"avatar_url"`
	} `json:"account"`
}

func resolveGitHubInstallationForAccount(
	ctx context.Context,
	code, accountLogin string,
) (githubUserInstallation, error) {
	clientID, clientSecret := githubAppClientID(), githubAppClientSecret()
	if clientID == "" || clientSecret == "" {
		return githubUserInstallation{}, errors.New("github: user authorization credentials are not configured")
	}
	if strings.TrimSpace(code) == "" {
		return githubUserInstallation{}, errGitHubUserAuthorizationMissing
	}
	ctx, cancel := context.WithTimeout(ctx, githubUserAuthorizationTimeout)
	defer cancel()
	client := &http.Client{Timeout: 15 * time.Second}
	userToken, err := exchangeGitHubUserCode(ctx, client, clientID, clientSecret, code)
	if err != nil {
		return githubUserInstallation{}, err
	}
	defer revokeGitHubUserToken(client, clientID, clientSecret, userToken)
	installations, err := listGitHubUserInstallations(ctx, client, userToken)
	if err != nil {
		return githubUserInstallation{}, err
	}
	for _, installation := range installations {
		if strings.EqualFold(installation.Account.Login, accountLogin) {
			return installation, nil
		}
	}
	return githubUserInstallation{}, errGitHubInstallationAccountNotAuthorized
}

// githubOwnershipErrorCode maps a verification failure to the `github_error`
// code carried back to Settings. Account-claim failures get actionable UI;
// the remaining distinctions help operators diagnose logs and support cases.
func githubOwnershipErrorCode(err error) string {
	switch {
	case errors.Is(err, errGitHubInstallationNotAuthorized):
		return "installation_not_authorized"
	case errors.Is(err, errGitHubInstallationAccountNotAuthorized):
		return "installation_account_not_authorized"
	case errors.Is(err, errGitHubUserAuthorizationMissing):
		return "missing_authorization"
	default:
		return "verification_failed"
	}
}

// exchangeGitHubUserCode trades the setup redirect's `code` for a
// user-to-server access token. GitHub answers 200 with an `error` field for a
// bad or replayed code, so the body is checked as well as the status.
func exchangeGitHubUserCode(
	ctx context.Context,
	client *http.Client,
	clientID, clientSecret, code string,
) (string, error) {
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	form.Set("code", code)
	endpoint := strings.TrimRight(githubOAuthBase, "/") + "/login/oauth/access_token"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("exchange github user code: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, githubAPIResponseLimit))
		return "", fmt.Errorf("exchange github user code: github status %d", resp.StatusCode)
	}
	var body struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, githubAPIResponseLimit)).Decode(&body); err != nil {
		return "", fmt.Errorf("decode github user token: %w", err)
	}
	if body.Error != "" {
		return "", fmt.Errorf("exchange github user code: %s", body.Error)
	}
	if body.AccessToken == "" {
		return "", errors.New("github returned an empty user access token")
	}
	return body.AccessToken, nil
}

// assertUserCanAccessInstallation returns nil only when GitHub lists
// installationID among the installations the token's user can reach.
func assertUserCanAccessInstallation(
	ctx context.Context,
	client *http.Client,
	userToken string,
	installationID int64,
) error {
	installations, err := listGitHubUserInstallations(ctx, client, userToken)
	if err != nil {
		return err
	}
	for _, installation := range installations {
		if installation.ID == installationID {
			return nil
		}
	}
	return errGitHubInstallationNotAuthorized
}

func listGitHubUserInstallations(
	ctx context.Context,
	client *http.Client,
	userToken string,
) ([]githubUserInstallation, error) {
	installations := make([]githubUserInstallation, 0)
	for page := 1; page <= githubUserInstallationsMaxPages; page++ {
		endpoint := fmt.Sprintf(
			"%s/user/installations?per_page=%d&page=%d",
			strings.TrimRight(githubAPIBase, "/"),
			githubUserInstallationsPageSize,
			page,
		)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		setGitHubAPIHeaders(req, userToken)
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("list user installations: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, githubAPIResponseLimit))
			resp.Body.Close()
			return nil, fmt.Errorf("list user installations: github status %d", resp.StatusCode)
		}
		var body struct {
			Installations []githubUserInstallation `json:"installations"`
		}
		decodeErr := json.NewDecoder(io.LimitReader(resp.Body, githubAPIResponseLimit)).Decode(&body)
		resp.Body.Close()
		if decodeErr != nil {
			return nil, fmt.Errorf("decode user installations: %w", decodeErr)
		}
		installations = append(installations, body.Installations...)
		if len(body.Installations) < githubUserInstallationsPageSize {
			break
		}
	}
	return installations, nil
}

// revokeGitHubUserToken drops the short-lived user token as soon as the
// ownership check is done. Best-effort: the token is never persisted, so a
// failure here only means it lives out its natural expiry.
func revokeGitHubUserToken(client *http.Client, clientID, clientSecret, token string) {
	if token == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	endpoint := fmt.Sprintf(
		"%s/applications/%s/token",
		strings.TrimRight(githubAPIBase, "/"),
		url.PathEscape(clientID),
	)
	payload, err := json.Marshal(map[string]string{"access_token": token})
	if err != nil {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, bytes.NewReader(payload))
	if err != nil {
		return
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(clientID, clientSecret)
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, githubAPIResponseLimit))
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
		"installations":                out,
		"configured":                   isGitHubConfigured(),
		"repository_browse_configured": isGitHubRepositoryBrowseConfigured(),
		"can_manage":                   canManage,
	})
}

// ListGitHubInstallationRepositories returns the repositories accessible to a
// workspace-bound GitHub App installation. The route is admin-only because
// private repository names are sensitive. The path takes our installation row
// UUID (not GitHub's numeric installation id), and the workspace ownership
// check happens before any GitHub API call.
func (h *Handler) ListGitHubInstallationRepositories(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	if _, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id"); !ok {
		return
	}
	installationRowID := chi.URLParam(r, "installationId")
	rowUUID, ok := parseUUIDOrBadRequest(w, installationRowID, "installation id")
	if !ok {
		return
	}
	row, err := h.Queries.GetGitHubInstallationByID(r.Context(), rowUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "github installation not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load github installation")
		return
	}
	if uuidToString(row.WorkspaceID) != workspaceID {
		writeError(w, http.StatusNotFound, "github installation not found")
		return
	}
	if !isGitHubRepositoryBrowseConfigured() {
		writeError(w, http.StatusServiceUnavailable, "github repository browsing is not configured")
		return
	}
	page, ok := parseGitHubPageParam(w, r, "page", 1, 1, 100000)
	if !ok {
		return
	}
	perPage, ok := parseGitHubPageParam(w, r, "per_page", 100, 1, 100)
	if !ok {
		return
	}

	repositories, err := fetchGitHubInstallationRepositories(
		r.Context(),
		row.InstallationID,
		page,
		perPage,
	)
	if err != nil {
		slog.Warn("github: list installation repositories failed", "err", err)
		writeError(w, http.StatusBadGateway, "failed to list github repositories")
		return
	}
	writeJSON(w, http.StatusOK, repositories)
}

func parseGitHubPageParam(
	w http.ResponseWriter,
	r *http.Request,
	name string,
	defaultValue, minValue, maxValue int,
) (int, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return defaultValue, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < minValue || value > maxValue {
		writeError(w, http.StatusBadRequest, "invalid "+name)
		return 0, false
	}
	return value, true
}

func fetchGitHubInstallationRepositories(
	ctx context.Context,
	installationID int64,
	page, perPage int,
) (GitHubRepositoriesResponse, error) {
	appJWT, err := signGitHubAppJWT(time.Now())
	if err != nil {
		return GitHubRepositoriesResponse{}, err
	}
	if appJWT == "" {
		return GitHubRepositoriesResponse{}, errors.New("github App JWT credentials unavailable")
	}

	client := &http.Client{Timeout: 15 * time.Second}
	tokenEndpoint := fmt.Sprintf(
		"%s/app/installations/%d/access_tokens",
		strings.TrimRight(githubAPIBase, "/"),
		installationID,
	)
	tokenReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		tokenEndpoint,
		strings.NewReader(`{"permissions":{"metadata":"read"}}`),
	)
	if err != nil {
		return GitHubRepositoriesResponse{}, err
	}
	setGitHubAPIHeaders(tokenReq, appJWT)
	tokenReq.Header.Set("Content-Type", "application/json")
	tokenResp, err := client.Do(tokenReq)
	if err != nil {
		return GitHubRepositoriesResponse{}, fmt.Errorf("create installation token: %w", err)
	}
	defer tokenResp.Body.Close()
	if tokenResp.StatusCode != http.StatusCreated {
		_, _ = io.Copy(io.Discard, io.LimitReader(tokenResp.Body, githubAPIResponseLimit))
		return GitHubRepositoriesResponse{}, fmt.Errorf("create installation token: github status %d", tokenResp.StatusCode)
	}
	var tokenBody struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(io.LimitReader(tokenResp.Body, githubAPIResponseLimit)).Decode(&tokenBody); err != nil {
		return GitHubRepositoriesResponse{}, fmt.Errorf("decode installation token: %w", err)
	}
	if tokenBody.Token == "" {
		return GitHubRepositoriesResponse{}, errors.New("github returned an empty installation token")
	}
	defer revokeGitHubInstallationToken(client, tokenBody.Token)

	repositoriesEndpoint := fmt.Sprintf(
		"%s/installation/repositories?page=%d&per_page=%d",
		strings.TrimRight(githubAPIBase, "/"),
		page,
		perPage,
	)
	repositoriesReq, err := http.NewRequestWithContext(ctx, http.MethodGet, repositoriesEndpoint, nil)
	if err != nil {
		return GitHubRepositoriesResponse{}, err
	}
	setGitHubAPIHeaders(repositoriesReq, tokenBody.Token)
	repositoriesResp, err := client.Do(repositoriesReq)
	if err != nil {
		return GitHubRepositoriesResponse{}, fmt.Errorf("list installation repositories: %w", err)
	}
	defer repositoriesResp.Body.Close()
	if repositoriesResp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(repositoriesResp.Body, githubAPIResponseLimit))
		return GitHubRepositoriesResponse{}, fmt.Errorf("list installation repositories: github status %d", repositoriesResp.StatusCode)
	}
	var body struct {
		TotalCount   int64 `json:"total_count"`
		Repositories []struct {
			ID            int64   `json:"id"`
			FullName      string  `json:"full_name"`
			HTMLURL       string  `json:"html_url"`
			CloneURL      string  `json:"clone_url"`
			Description   *string `json:"description"`
			Private       bool    `json:"private"`
			Archived      bool    `json:"archived"`
			DefaultBranch string  `json:"default_branch"`
		} `json:"repositories"`
	}
	if err := json.NewDecoder(io.LimitReader(repositoriesResp.Body, githubAPIResponseLimit)).Decode(&body); err != nil {
		return GitHubRepositoriesResponse{}, fmt.Errorf("decode installation repositories: %w", err)
	}
	out := GitHubRepositoriesResponse{
		Repositories: make([]GitHubRepositoryResponse, 0, len(body.Repositories)),
		TotalCount:   body.TotalCount,
	}
	for _, repository := range body.Repositories {
		out.Repositories = append(out.Repositories, GitHubRepositoryResponse(repository))
	}
	if int64(page*perPage) < body.TotalCount {
		nextPage := page + 1
		out.NextPage = &nextPage
	}
	return out, nil
}

func setGitHubAPIHeaders(req *http.Request, token string) {
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
}

func revokeGitHubInstallationToken(client *http.Client, token string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	endpoint := strings.TrimRight(githubAPIBase, "/") + "/installation/token"
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return
	}
	setGitHubAPIHeaders(req, token)
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, githubAPIResponseLimit))
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
		out = append(out, issuePullRequestRowToResponse(row, h.PRRefresh.Enabled()))
		// Page-visit trigger (MUL-5265): if this card's snapshot is missing or
		// older than the view TTL, kick an async refresh. Non-blocking — the
		// current (possibly stale) response is returned immediately and the
		// fresh snapshot arrives via the pull_request:updated realtime event.
		h.PRRefresh.MaybeEnqueueOnView(
			row.InstallationID, row.RepoOwner, row.RepoName, row.PrNumber,
			row.SnapshotFetchedAt.Time,
			row.SnapshotFetchedAt.Valid &&
				row.SnapshotHeadSha != "" &&
				row.SnapshotHeadSha == row.HeadSha,
		)
	}
	// PRs from token-based providers (Forgejo / Gitea / GitLab) share the same
	// card list. They live in their own provider-tagged tables, so they merge
	// in here mapped to the same response shape; the combined list is re-sorted
	// newest-first.
	vcsRows, err := h.Queries.ListVCSPullRequestsByIssue(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list pull requests")
		return
	}
	for _, row := range vcsRows {
		out = append(out, vcsPullRequestRowToResponse(row))
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].PRCreatedAt > out[j].PRCreatedAt
	})
	writeJSON(w, http.StatusOK, map[string]any{"pull_requests": out})
}

// broadcastPRSnapshotApplied is the ghsnapshot pipeline's onApplied callback:
// once an API snapshot is written to a PR row, re-broadcast the PR so every
// open issue detail page re-queries its PR list and picks up the fresh CI /
// mergeability state. Runs on a background pipeline goroutine.
func (h *Handler) broadcastPRSnapshotApplied(ctx context.Context, prID pgtype.UUID) {
	pr, err := h.Queries.GetGitHubPullRequestByID(ctx, prID)
	if err != nil {
		return
	}
	issueIDs, err := h.Queries.ListIssueIDsForPullRequest(ctx, prID)
	if err != nil {
		return
	}
	linked := make([]string, 0, len(issueIDs))
	for _, id := range issueIDs {
		linked = append(linked, uuidToString(id))
	}
	h.publish(protocol.EventPullRequestUpdated, uuidToString(pr.WorkspaceID), "system", "", map[string]any{
		"pull_request":     githubPullRequestToResponse(pr, h.PRRefresh.Enabled()),
		"linked_issue_ids": linked,
	})
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
	case "check_suite", "check_run", "status":
		// CI events are pure triggers under Plan C (MUL-5265): their payload is
		// never read for display. Each just asks the API pipeline to re-fetch
		// the authoritative snapshot for the PR(s) it concerns.
		h.triggerPRRefreshFromCIEvent(ctx, body)
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
	//
	// Fanning out means an identifier can resolve in more than one workspace at
	// once (issue prefixes are not globally unique and issue numbers restart at
	// 1 per workspace, so two bound workspaces can both own a real "ABC-100").
	// Settle who — if anyone — may act on a closing keyword BEFORE any workspace
	// links, and treat that verdict as authoritative for the whole delivery, so
	// the mirror pass cannot re-derive a different answer. See closeIntentPolicy.
	closePolicy := h.resolveCloseIntentPolicy(ctx, insts, &p)
	for _, inst := range insts {
		h.mirrorPullRequestForWorkspace(ctx, inst.WorkspaceID, inst.InstallationID, &p, closePolicy)
	}
	// The PR row(s) now carry the new head; ask the API pipeline for the
	// authoritative CI + mergeability snapshot for that head. The webhook is
	// only the doorbell — its own mergeable/checks payload is not used for
	// display anymore (MUL-5265).
	h.PRRefresh.Enqueue(p.Installation.ID, p.Repository.Owner.Login, p.Repository.Name, p.PullRequest.Number)
}

// closeIntentPolicy decides which (closing identifier, workspace) pairs this
// delivery is allowed to act on, and is the single authority for that decision:
// the per-workspace mirror pass consults it instead of re-deriving the answer
// from its own reads.
//
// It is an allowlist, not a denylist, so it fails closed by construction — an
// identifier we could not positively attribute to exactly one workspace is
// simply absent, and absence denies. The zero value therefore permits nothing,
// which is what every indeterminate read returns.
type closeIntentPolicy struct {
	// unrestricted marks the single-binding case: cross-workspace ambiguity is
	// impossible with one bound workspace, so every closing identifier keeps
	// its pre-#6804 behavior and the scan does no reads at all.
	unrestricted bool
	// owner maps a closing identifier to the one workspace proven to resolve
	// it. Recording the winner (rather than a bare "allowed") also closes the
	// window between the scan and the mirror pass: if a second workspace grows
	// a same-numbered issue in between, it is not the recorded owner, so it
	// still cannot act.
	owner map[string]string
}

// permits reports whether workspaceID may carry close intent for identifier.
func (c closeIntentPolicy) permits(identifier, workspaceID string) bool {
	if c.unrestricted {
		return true
	}
	owner, ok := c.owner[identifier]
	return ok && owner == workspaceID
}

// resolveCloseIntentPolicy determines, before any workspace links, which
// closing identifiers on this PR may advance an issue and in which workspace.
//
// Issue prefixes are deliberately not globally unique (#2797) and issue numbers
// restart at 1 in every workspace, so once #5183 fanned PR webhooks out to every
// bound workspace, "Closes ABC-100" could resolve to a genuine — but different —
// issue in each of them. Linking in both is recoverable noise; auto-advancing
// both to done is not, because it silently rewrites the status of an issue in a
// workspace that has nothing to do with this PR (#6804).
//
// An identifier is allowed only when exactly one bound workspace was *proven*
// to resolve it. Anything else — two resolvers, or a read we could not complete
// — leaves it out, so no workspace acts on it. That asymmetry is deliberate:
// misjudging "unique" as "ambiguous" costs an auto-complete a human can perform,
// while misjudging "ambiguous" as "unique" silently closes someone else's issue.
//
// Only closing identifiers are examined, and only when the installation has more
// than one binding, so a PR without a closing keyword — or the overwhelmingly
// common single-workspace installation — does no extra work.
func (h *Handler) resolveCloseIntentPolicy(ctx context.Context, insts []db.GithubInstallation, p *ghPullRequestPayload) closeIntentPolicy {
	if len(insts) < 2 {
		return closeIntentPolicy{unrestricted: true}
	}
	idents := extractClosingIdentifiers(p.PullRequest.Title, p.PullRequest.Body)
	if len(idents) == 0 {
		return closeIntentPolicy{}
	}
	prNumber := p.PullRequest.Number
	repo := p.Repository.Owner.Login + "/" + p.Repository.Name

	// Collect every workspace that resolves each identifier. Any read we cannot
	// complete abandons the whole event: a workspace we failed to inspect might
	// be a second resolver, and without ruling that out we cannot call any other
	// workspace the unique one.
	resolvers := make(map[string][]string, len(idents))
	for _, inst := range insts {
		ws, err := h.Queries.GetWorkspace(ctx, inst.WorkspaceID)
		if err != nil {
			slog.Warn("github: cannot load bound workspace, withholding close intent for this delivery",
				"err", err, "installation_id", p.Installation.ID, "repo", repo, "pr_number", prNumber)
			return closeIntentPolicy{}
		}
		// A workspace with auto-link off never writes a link row, so it is not a
		// competing claimant and must not suppress one that would legitimately
		// act. A settings blob we cannot parse is not evidence of either, so it
		// fails closed rather than defaulting to "enabled".
		autoLink, err := autoLinkPRsEnabledForWorkspace(ws)
		if err != nil {
			slog.Warn("github: cannot read workspace auto-link setting, withholding close intent for this delivery",
				"err", err, "workspace_id", uuidToString(inst.WorkspaceID),
				"installation_id", p.Installation.ID, "repo", repo, "pr_number", prNumber)
			return closeIntentPolicy{}
		}
		if !autoLink {
			continue
		}
		prefix := issuePrefixForWorkspace(ws)
		for _, id := range idents {
			number, ok := issueNumberForPrefix(id, prefix)
			if !ok {
				continue
			}
			if _, err := h.Queries.GetIssueByNumber(ctx, db.GetIssueByNumberParams{
				WorkspaceID: inst.WorkspaceID,
				Number:      number,
			}); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					// Proven miss: this workspace has no such issue.
					continue
				}
				slog.Warn("github: cannot resolve identifier in bound workspace, withholding close intent for this delivery",
					"err", err, "identifier", id, "workspace_id", uuidToString(inst.WorkspaceID),
					"installation_id", p.Installation.ID, "repo", repo, "pr_number", prNumber)
				return closeIntentPolicy{}
			}
			resolvers[id] = append(resolvers[id], uuidToString(inst.WorkspaceID))
		}
	}

	policy := closeIntentPolicy{owner: make(map[string]string, len(resolvers))}
	for id, wss := range resolvers {
		if len(wss) == 1 {
			policy.owner[id] = wss[0]
			continue
		}
		// The symptom this prevents (an issue advancing to done for no visible
		// reason) is otherwise untraceable back to GitHub, so leave a breadcrumb
		// naming the collision an operator has to resolve by changing a prefix.
		slog.Warn("github: ambiguous closing identifier across bound workspaces, withholding close intent",
			"identifier", id,
			"workspaces", len(wss),
			"installation_id", p.Installation.ID,
			"repo", repo,
			"pr_number", prNumber,
		)
	}
	return policy
}

// ghCIEventPayload captures the shared shape of the check_suite / check_run /
// status webhooks — enough to resolve which PR to refresh. These events are
// pure triggers under Plan C: their payload data is never read for display.
type ghCIEventPayload struct {
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
	Repository struct {
		Name  string `json:"name"`
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
	// status events: top-level commit SHA, no PR number.
	SHA        string `json:"sha"`
	CheckSuite struct {
		HeadSHA      string `json:"head_sha"`
		PullRequests []struct {
			Number int32 `json:"number"`
		} `json:"pull_requests"`
	} `json:"check_suite"`
	CheckRun struct {
		PullRequests []struct {
			Number int32 `json:"number"`
		} `json:"pull_requests"`
		CheckSuite struct {
			HeadSHA string `json:"head_sha"`
		} `json:"check_suite"`
	} `json:"check_run"`
}

// triggerPRRefreshFromCIEvent enqueues an API refresh for the PR(s) a
// check_suite / check_run / status webhook concerns. check_suite/check_run
// carry the PR numbers directly; status events carry only a commit SHA, so we
// map it back to the mirrored head_sha to find the PR(s).
func (h *Handler) triggerPRRefreshFromCIEvent(ctx context.Context, body []byte) {
	if !h.PRRefresh.Enabled() {
		return
	}
	var p ghCIEventPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return
	}
	if p.Installation.ID == 0 || p.Repository.Name == "" {
		return
	}
	owner, repo := p.Repository.Owner.Login, p.Repository.Name

	seen := map[int32]struct{}{}
	enqueue := func(number int32) {
		if number == 0 {
			return
		}
		if _, ok := seen[number]; ok {
			return
		}
		seen[number] = struct{}{}
		h.PRRefresh.Enqueue(p.Installation.ID, owner, repo, number)
	}
	for _, pr := range p.CheckSuite.PullRequests {
		enqueue(pr.Number)
	}
	for _, pr := range p.CheckRun.PullRequests {
		enqueue(pr.Number)
	}
	if len(seen) > 0 {
		return
	}
	// No PR number in the payload (status event, or a check event whose
	// pull_requests array was empty) — resolve by head SHA.
	sha := p.SHA
	if sha == "" {
		sha = coalesce(p.CheckSuite.HeadSHA, p.CheckRun.CheckSuite.HeadSHA)
	}
	if sha == "" {
		return
	}
	numbers, err := h.Queries.ListGitHubPRNumbersByHeadSHA(ctx, db.ListGitHubPRNumbersByHeadSHAParams{
		InstallationID: p.Installation.ID,
		RepoOwner:      owner,
		RepoName:       repo,
		HeadSha:        sha,
	})
	if err != nil {
		return
	}
	for _, number := range numbers {
		enqueue(number)
	}
}

// mirrorPullRequestForWorkspace mirrors a pull_request webhook into a single
// workspace: it upserts the PR row, replays any check_suite events that
// arrived before the PR was mirrored, auto-links referenced issues (gated by
// the workspace's github toggles), advances issues on terminal events, and
// broadcasts the change. Invoked once per workspace bound to the delivering
// installation.
//
// closePolicy is the delivery-wide verdict on which closing identifiers this
// workspace may act on; identifiers it does not permit still link, but never
// carry close_intent, so they can never advance an issue to done. This function
// only ever narrows that verdict — it cannot grant close intent the policy
// withheld.
func (h *Handler) mirrorPullRequestForWorkspace(ctx context.Context, wsID pgtype.UUID, installationID int64, p *ghPullRequestPayload, closePolicy closeIntentPolicy) {
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

	workspaceID := uuidToString(wsID)
	resp := githubPullRequestToResponse(pr, h.PRRefresh.Enabled())

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
			if declared && !closePolicy.permits(id, workspaceID) {
				// The delivery-wide scan did not prove this workspace is the one
				// this closing keyword refers to, so it does not act on it.
				// Falling through with declared=false also clears close_intent
				// on a row a pre-#6804 delivery had already set, so a re-fired
				// webhook heals the stored decision.
				declared = false
			}
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
				// Combined across providers: an issue may also carry a still-open
				// self-hosted VCS PR, which must block auto-advance here just as
				// an open GitHub PR blocks it on the VCS webhook path.
				counts, err := h.Queries.GetIssueCombinedPullRequestCloseAggregate(ctx, issue.ID)
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

// autoLinkPRsEnabledForWorkspace reports whether the workspace allows the
// GitHub webhook to create issue ↔ PR link rows. Defaults to true so that
// workspaces predating RFC MUL-2414 keep the historical "auto-link on"
// behavior, and short-circuits to false whenever the master GitHub switch
// is explicitly off — mirroring the precedence used on the client side.
//
// An unparseable settings blob is surfaced as an error instead of being folded
// into the permissive default. Callers deciding whether to write a link row
// keep taking the default, but the close-intent scan has to tell "auto-link is
// on" apart from "we could not find out" — see closeIntentPolicy.
func autoLinkPRsEnabledForWorkspace(ws db.Workspace) (bool, error) {
	if len(ws.Settings) == 0 {
		return true, nil
	}
	var s struct {
		GitHubEnabled            *bool `json:"github_enabled"`
		GitHubAutoLinkPRsEnabled *bool `json:"github_auto_link_prs_enabled"`
	}
	if err := json.Unmarshal(ws.Settings, &s); err != nil {
		return true, err
	}
	if s.GitHubEnabled != nil && !*s.GitHubEnabled {
		return false, nil
	}
	if s.GitHubAutoLinkPRsEnabled == nil {
		return true, nil
	}
	return *s.GitHubAutoLinkPRsEnabled, nil
}

func (h *Handler) workspaceAutoLinkPRsEnabled(ctx context.Context, workspaceID pgtype.UUID) bool {
	ws, err := h.Queries.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return true
	}
	enabled, _ := autoLinkPRsEnabledForWorkspace(ws)
	return enabled
}

// issueNumberForPrefix returns the issue number encoded in a "PREFIX-NUMBER"
// identifier, and false when the identifier is malformed or carries a prefix
// that is not the workspace's. Case-insensitive: branch names are
// conventionally lowercase while issue prefixes are uppercase.
func issueNumberForPrefix(identifier, prefix string) (int32, bool) {
	idx := strings.LastIndex(identifier, "-")
	if idx < 0 {
		return 0, false
	}
	gotPrefix, numStr := identifier[:idx], identifier[idx+1:]
	if !strings.EqualFold(gotPrefix, prefix) {
		return 0, false
	}
	n, err := strconv.Atoi(numStr)
	if err != nil {
		return 0, false
	}
	return int32(n), true
}

// lookupIssueByIdentifier looks up an issue in the given workspace by its
// "PREFIX-NUMBER" identifier. Returns the row + true if the prefix matches
// the workspace's configured prefix and the number resolves to a real issue.
func (h *Handler) lookupIssueByIdentifier(ctx context.Context, workspaceID pgtype.UUID, prefix, identifier string) (db.Issue, bool) {
	number, ok := issueNumberForPrefix(identifier, prefix)
	if !ok {
		return db.Issue{}, false
	}
	issue, err := h.Queries.GetIssueByNumber(ctx, db.GetIssueByNumberParams{
		WorkspaceID: workspaceID,
		Number:      number,
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
