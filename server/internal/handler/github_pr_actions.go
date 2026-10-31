package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/ghuser"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// PR write operations on an issue's linked GitHub pull requests (MUL-8433),
// mounted at /api/issues/{id}/pull-requests/{prId}/... behind
// RequireHumanActor. Every GitHub call is made with the calling member's own
// user token, so GitHub applies that person's repository permissions and
// branch protection. None of these handlers ever answers 401: the web client
// treats any 401 as a Multica session expiry.

const (
	maxReviewersPerRequest    = 15
	maxReviewerCandidates     = 50
	reviewerCandidatePages    = 3
	githubActionsAppSlug      = "github-actions"
	githubPRActionBodyLimit   = 64 << 10
	githubPRActivityMerged    = "pull_request_merged"
	githubPRActivityClosed    = "pull_request_closed"
	githubReauthMessage       = "Your GitHub authorization has expired. Reconnect your GitHub account to continue."
	githubNotConnectedMessage = "Connect your GitHub account to do this from Multica."
)

var githubLoginPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})$`)

var mergeMethods = map[string]string{"merge": "MERGE", "squash": "SQUASH", "rebase": "REBASE"}

// githubPRTarget is a PR the caller may act on, plus the caller's token.
type githubPRTarget struct {
	issue  db.Issue
	pr     db.GithubPullRequest
	userID pgtype.UUID
	token  ghuser.Token
}

func (t githubPRTarget) repoPath() string {
	return "/repos/" + url.PathEscape(t.pr.RepoOwner) + "/" + url.PathEscape(t.pr.RepoName)
}

func (t githubPRTarget) pullPath() string {
	return fmt.Sprintf("%s/pulls/%d", t.repoPath(), t.pr.PrNumber)
}

func (t githubPRTarget) repoVariables() map[string]any {
	return map[string]any{"owner": t.pr.RepoOwner, "name": t.pr.RepoName, "number": t.pr.PrNumber}
}

// loadGitHubPRTarget enforces the shared preconditions: the caller can see the
// issue, the PR is a GitHub PR linked to it (and not explicitly unlinked), the
// repository's installation belongs to this workspace, and the caller has a
// usable GitHub authorization.
func (h *Handler) loadGitHubPRTarget(w http.ResponseWriter, r *http.Request) (githubPRTarget, bool) {
	if isMachineCredentialActor(r) {
		writeError(w, http.StatusForbidden, "this endpoint is only available to human actors")
		return githubPRTarget{}, false
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return githubPRTarget{}, false
	}
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return githubPRTarget{}, false
	}
	prID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "prId"), "pull request id")
	if !ok {
		return githubPRTarget{}, false
	}
	ctx := r.Context()
	pr, err := h.Queries.GetGitHubPullRequestInWorkspace(ctx, db.GetGitHubPullRequestInWorkspaceParams{ID: prID, WorkspaceID: issue.WorkspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		_, vcsErr := h.Queries.GetVCSPullRequestInWorkspace(ctx, db.GetVCSPullRequestInWorkspaceParams{ID: prID, WorkspaceID: issue.WorkspaceID})
		switch {
		case vcsErr == nil:
			writeErrorCode(w, http.StatusUnprocessableEntity, "pull_request_provider_unsupported",
				"This action is only available for GitHub pull requests")
		case errors.Is(vcsErr, pgx.ErrNoRows):
			writeError(w, http.StatusNotFound, "pull request not found")
		default:
			writeError(w, http.StatusInternalServerError, "failed to load pull request")
		}
		return githubPRTarget{}, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load pull request")
		return githubPRTarget{}, false
	}
	linked, err := h.Queries.IsPullRequestLinkedToIssue(ctx, db.IsPullRequestLinkedToIssueParams{IssueID: issue.ID, PullRequestID: pr.ID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load pull request")
		return githubPRTarget{}, false
	}
	if !linked {
		writeError(w, http.StatusNotFound, "pull request is not linked to this issue")
		return githubPRTarget{}, false
	}
	bound, err := h.Queries.IsGitHubInstallationBoundToWorkspace(ctx, db.IsGitHubInstallationBoundToWorkspaceParams{
		WorkspaceID: issue.WorkspaceID, InstallationID: pr.InstallationID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load pull request")
		return githubPRTarget{}, false
	}
	if !bound {
		writeErrorCode(w, http.StatusUnprocessableEntity, "github_installation_missing",
			"This repository's GitHub App installation is no longer connected to the workspace")
		return githubPRTarget{}, false
	}
	if !h.GitHubUser.Configured() {
		writeGitHubUserAuthNotConfigured(w)
		return githubPRTarget{}, false
	}
	uid, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return githubPRTarget{}, false
	}
	token, err := h.GitHubUser.AccessToken(ctx, uid)
	if err != nil {
		writeGitHubUserTokenError(w, err)
		return githubPRTarget{}, false
	}
	return githubPRTarget{issue: issue, pr: pr, userID: uid, token: token}, true
}

func writeGitHubUserTokenError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ghuser.ErrNotConfigured):
		writeGitHubUserAuthNotConfigured(w)
	case errors.Is(err, ghuser.ErrNotConnected):
		writeErrorCode(w, http.StatusConflict, "github_account_not_connected", githubNotConnectedMessage)
	case errors.Is(err, ghuser.ErrReauthRequired):
		writeErrorCode(w, http.StatusConflict, "github_reauth_required", githubReauthMessage)
	default:
		slog.Warn("github pr action: user token unavailable", "err", err)
		writeErrorCode(w, http.StatusBadGateway, "github_request_failed", "Could not renew your GitHub authorization. Try again.")
	}
}

// writeGitHubAPIError maps a failed GitHub call to the error contract. A 401
// from GitHub means the token itself was rejected, so the connection is
// dropped and the user asked to reconnect.
func (h *Handler) writeGitHubAPIError(ctx context.Context, w http.ResponseWriter, t githubPRTarget, err error) {
	var apiErr *ghuser.APIError
	if !errors.As(err, &apiErr) {
		slog.Warn("github pr action: request failed", "repo", t.pr.RepoOwner+"/"+t.pr.RepoName, "number", t.pr.PrNumber, "err", err)
		writeErrorCode(w, http.StatusBadGateway, "github_request_failed", "Could not reach GitHub. Try again.")
		return
	}
	switch {
	case apiErr.RateLimited:
		if apiErr.RetryAfter > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(apiErr.RetryAfter.Seconds()))))
		}
		writeErrorCode(w, http.StatusTooManyRequests, "github_rate_limited", "GitHub's rate limit was reached. Try again later.")
	case apiErr.Status == http.StatusUnauthorized:
		if err := h.GitHubUser.Forget(context.WithoutCancel(ctx), t.userID); err != nil {
			slog.Warn("github pr action: failed to drop rejected authorization", "err", err)
		}
		writeErrorCode(w, http.StatusConflict, "github_reauth_required", githubReauthMessage)
	case isGitHubIntegrationPermissionError(apiErr):
		writeErrorCode(w, http.StatusForbidden, "github_app_permission_missing",
			"The Multica GitHub App doesn't have write access to this repository yet. An owner of the GitHub installation has to approve the updated permissions on GitHub.")
	case apiErr.Status == http.StatusForbidden || apiErr.Status == http.StatusNotFound ||
		apiErr.GraphQLType == "FORBIDDEN" || apiErr.GraphQLType == "NOT_FOUND":
		writeErrorCode(w, http.StatusForbidden, "github_forbidden",
			githubMessageOr(apiErr, "Your GitHub account doesn't have permission to do this in this repository."))
	case apiErr.Status == http.StatusUnprocessableEntity || apiErr.GraphQLType == "UNPROCESSABLE":
		writeErrorCode(w, http.StatusUnprocessableEntity, "github_request_failed", githubMessageOr(apiErr, "GitHub rejected the request."))
	default:
		slog.Warn("github pr action: unexpected github response", "repo", t.pr.RepoOwner+"/"+t.pr.RepoName, "number", t.pr.PrNumber, "err", err)
		writeErrorCode(w, http.StatusBadGateway, "github_request_failed", "GitHub returned an unexpected error. Try again.")
	}
}

func isGitHubIntegrationPermissionError(e *ghuser.APIError) bool {
	return strings.Contains(strings.ToLower(e.Message), "resource not accessible by integration")
}

// githubMessageOr passes GitHub's own explanation through when it says more
// than the status code does.
func githubMessageOr(e *ghuser.APIError, fallback string) string {
	msg := strings.TrimSpace(e.Message)
	if msg == "" || strings.EqualFold(msg, "not found") || strings.EqualFold(msg, http.StatusText(e.Status)) {
		return fallback
	}
	return msg
}

// decodeGitHubPRActionBody decodes an optional JSON body; an empty body
// leaves dst untouched.
func decodeGitHubPRActionBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, githubPRActionBodyLimit)).Decode(dst)
	if err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

// afterGitHubPRWrite refreshes the PR card's snapshot ahead of the webhook.
func (h *Handler) afterGitHubPRWrite(t githubPRTarget) {
	h.PRRefresh.Enqueue(t.pr.InstallationID, t.pr.RepoOwner, t.pr.RepoName, t.pr.PrNumber)
}

func writeGitHubPROK(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// recordGitHubPRActivity writes the issue timeline entry for a merge or close
// performed from Multica, attributed to the member who did it.
func (h *Handler) recordGitHubPRActivity(ctx context.Context, t githubPRTarget, action string, extra map[string]any) {
	ctx = context.WithoutCancel(ctx)
	detailMap := map[string]any{
		"pull_request_id": uuidToString(t.pr.ID),
		"pr_number":       t.pr.PrNumber,
		"pr_title":        t.pr.Title,
		"pr_url":          t.pr.HtmlUrl,
		"repo_owner":      t.pr.RepoOwner,
		"repo_name":       t.pr.RepoName,
	}
	for k, v := range extra {
		detailMap[k] = v
	}
	details, _ := json.Marshal(detailMap)
	actorID := uuidToString(t.userID)
	activity, err := h.Queries.CreateActivity(ctx, db.CreateActivityParams{
		ID:          dbid.NewV7(),
		WorkspaceID: t.issue.WorkspaceID,
		IssueID:     t.issue.ID,
		ActorType:   strToText("member"),
		ActorID:     t.userID,
		Action:      action,
		Details:     details,
	})
	if err != nil {
		slog.Warn("github pr action: record activity failed", "action", action, "err", err)
		return
	}
	h.publish(protocol.EventActivityCreated, uuidToString(t.issue.WorkspaceID), "member", actorID, map[string]any{
		"issue_id": uuidToString(t.issue.ID),
		"entry": map[string]any{
			"type":       "activity",
			"id":         uuidToString(activity.ID),
			"actor_type": "member",
			"actor_id":   actorID,
			"action":     activity.Action,
			"details":    json.RawMessage(details),
			"created_at": timestampToString(activity.CreatedAt),
		},
	})
}

// --- merge-options ---------------------------------------------------------

type GitHubMergeOptionsViewer struct {
	Login              string `json:"login"`
	DidAuthor          bool   `json:"did_author"`
	CanUpdateBranch    bool   `json:"can_update_branch"`
	CanEnableAutoMerge bool   `json:"can_enable_auto_merge"`
}

type GitHubMergeCommitMessage struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

type GitHubMergeOptionsResponse struct {
	Viewer              GitHubMergeOptionsViewer            `json:"viewer"`
	AllowedMethods      []string                            `json:"allowed_methods"`
	DefaultMethod       *string                             `json:"default_method"`
	CommitMessages      map[string]GitHubMergeCommitMessage `json:"commit_messages"`
	DeleteBranchOnMerge bool                                `json:"delete_branch_on_merge"`
	HeadIsFork          bool                                `json:"head_is_fork"`
	AutoMergeAllowed    bool                                `json:"auto_merge_allowed"`
	AutoMerge           *GitHubPullRequestAutoMerge         `json:"auto_merge"`
	ReviewDecision      *string                             `json:"review_decision"`
	MergeStateStatus    string                              `json:"merge_state_status"`
	Mergeable           string                              `json:"mergeable"`
	HeadSHA             string                              `json:"head_sha"`
}

const mergeOptionsQuery = `query($owner: String!, $name: String!, $number: Int!) {
  viewer { login }
  repository(owner: $owner, name: $name) {
    squashMergeAllowed
    mergeCommitAllowed
    rebaseMergeAllowed
    autoMergeAllowed
    deleteBranchOnMerge
    viewerDefaultMergeMethod
    pullRequest(number: $number) {
      id
      headRefOid
      isCrossRepository
      viewerDidAuthor
      viewerCanUpdateBranch
      viewerCanEnableAutoMerge
      reviewDecision
      mergeStateStatus
      mergeable
      autoMergeRequest { mergeMethod enabledBy { login } }
      squashTitle: viewerMergeHeadlineText(mergeType: SQUASH)
      squashBody: viewerMergeBodyText(mergeType: SQUASH)
      mergeTitle: viewerMergeHeadlineText(mergeType: MERGE)
      mergeBody: viewerMergeBodyText(mergeType: MERGE)
    }
  }
}`

type mergeOptionsData struct {
	Viewer struct {
		Login string `json:"login"`
	} `json:"viewer"`
	Repository *struct {
		SquashMergeAllowed       bool   `json:"squashMergeAllowed"`
		MergeCommitAllowed       bool   `json:"mergeCommitAllowed"`
		RebaseMergeAllowed       bool   `json:"rebaseMergeAllowed"`
		AutoMergeAllowed         bool   `json:"autoMergeAllowed"`
		DeleteBranchOnMerge      bool   `json:"deleteBranchOnMerge"`
		ViewerDefaultMergeMethod string `json:"viewerDefaultMergeMethod"`
		PullRequest              *struct {
			ID                       string  `json:"id"`
			HeadRefOid               string  `json:"headRefOid"`
			IsCrossRepository        bool    `json:"isCrossRepository"`
			ViewerDidAuthor          bool    `json:"viewerDidAuthor"`
			ViewerCanUpdateBranch    bool    `json:"viewerCanUpdateBranch"`
			ViewerCanEnableAutoMerge bool    `json:"viewerCanEnableAutoMerge"`
			ReviewDecision           *string `json:"reviewDecision"`
			MergeStateStatus         string  `json:"mergeStateStatus"`
			Mergeable                string  `json:"mergeable"`
			AutoMergeRequest         *struct {
				MergeMethod string `json:"mergeMethod"`
				EnabledBy   *struct {
					Login string `json:"login"`
				} `json:"enabledBy"`
			} `json:"autoMergeRequest"`
			SquashTitle string `json:"squashTitle"`
			SquashBody  string `json:"squashBody"`
			MergeTitle  string `json:"mergeTitle"`
			MergeBody   string `json:"mergeBody"`
		} `json:"pullRequest"`
	} `json:"repository"`
}

// GetPullRequestMergeOptions (GET .../merge-options) returns what the merge
// dialog needs, as seen by the caller, from one GraphQL query.
func (h *Handler) GetPullRequestMergeOptions(w http.ResponseWriter, r *http.Request) {
	t, ok := h.loadGitHubPRTarget(w, r)
	if !ok {
		return
	}
	var data mergeOptionsData
	if err := h.GitHubUser.GraphQL(r.Context(), t.token.AccessToken, mergeOptionsQuery, t.repoVariables(), &data); err != nil {
		h.writeGitHubAPIError(r.Context(), w, t, err)
		return
	}
	if data.Repository == nil || data.Repository.PullRequest == nil {
		writeErrorCode(w, http.StatusForbidden, "github_forbidden", "Your GitHub account can't see this pull request.")
		return
	}
	repo, pr := data.Repository, data.Repository.PullRequest

	resp := GitHubMergeOptionsResponse{
		Viewer: GitHubMergeOptionsViewer{
			Login:              data.Viewer.Login,
			DidAuthor:          pr.ViewerDidAuthor,
			CanUpdateBranch:    pr.ViewerCanUpdateBranch,
			CanEnableAutoMerge: pr.ViewerCanEnableAutoMerge,
		},
		AllowedMethods:      []string{},
		CommitMessages:      map[string]GitHubMergeCommitMessage{},
		DeleteBranchOnMerge: repo.DeleteBranchOnMerge,
		HeadIsFork:          pr.IsCrossRepository,
		AutoMergeAllowed:    repo.AutoMergeAllowed,
		MergeStateStatus:    strings.ToLower(pr.MergeStateStatus),
		Mergeable:           strings.ToLower(pr.Mergeable),
		HeadSHA:             pr.HeadRefOid,
	}
	if repo.SquashMergeAllowed {
		resp.AllowedMethods = append(resp.AllowedMethods, "squash")
		resp.CommitMessages["squash"] = GitHubMergeCommitMessage{Title: pr.SquashTitle, Body: pr.SquashBody}
	}
	if repo.MergeCommitAllowed {
		resp.AllowedMethods = append(resp.AllowedMethods, "merge")
		resp.CommitMessages["merge"] = GitHubMergeCommitMessage{Title: pr.MergeTitle, Body: pr.MergeBody}
	}
	if repo.RebaseMergeAllowed {
		resp.AllowedMethods = append(resp.AllowedMethods, "rebase")
	}
	if preferred := strings.ToLower(repo.ViewerDefaultMergeMethod); slices.Contains(resp.AllowedMethods, preferred) {
		resp.DefaultMethod = &preferred
	} else if len(resp.AllowedMethods) > 0 {
		resp.DefaultMethod = &resp.AllowedMethods[0]
	}
	if pr.ReviewDecision != nil && *pr.ReviewDecision != "" {
		decision := strings.ToLower(*pr.ReviewDecision)
		resp.ReviewDecision = &decision
	}
	if am := pr.AutoMergeRequest; am != nil {
		resp.AutoMerge = &GitHubPullRequestAutoMerge{MergeMethod: strings.ToLower(am.MergeMethod)}
		if am.EnabledBy != nil && am.EnabledBy.Login != "" {
			login := am.EnabledBy.Login
			resp.AutoMerge.EnabledBy = &login
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// --- merge -------------------------------------------------------------------

type MergePullRequestRequest struct {
	Method          string  `json:"method"`
	CommitTitle     *string `json:"commit_title"`
	CommitMessage   *string `json:"commit_message"`
	DeleteBranch    bool    `json:"delete_branch"`
	ExpectedHeadSHA string  `json:"expected_head_sha"`
}

type MergePullRequestResponse struct {
	Merged        bool   `json:"merged"`
	SHA           string `json:"sha"`
	BranchDeleted bool   `json:"branch_deleted"`
}

type githubRESTPull struct {
	NodeID string `json:"node_id"`
	Head   struct {
		Ref  string `json:"ref"`
		SHA  string `json:"sha"`
		Repo *struct {
			ID int64 `json:"id"`
		} `json:"repo"`
	} `json:"head"`
	Base struct {
		Repo *struct {
			ID int64 `json:"id"`
		} `json:"repo"`
	} `json:"base"`
}

// MergePullRequest (POST .../merge) merges as the caller. GitHub refuses the
// merge when the head moved past expected_head_sha.
func (h *Handler) MergePullRequest(w http.ResponseWriter, r *http.Request) {
	t, ok := h.loadGitHubPRTarget(w, r)
	if !ok {
		return
	}
	var req MergePullRequestRequest
	if !decodeGitHubPRActionBody(w, r, &req) {
		return
	}
	req.Method = strings.ToLower(strings.TrimSpace(req.Method))
	if _, ok := mergeMethods[req.Method]; !ok {
		writeError(w, http.StatusBadRequest, "method must be merge, squash or rebase")
		return
	}
	req.ExpectedHeadSHA = strings.TrimSpace(req.ExpectedHeadSHA)
	if req.ExpectedHeadSHA == "" {
		writeError(w, http.StatusBadRequest, "expected_head_sha is required")
		return
	}
	ctx := r.Context()

	var pull githubRESTPull
	if req.DeleteBranch {
		if err := h.GitHubUser.REST(ctx, t.token.AccessToken, http.MethodGet, t.pullPath(), nil, &pull); err != nil {
			h.writeGitHubAPIError(ctx, w, t, err)
			return
		}
	}

	body := map[string]any{"merge_method": req.Method, "sha": req.ExpectedHeadSHA}
	if req.CommitTitle != nil && req.Method != "rebase" {
		body["commit_title"] = *req.CommitTitle
	}
	if req.CommitMessage != nil && req.Method != "rebase" {
		body["commit_message"] = *req.CommitMessage
	}
	var merged struct {
		SHA     string `json:"sha"`
		Merged  bool   `json:"merged"`
		Message string `json:"message"`
	}
	if err := h.GitHubUser.REST(ctx, t.token.AccessToken, http.MethodPut, t.pullPath()+"/merge", body, &merged); err != nil {
		var apiErr *ghuser.APIError
		if errors.As(err, &apiErr) && !apiErr.RateLimited && !isGitHubIntegrationPermissionError(apiErr) {
			switch apiErr.Status {
			case http.StatusConflict:
				writeErrorCode(w, http.StatusConflict, "github_head_changed",
					githubMessageOr(apiErr, "The pull request changed since you opened it. Review the new commits and try again."))
				return
			case http.StatusMethodNotAllowed, http.StatusUnprocessableEntity:
				writeErrorCode(w, http.StatusUnprocessableEntity, "github_merge_blocked",
					githubMessageOr(apiErr, "GitHub refused to merge this pull request."))
				return
			}
		}
		h.writeGitHubAPIError(ctx, w, t, err)
		return
	}
	if !merged.Merged {
		writeErrorCode(w, http.StatusUnprocessableEntity, "github_merge_blocked",
			firstNonEmpty(merged.Message, "GitHub did not merge this pull request."))
		return
	}

	branchDeleted := false
	if req.DeleteBranch {
		branchDeleted = h.deleteMergedBranch(ctx, t, pull)
	}
	h.afterGitHubPRWrite(t)
	h.recordGitHubPRActivity(ctx, t, githubPRActivityMerged, map[string]any{"merge_method": req.Method})
	writeJSON(w, http.StatusOK, MergePullRequestResponse{Merged: true, SHA: merged.SHA, BranchDeleted: branchDeleted})
}

// deleteMergedBranch removes the head branch of a same-repository PR. It never
// fails the merge; a branch GitHub already deleted counts as deleted.
func (h *Handler) deleteMergedBranch(ctx context.Context, t githubPRTarget, pull githubRESTPull) bool {
	if pull.Head.Ref == "" || pull.Head.Repo == nil || pull.Base.Repo == nil || pull.Head.Repo.ID != pull.Base.Repo.ID {
		return false
	}
	segments := strings.Split(pull.Head.Ref, "/")
	for i, s := range segments {
		segments[i] = url.PathEscape(s)
	}
	err := h.GitHubUser.REST(context.WithoutCancel(ctx), t.token.AccessToken, http.MethodDelete,
		t.repoPath()+"/git/refs/heads/"+strings.Join(segments, "/"), nil, nil)
	if err == nil {
		return true
	}
	var apiErr *ghuser.APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusUnprocessableEntity &&
		strings.Contains(strings.ToLower(apiErr.Message), "reference does not exist") {
		return true
	}
	slog.Info("github pr action: branch deletion after merge failed", "repo", t.pr.RepoOwner+"/"+t.pr.RepoName, "number", t.pr.PrNumber, "err", err)
	return false
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// --- auto-merge, draft state ---------------------------------------------------

const pullRequestNodeQuery = `query($owner: String!, $name: String!, $number: Int!) {
  repository(owner: $owner, name: $name) {
    autoMergeAllowed
    pullRequest(number: $number) { id }
  }
}`

// pullRequestNode resolves the GraphQL node ID the mutations need.
func (h *Handler) pullRequestNode(ctx context.Context, t githubPRTarget) (id string, autoMergeAllowed bool, err error) {
	var data struct {
		Repository *struct {
			AutoMergeAllowed bool `json:"autoMergeAllowed"`
			PullRequest      *struct {
				ID string `json:"id"`
			} `json:"pullRequest"`
		} `json:"repository"`
	}
	if err := h.GitHubUser.GraphQL(ctx, t.token.AccessToken, pullRequestNodeQuery, t.repoVariables(), &data); err != nil {
		return "", false, err
	}
	if data.Repository == nil || data.Repository.PullRequest == nil || data.Repository.PullRequest.ID == "" {
		return "", false, &ghuser.APIError{Status: http.StatusOK, GraphQLType: "NOT_FOUND"}
	}
	return data.Repository.PullRequest.ID, data.Repository.AutoMergeAllowed, nil
}

// runPullRequestMutation resolves the PR node, then runs one mutation whose
// only variable is $id.
func (h *Handler) runPullRequestMutation(w http.ResponseWriter, r *http.Request, mutation string) {
	t, ok := h.loadGitHubPRTarget(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	id, _, err := h.pullRequestNode(ctx, t)
	if err == nil {
		err = h.GitHubUser.GraphQL(ctx, t.token.AccessToken, mutation, map[string]any{"id": id}, nil)
	}
	if err != nil {
		h.writeGitHubAPIError(ctx, w, t, err)
		return
	}
	h.afterGitHubPRWrite(t)
	writeGitHubPROK(w)
}

type EnablePullRequestAutoMergeRequest struct {
	Method string `json:"method"`
}

const enableAutoMergeMutation = `mutation($id: ID!, $method: PullRequestMergeMethod!) {
  enablePullRequestAutoMerge(input: { pullRequestId: $id, mergeMethod: $method }) { clientMutationId }
}`

// EnablePullRequestAutoMerge (PUT .../auto-merge) queues the PR to merge as
// the caller once its requirements pass.
func (h *Handler) EnablePullRequestAutoMerge(w http.ResponseWriter, r *http.Request) {
	t, ok := h.loadGitHubPRTarget(w, r)
	if !ok {
		return
	}
	var req EnablePullRequestAutoMergeRequest
	if !decodeGitHubPRActionBody(w, r, &req) {
		return
	}
	method, ok := mergeMethods[strings.ToLower(strings.TrimSpace(req.Method))]
	if !ok {
		writeError(w, http.StatusBadRequest, "method must be merge, squash or rebase")
		return
	}
	ctx := r.Context()
	id, allowed, err := h.pullRequestNode(ctx, t)
	if err != nil {
		h.writeGitHubAPIError(ctx, w, t, err)
		return
	}
	if !allowed {
		writeGitHubAutoMergeNotAllowed(w)
		return
	}
	err = h.GitHubUser.GraphQL(ctx, t.token.AccessToken, enableAutoMergeMutation, map[string]any{"id": id, "method": method}, nil)
	if err != nil {
		var apiErr *ghuser.APIError
		if errors.As(err, &apiErr) && isAutoMergeNotAllowedMessage(apiErr.Message) {
			writeGitHubAutoMergeNotAllowed(w)
			return
		}
		h.writeGitHubAPIError(ctx, w, t, err)
		return
	}
	h.afterGitHubPRWrite(t)
	writeGitHubPROK(w)
}

func writeGitHubAutoMergeNotAllowed(w http.ResponseWriter) {
	writeErrorCode(w, http.StatusUnprocessableEntity, "github_auto_merge_not_allowed",
		"Auto-merge is not enabled for this repository. A repository admin can allow it in the repository settings on GitHub.")
}

func isAutoMergeNotAllowedMessage(msg string) bool {
	msg = strings.ToLower(strings.ReplaceAll(msg, "-", " "))
	return strings.Contains(msg, "auto merge is not allowed") || strings.Contains(msg, "auto merge is not enabled")
}

// DisablePullRequestAutoMerge (DELETE .../auto-merge).
func (h *Handler) DisablePullRequestAutoMerge(w http.ResponseWriter, r *http.Request) {
	h.runPullRequestMutation(w, r, `mutation($id: ID!) {
  disablePullRequestAutoMerge(input: { pullRequestId: $id }) { clientMutationId }
}`)
}

// MarkPullRequestReadyForReview (POST .../ready-for-review).
func (h *Handler) MarkPullRequestReadyForReview(w http.ResponseWriter, r *http.Request) {
	h.runPullRequestMutation(w, r, `mutation($id: ID!) {
  markPullRequestReadyForReview(input: { pullRequestId: $id }) { clientMutationId }
}`)
}

// ConvertPullRequestToDraft (POST .../convert-to-draft).
func (h *Handler) ConvertPullRequestToDraft(w http.ResponseWriter, r *http.Request) {
	h.runPullRequestMutation(w, r, `mutation($id: ID!) {
  convertPullRequestToDraft(input: { pullRequestId: $id }) { clientMutationId }
}`)
}

// --- update-branch -----------------------------------------------------------

type UpdatePullRequestBranchRequest struct {
	ExpectedHeadSHA string `json:"expected_head_sha"`
}

// UpdatePullRequestBranch (POST .../update-branch) merges the base branch into
// the head branch as the caller.
func (h *Handler) UpdatePullRequestBranch(w http.ResponseWriter, r *http.Request) {
	t, ok := h.loadGitHubPRTarget(w, r)
	if !ok {
		return
	}
	var req UpdatePullRequestBranchRequest
	if !decodeGitHubPRActionBody(w, r, &req) {
		return
	}
	ctx := r.Context()
	body := map[string]any{}
	if sha := strings.TrimSpace(req.ExpectedHeadSHA); sha != "" {
		body["expected_head_sha"] = sha
	}
	if err := h.GitHubUser.REST(ctx, t.token.AccessToken, http.MethodPut, t.pullPath()+"/update-branch", body, nil); err != nil {
		var apiErr *ghuser.APIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusUnprocessableEntity &&
			strings.Contains(strings.ToLower(apiErr.Message), "expected head sha") {
			writeErrorCode(w, http.StatusConflict, "github_head_changed",
				"The pull request changed since you opened it. Review the new commits and try again.")
			return
		}
		h.writeGitHubAPIError(ctx, w, t, err)
		return
	}
	h.afterGitHubPRWrite(t)
	writeGitHubPROK(w)
}

// --- rerun-failed-checks -------------------------------------------------------

type RerunFailedChecksResponse struct {
	RerunCount int `json:"rerun_count"`
	// NotRerunnable names failed checks a user token cannot rerun: checks
	// from other CI apps, commit statuses, and Actions runs GitHub refused.
	NotRerunnable []string `json:"not_rerunnable"`
}

func isFailedConclusion(conclusion string) bool {
	switch conclusion {
	case "failure", "cancelled", "timed_out", "action_required", "startup_failure", "stale":
		return true
	}
	return false
}

// RerunFailedChecks (POST .../rerun-failed-checks) reruns the failed jobs of
// every failed GitHub Actions run on the PR's head commit, and lists the
// failed checks it cannot rerun.
func (h *Handler) RerunFailedChecks(w http.ResponseWriter, r *http.Request) {
	t, ok := h.loadGitHubPRTarget(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	token := t.token.AccessToken

	var pull githubRESTPull
	if err := h.GitHubUser.REST(ctx, token, http.MethodGet, t.pullPath(), nil, &pull); err != nil {
		h.writeGitHubAPIError(ctx, w, t, err)
		return
	}
	if pull.Head.SHA == "" {
		writeErrorCode(w, http.StatusBadGateway, "github_request_failed", "GitHub returned no head commit for this pull request.")
		return
	}
	headQuery, headPath := url.QueryEscape(pull.Head.SHA), url.PathEscape(pull.Head.SHA)

	var runs struct {
		WorkflowRuns []struct {
			ID         int64  `json:"id"`
			Name       string `json:"name"`
			WorkflowID int64  `json:"workflow_id"`
			Event      string `json:"event"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
		} `json:"workflow_runs"`
	}
	if err := h.GitHubUser.REST(ctx, token, http.MethodGet, t.repoPath()+"/actions/runs?per_page=100&head_sha="+headQuery, nil, &runs); err != nil {
		h.writeGitHubAPIError(ctx, w, t, err)
		return
	}
	var checkRuns struct {
		CheckRuns []struct {
			Name       string `json:"name"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
			App        *struct {
				Slug string `json:"slug"`
			} `json:"app"`
		} `json:"check_runs"`
	}
	if err := h.GitHubUser.REST(ctx, token, http.MethodGet, t.repoPath()+"/commits/"+headPath+"/check-runs?filter=latest&per_page=100", nil, &checkRuns); err != nil {
		h.writeGitHubAPIError(ctx, w, t, err)
		return
	}
	var combined struct {
		Statuses []struct {
			Context string `json:"context"`
			State   string `json:"state"`
		} `json:"statuses"`
	}
	if err := h.GitHubUser.REST(ctx, token, http.MethodGet, t.repoPath()+"/commits/"+headPath+"/status?per_page=100", nil, &combined); err != nil {
		h.writeGitHubAPIError(ctx, w, t, err)
		return
	}

	notRerunnable := map[string]bool{}
	for _, cr := range checkRuns.CheckRuns {
		if cr.Status == "completed" && isFailedConclusion(cr.Conclusion) && (cr.App == nil || cr.App.Slug != githubActionsAppSlug) {
			notRerunnable[cr.Name] = true
		}
	}
	for _, st := range combined.Statuses {
		if st.State == "failure" || st.State == "error" {
			notRerunnable[st.Context] = true
		}
	}

	// The list is newest first; only the latest run of each workflow and
	// trigger event decides whether that workflow is failing.
	seen := map[string]bool{}
	rerunCount := 0
	for _, run := range runs.WorkflowRuns {
		key := fmt.Sprintf("%d/%s", run.WorkflowID, run.Event)
		if seen[key] {
			continue
		}
		seen[key] = true
		if run.Status != "completed" || !isFailedConclusion(run.Conclusion) {
			continue
		}
		err := h.GitHubUser.REST(ctx, token, http.MethodPost, fmt.Sprintf("%s/actions/runs/%d/rerun-failed-jobs", t.repoPath(), run.ID), nil, nil)
		if err == nil {
			rerunCount++
			continue
		}
		var apiErr *ghuser.APIError
		if !errors.As(err, &apiErr) || apiErr.RateLimited || apiErr.Status == http.StatusUnauthorized || isGitHubIntegrationPermissionError(apiErr) {
			h.writeGitHubAPIError(ctx, w, t, err)
			return
		}
		notRerunnable[run.Name] = true
	}

	names := make([]string, 0, len(notRerunnable))
	for name := range notRerunnable {
		names = append(names, name)
	}
	sort.Strings(names)
	if rerunCount > 0 {
		h.afterGitHubPRWrite(t)
	}
	writeJSON(w, http.StatusOK, RerunFailedChecksResponse{RerunCount: rerunCount, NotRerunnable: names})
}

// --- reviews -----------------------------------------------------------------

type ApprovePullRequestRequest struct {
	Body *string `json:"body"`
}

func writeGitHubCannotApproveOwnPR(w http.ResponseWriter) {
	writeErrorCode(w, http.StatusUnprocessableEntity, "github_cannot_approve_own_pr", "You can't approve your own pull request.")
}

// ApprovePullRequest (POST .../approve) submits an approving review as the
// caller.
func (h *Handler) ApprovePullRequest(w http.ResponseWriter, r *http.Request) {
	t, ok := h.loadGitHubPRTarget(w, r)
	if !ok {
		return
	}
	var req ApprovePullRequestRequest
	if !decodeGitHubPRActionBody(w, r, &req) {
		return
	}
	if t.pr.AuthorLogin.Valid && strings.EqualFold(t.pr.AuthorLogin.String, t.token.Login) {
		writeGitHubCannotApproveOwnPR(w)
		return
	}
	ctx := r.Context()
	body := map[string]any{"event": "APPROVE"}
	if req.Body != nil && strings.TrimSpace(*req.Body) != "" {
		body["body"] = *req.Body
	}
	if err := h.GitHubUser.REST(ctx, t.token.AccessToken, http.MethodPost, t.pullPath()+"/reviews", body, nil); err != nil {
		var apiErr *ghuser.APIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusUnprocessableEntity &&
			strings.Contains(strings.ToLower(apiErr.Message), "own pull request") {
			writeGitHubCannotApproveOwnPR(w)
			return
		}
		h.writeGitHubAPIError(ctx, w, t, err)
		return
	}
	h.afterGitHubPRWrite(t)
	writeGitHubPROK(w)
}

type GitHubReviewerCandidate struct {
	Login     string `json:"login"`
	AvatarURL string `json:"avatar_url"`
}

type GitHubReviewerCandidatesResponse struct {
	Users []GitHubReviewerCandidate `json:"users"`
}

// ListPullRequestReviewerCandidates (GET .../reviewer-candidates?q=) lists the
// repository's assignable users whose login matches q, prefix matches first.
// The PR author is left out: GitHub rejects a review request to the author.
func (h *Handler) ListPullRequestReviewerCandidates(w http.ResponseWriter, r *http.Request) {
	t, ok := h.loadGitHubPRTarget(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	var prefix, substring []GitHubReviewerCandidate
	for page := 1; page <= reviewerCandidatePages; page++ {
		var users []GitHubReviewerCandidate
		path := fmt.Sprintf("%s/assignees?per_page=100&page=%d", t.repoPath(), page)
		if err := h.GitHubUser.REST(ctx, t.token.AccessToken, http.MethodGet, path, nil, &users); err != nil {
			h.writeGitHubAPIError(ctx, w, t, err)
			return
		}
		for _, u := range users {
			login := strings.ToLower(u.Login)
			if u.Login == "" || (t.pr.AuthorLogin.Valid && strings.EqualFold(u.Login, t.pr.AuthorLogin.String)) {
				continue
			}
			switch {
			case strings.HasPrefix(login, q):
				prefix = append(prefix, u)
			case strings.Contains(login, q):
				substring = append(substring, u)
			}
		}
		if len(users) < 100 || len(prefix) >= maxReviewerCandidates {
			break
		}
	}
	out := append(prefix, substring...)
	if len(out) > maxReviewerCandidates {
		out = out[:maxReviewerCandidates]
	}
	if out == nil {
		out = []GitHubReviewerCandidate{}
	}
	writeJSON(w, http.StatusOK, GitHubReviewerCandidatesResponse{Users: out})
}

type RequestPullRequestReviewRequest struct {
	Reviewers []string `json:"reviewers"`
}

// RequestPullRequestReview (POST .../request-review) requests reviews from
// GitHub users as the caller.
func (h *Handler) RequestPullRequestReview(w http.ResponseWriter, r *http.Request) {
	t, ok := h.loadGitHubPRTarget(w, r)
	if !ok {
		return
	}
	var req RequestPullRequestReviewRequest
	if !decodeGitHubPRActionBody(w, r, &req) {
		return
	}
	reviewers := make([]string, 0, len(req.Reviewers))
	seen := map[string]bool{}
	for _, login := range req.Reviewers {
		login = strings.TrimSpace(login)
		if !githubLoginPattern.MatchString(login) {
			writeError(w, http.StatusBadRequest, "reviewers must be GitHub logins")
			return
		}
		if key := strings.ToLower(login); !seen[key] {
			seen[key] = true
			reviewers = append(reviewers, login)
		}
	}
	if len(reviewers) == 0 || len(reviewers) > maxReviewersPerRequest {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("reviewers must list 1 to %d GitHub logins", maxReviewersPerRequest))
		return
	}
	ctx := r.Context()
	if err := h.GitHubUser.REST(ctx, t.token.AccessToken, http.MethodPost, t.pullPath()+"/requested_reviewers",
		map[string]any{"reviewers": reviewers}, nil); err != nil {
		h.writeGitHubAPIError(ctx, w, t, err)
		return
	}
	h.afterGitHubPRWrite(t)
	writeGitHubPROK(w)
}

// --- close -------------------------------------------------------------------

// ClosePullRequest (POST .../close) closes the PR without merging, as the
// caller.
func (h *Handler) ClosePullRequest(w http.ResponseWriter, r *http.Request) {
	t, ok := h.loadGitHubPRTarget(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	if err := h.GitHubUser.REST(ctx, t.token.AccessToken, http.MethodPatch, t.pullPath(), map[string]any{"state": "closed"}, nil); err != nil {
		h.writeGitHubAPIError(ctx, w, t, err)
		return
	}
	h.afterGitHubPRWrite(t)
	h.recordGitHubPRActivity(ctx, t, githubPRActivityClosed, nil)
	writeGitHubPROK(w)
}
