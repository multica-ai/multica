package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
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

// errGitHubPRNotFound signals that GitHub returned 404 for a pull request
// fetch — either the URL is wrong or none of the workspace's installations
// are authorized to see it.
var errGitHubPRNotFound = errors.New("github pull request not found or not accessible")

var githubPullRequestURLPattern = regexp.MustCompile(`^https://github\.com/([^/]+)/([^/]+)/pull/(\d+)(?:/.*)?$`)

// parseGitHubPullRequestURL extracts (owner, repo, number) from a GitHub pull
// request URL such as https://github.com/owner/repo/pull/123, tolerating a
// trailing path segment (e.g. /files) or query/fragment.
func parseGitHubPullRequestURL(raw string) (owner, repo string, number int32, err error) {
	trimmed := strings.TrimSpace(raw)
	if idx := strings.IndexAny(trimmed, "?#"); idx >= 0 {
		trimmed = trimmed[:idx]
	}
	trimmed = strings.TrimSuffix(trimmed, "/")
	m := githubPullRequestURLPattern.FindStringSubmatch(trimmed)
	if m == nil {
		return "", "", 0, errors.New("not a GitHub pull request URL; expected https://github.com/<owner>/<repo>/pull/<number>")
	}
	n, convErr := strconv.ParseInt(m[3], 10, 32)
	if convErr != nil {
		return "", "", 0, errors.New("invalid pull request number")
	}
	return m[1], strings.TrimSuffix(m[2], ".git"), int32(n), nil
}

// mintGitHubPRLinkToken exchanges the App JWT for an installation access
// token. Unlike fetchGitHubInstallationRepositories's scoped-down metadata
// token, this requests the installation's full granted permissions (empty
// body), since reading pull request content needs the app's normal
// pull-requests permission.
func mintGitHubPRLinkToken(ctx context.Context, installationID int64) (string, *http.Client, error) {
	appJWT, err := signGitHubAppJWT(time.Now())
	if err != nil {
		return "", nil, err
	}
	if appJWT == "" {
		return "", nil, errors.New("github App JWT credentials unavailable")
	}

	client := &http.Client{Timeout: 15 * time.Second}
	endpoint := fmt.Sprintf("%s/app/installations/%d/access_tokens", strings.TrimRight(githubAPIBase, "/"), installationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return "", nil, err
	}
	setGitHubAPIHeaders(req, appJWT)
	resp, err := client.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("create installation token: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, githubAPIResponseLimit))
		return "", nil, fmt.Errorf("create installation token: github status %d", resp.StatusCode)
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, githubAPIResponseLimit)).Decode(&body); err != nil {
		return "", nil, fmt.Errorf("decode installation token: %w", err)
	}
	if body.Token == "" {
		return "", nil, errors.New("github returned an empty installation token")
	}
	return body.Token, client, nil
}

// fetchGitHubPullRequestByNumber fetches a single pull request straight from
// the GitHub REST API, as the given installation. The response body is the
// same "pull_request" object shape GitHub sends in a pull_request webhook, so
// it decodes into ghPRObject and can be mirrored through the same upsert path
// as webhook-discovered PRs.
func fetchGitHubPullRequestByNumber(ctx context.Context, installationID int64, owner, repo string, number int32) (ghPRObject, error) {
	token, client, err := mintGitHubPRLinkToken(ctx, installationID)
	if err != nil {
		return ghPRObject{}, err
	}
	defer revokeGitHubInstallationToken(client, token)

	endpoint := fmt.Sprintf("%s/repos/%s/%s/pulls/%d", strings.TrimRight(githubAPIBase, "/"), owner, repo, number)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ghPRObject{}, err
	}
	setGitHubAPIHeaders(req, token)
	resp, err := client.Do(req)
	if err != nil {
		return ghPRObject{}, fmt.Errorf("fetch pull request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, githubAPIResponseLimit))
		return ghPRObject{}, errGitHubPRNotFound
	}
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, githubAPIResponseLimit))
		return ghPRObject{}, fmt.Errorf("fetch pull request: github status %d", resp.StatusCode)
	}
	var pr ghPRObject
	if err := json.NewDecoder(io.LimitReader(resp.Body, githubAPIResponseLimit)).Decode(&pr); err != nil {
		return ghPRObject{}, fmt.Errorf("decode pull request: %w", err)
	}
	return pr, nil
}

// resolveGitHubPRInstallations lists the workspace's GitHub installations
// with the one whose account login matches the PR URL's owner (the common
// case) tried first, so a workspace with several installations doesn't mint
// a token against every one of them before finding the right match.
func (h *Handler) resolveGitHubPRInstallations(ctx context.Context, wsID pgtype.UUID, owner string) ([]db.GithubInstallation, error) {
	insts, err := h.Queries.ListGitHubInstallationsByWorkspace(ctx, wsID)
	if err != nil {
		return nil, err
	}
	ordered := make([]db.GithubInstallation, 0, len(insts))
	var rest []db.GithubInstallation
	for _, inst := range insts {
		if strings.EqualFold(inst.AccountLogin, owner) {
			ordered = append(ordered, inst)
		} else {
			rest = append(rest, inst)
		}
	}
	return append(ordered, rest...), nil
}

// upsertGitHubPRFromFetch mirrors a REST-fetched pull request into
// github_pull_request the same way mirrorPullRequestForWorkspace mirrors a
// webhook delivery. A freshly fetched PR always carries GitHub's current
// mergeable_state verdict, so clear_mergeable_state is always false here —
// there is no stale value to blank out.
func (h *Handler) upsertGitHubPRFromFetch(ctx context.Context, wsID pgtype.UUID, installationID int64, owner, repo string, pr ghPRObject) (db.GithubPullRequest, error) {
	state := derivePRState(pr.State, pr.Draft, pr.Merged)
	return h.Queries.UpsertGitHubPullRequest(ctx, db.UpsertGitHubPullRequestParams{
		WorkspaceID:         wsID,
		InstallationID:      installationID,
		RepoOwner:           owner,
		RepoName:            repo,
		PrNumber:            pr.Number,
		Title:               pr.Title,
		State:               state,
		HtmlUrl:             pr.HTMLURL,
		Branch:              ptrToText(strPtrOrNil(pr.Head.Ref)),
		AuthorLogin:         ptrToText(strPtrOrNil(pr.User.Login)),
		AuthorAvatarUrl:     ptrToText(strPtrOrNil(pr.User.AvatarURL)),
		MergedAt:            parseGHTime(pr.MergedAt),
		ClosedAt:            parseGHTime(pr.ClosedAt),
		PrCreatedAt:         parseGHTimeRequired(pr.CreatedAt),
		PrUpdatedAt:         parseGHTimeRequired(pr.UpdatedAt),
		HeadSha:             pr.Head.SHA,
		MergeableState:      ptrToText(strPtrOrNil(pr.MergeableState)),
		ClearMergeableState: pgtype.Bool{Bool: false, Valid: true},
		Additions:           pr.Additions,
		Deletions:           pr.Deletions,
		ChangedFiles:        pr.ChangedFiles,
	})
}

type linkPullRequestRequest struct {
	URL          string `json:"url"`
	CloseOnMerge bool   `json:"close_on_merge"`
}

// LinkPullRequestToIssue is the private counterpart to webhook auto-link
// discovery (mirrorPullRequestForWorkspace): it links a GitHub pull request
// to an issue without requiring the issue identifier to appear anywhere in
// the PR's title, body, or branch name. It fetches the PR fresh from GitHub
// so the mirrored github_pull_request row holds real data — exactly as a
// webhook delivery would have written it — then writes the same
// issue_pull_request link row webhook discovery uses, so the PR shows up in
// the issue's linked-PR list and UI. The link is idempotent: relinking the
// same issue/PR pair updates close_intent instead of creating a duplicate row
// (see LinkIssueToPullRequest's ON CONFLICT).
func (h *Handler) LinkPullRequestToIssue(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req linkPullRequestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	owner, repo, number, err := parseGitHubPullRequestURL(req.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	installations, err := h.resolveGitHubPRInstallations(r.Context(), issue.WorkspaceID, owner)
	if err != nil {
		slog.Warn("link pull request: list installations failed", "err", err, "issue_id", issueID)
		writeError(w, http.StatusInternalServerError, "failed to list github installations")
		return
	}
	if len(installations) == 0 {
		writeError(w, http.StatusBadRequest, "no GitHub installation connected for this workspace")
		return
	}

	var (
		pr       ghPRObject
		usedInst db.GithubInstallation
		fetchErr error
		found    bool
	)
	for _, inst := range installations {
		pr, fetchErr = fetchGitHubPullRequestByNumber(r.Context(), inst.InstallationID, owner, repo, number)
		if fetchErr == nil {
			usedInst = inst
			found = true
			break
		}
	}
	if !found {
		if errors.Is(fetchErr, errGitHubPRNotFound) {
			writeError(w, http.StatusNotFound, "pull request not found, or not accessible by any GitHub installation connected to this workspace")
			return
		}
		slog.Warn("link pull request: fetch failed", "err", fetchErr, "owner", owner, "repo", repo, "number", number)
		writeError(w, http.StatusBadGateway, "failed to fetch pull request from github")
		return
	}

	prRow, err := h.upsertGitHubPRFromFetch(r.Context(), issue.WorkspaceID, usedInst.InstallationID, owner, repo, pr)
	if err != nil {
		slog.Warn("link pull request: upsert failed", "err", err, "issue_id", issueID)
		writeError(w, http.StatusInternalServerError, "failed to save pull request")
		return
	}

	workspaceID := uuidToString(issue.WorkspaceID)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)

	if err := h.Queries.LinkIssueToPullRequest(r.Context(), db.LinkIssueToPullRequestParams{
		IssueID:             issue.ID,
		PullRequestID:       prRow.ID,
		CloseIntent:         req.CloseOnMerge,
		LinkedByType:        strToText(actorType),
		LinkedByID:          parseUUID(actorID),
		ReferenceOnly:       false,
		PreserveCloseIntent: false,
	}); err != nil {
		slog.Warn("link pull request: link failed", "err", err, "issue_id", issueID)
		writeError(w, http.StatusInternalServerError, "failed to link pull request")
		return
	}

	resp := githubPullRequestToResponse(prRow, h.PRRefresh.Enabled())

	h.publish(protocol.EventPullRequestLinked, workspaceID, actorType, actorID, map[string]any{
		"pull_request":     resp,
		"issue_id":         uuidToString(issue.ID),
		"linked_issue_ids": []string{uuidToString(issue.ID)},
	})

	writeJSON(w, http.StatusOK, map[string]any{"pull_request": resp})
}

type unlinkPullRequestRequest struct {
	URL string `json:"url"`
}

// UnlinkPullRequestFromIssue removes an issue↔PR link (manual or
// webhook-discovered). It never calls GitHub: the pull request row must
// already be mirrored locally for a link to exist at all.
func (h *Handler) UnlinkPullRequestFromIssue(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req unlinkPullRequestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	owner, repo, number, err := parseGitHubPullRequestURL(req.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	prRow, err := h.Queries.GetGitHubPullRequest(r.Context(), db.GetGitHubPullRequestParams{
		WorkspaceID: issue.WorkspaceID,
		RepoOwner:   owner,
		RepoName:    repo,
		PrNumber:    number,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "pull request is not known to this workspace")
			return
		}
		slog.Warn("unlink pull request: lookup failed", "err", err, "issue_id", issueID)
		writeError(w, http.StatusInternalServerError, "failed to look up pull request")
		return
	}

	if err := h.Queries.UnlinkIssueFromPullRequest(r.Context(), db.UnlinkIssueFromPullRequestParams{
		IssueID:       issue.ID,
		PullRequestID: prRow.ID,
	}); err != nil {
		slog.Warn("unlink pull request: unlink failed", "err", err, "issue_id", issueID)
		writeError(w, http.StatusInternalServerError, "failed to unlink pull request")
		return
	}

	workspaceID := uuidToString(issue.WorkspaceID)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	h.publish(protocol.EventPullRequestUnlinked, workspaceID, actorType, actorID, map[string]any{
		"pull_request_id": uuidToString(prRow.ID),
		"issue_id":        uuidToString(issue.ID),
	})

	writeJSON(w, http.StatusOK, map[string]any{"unlinked": true})
}
