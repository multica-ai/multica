// CEREBRO-PATCH(cerebro-github-pr-link): FIR-2568 — net-new fork file: poll-based PR↔issue linking endpoint.
//
// The upstream GitHub integration only learns about a PR when GitHub delivers a
// signed `pull_request` webhook (handlePullRequestEvent). That path needs the
// webhook secret + a connected installation row, neither of which is configured
// on this deployment — so PRs never link. firtal-data-registry already polls
// GitHub on a timer (lib/deploy-scan) using the installed GitHub App, so it can
// see every PR's title/body/branch without a webhook. This endpoint is the
// inbound seam: the registry POSTs each open PR it scanned, and we run the exact
// same upsert + identifier-extraction + link logic the webhook uses, writing the
// link rows into the same issue_pull_request table the PR sidebar reads.
//
// Reuses the unexported handler helpers (extractIdentifiers, getIssuePrefix,
// lookupIssueByIdentifier, derivePRState, githubPullRequestToResponse, …) by
// living in package handler. Service-to-service auth mirrors cerebro/pricing:
// a shared Bearer key (CEREBRO_GITHUB_LINK_KEY); loopback-only when unset.
package handler

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// CerebroGitHubPRLinkHandler links polled PRs to issues. Constructed in the
// router from CEREBRO_GITHUB_LINK_KEY (auth) and CEREBRO_GITHUB_LINK_WORKSPACE_SLUG
// (the workspace a request defaults to when the body omits workspace_slug).
type CerebroGitHubPRLinkHandler struct {
	h           *Handler
	token       string
	defaultSlug string
}

// NewCerebroGitHubPRLinkHandler builds the handler. An empty token switches to
// loopback-only mode (local dev), matching the pricing endpoint's policy.
func NewCerebroGitHubPRLinkHandler(h *Handler, token, defaultSlug string) *CerebroGitHubPRLinkHandler {
	return &CerebroGitHubPRLinkHandler{
		h:           h,
		token:       strings.TrimSpace(token),
		defaultSlug: strings.TrimSpace(defaultSlug),
	}
}

// cerebroPRLinkRequest is one polled PR. The registry already fetched these
// fields from GitHub; we never call out to GitHub from here.
type cerebroPRLinkRequest struct {
	// WorkspaceSlug picks the workspace; falls back to the configured default.
	WorkspaceSlug string `json:"workspace_slug"`
	// InstallationID is the GitHub App installation id the registry used to
	// read the PR. Stored on the mirror row (BIGINT NOT NULL); not an FK, so a
	// connected github_installation row is not required.
	InstallationID int64  `json:"installation_id"`
	RepoOwner      string `json:"repo_owner"`
	RepoName       string `json:"repo_name"`
	PrNumber       int32  `json:"pr_number"`
	Title          string `json:"title"`
	Body           string `json:"body"`
	Branch         string `json:"branch"`
	// State is open|closed|merged|draft. The poller only sends open PRs today;
	// defaults to "open" when omitted. Linking is state-independent.
	State       string `json:"state"`
	HtmlURL     string `json:"html_url"`
	AuthorLogin string `json:"author_login"`
	HeadSha     string `json:"head_sha"`
	PrCreatedAt string `json:"pr_created_at"`
	PrUpdatedAt string `json:"pr_updated_at"`
	// MergedAt/ClosedAt let the poller mirror terminal PRs accurately — the
	// scanner covers merged/deployed PRs, not just open ones.
	MergedAt string `json:"merged_at"`
	ClosedAt string `json:"closed_at"`
}

type cerebroPRLinkResponse struct {
	PullRequestID  string   `json:"pull_request_id"`
	WorkspaceID    string   `json:"workspace_id"`
	LinkedIssueIDs []string `json:"linked_issue_ids"`
	// AutoLinkDisabled is true when the workspace has GitHub auto-linking off;
	// the PR mirror is still upserted but no link rows are written.
	AutoLinkDisabled bool `json:"auto_link_disabled"`
}

// Post ingests one polled PR: upsert the mirror row, then (when the workspace
// allows auto-linking) extract issue identifiers from title/body/branch and
// write the link rows. Idempotent end to end — re-posting the same PR upserts.
func (c *CerebroGitHubPRLinkHandler) Post(w http.ResponseWriter, r *http.Request) {
	if c.token != "" {
		if !hasBearerToken(r, c.token) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="github-link"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	} else if !isDirectLoopbackRequest(r) {
		http.NotFound(w, r)
		return
	}

	var req cerebroPRLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request body", http.StatusBadRequest)
		return
	}
	if req.RepoOwner == "" || req.RepoName == "" || req.PrNumber <= 0 || req.HtmlURL == "" || req.InstallationID == 0 {
		http.Error(w, "missing required fields (repo_owner, repo_name, pr_number, html_url, installation_id)", http.StatusBadRequest)
		return
	}

	slug := strings.TrimSpace(req.WorkspaceSlug)
	if slug == "" {
		slug = c.defaultSlug
	}
	if slug == "" {
		http.Error(w, "no workspace_slug and no configured default", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	ws, err := c.h.Queries.GetWorkspaceBySlug(ctx, slug)
	if err != nil {
		http.Error(w, "unknown workspace", http.StatusNotFound)
		return
	}

	state := strings.ToLower(strings.TrimSpace(req.State))
	switch state {
	case "open", "closed", "merged", "draft":
	default:
		state = "open"
	}

	pr, err := c.h.Queries.UpsertGitHubPullRequest(ctx, db.UpsertGitHubPullRequestParams{
		WorkspaceID:         ws.ID,
		InstallationID:      req.InstallationID,
		RepoOwner:           req.RepoOwner,
		RepoName:            req.RepoName,
		PrNumber:            req.PrNumber,
		Title:               req.Title,
		State:               state,
		HtmlUrl:             req.HtmlURL,
		Branch:              ptrToText(strPtrOrNil(req.Branch)),
		AuthorLogin:         ptrToText(strPtrOrNil(req.AuthorLogin)),
		AuthorAvatarUrl:     pgtype.Text{},
		MergedAt:            parseGHTime(req.MergedAt),
		ClosedAt:            parseGHTime(req.ClosedAt),
		PrCreatedAt:         parseGHTimeRequired(req.PrCreatedAt),
		PrUpdatedAt:         parseGHTimeRequired(req.PrUpdatedAt),
		HeadSha:             req.HeadSha,
		MergeableState:      pgtype.Text{},
		ClearMergeableState: pgtype.Bool{Bool: false, Valid: true},
		Additions:           0,
		Deletions:           0,
		ChangedFiles:        0,
	})
	if err != nil {
		slog.Warn("cerebro github link: upsert pr failed", "err", err, "repo", req.RepoOwner+"/"+req.RepoName, "pr", req.PrNumber)
		http.Error(w, "upsert failed", http.StatusInternalServerError)
		return
	}

	resp := cerebroPRLinkResponse{
		PullRequestID:  uuidToString(pr.ID),
		WorkspaceID:    uuidToString(ws.ID),
		LinkedIssueIDs: []string{},
	}

	// Respect the same workspace gate the webhook honors: when auto-linking is
	// off (or the master GitHub switch is off) we still keep the PR mirror but
	// write no link rows.
	if !c.h.workspaceAutoLinkPRsEnabled(ctx, ws.ID) {
		resp.AutoLinkDisabled = true
		c.writeJSON(w, resp)
		return
	}

	idents := extractIdentifiers(req.Title, req.Body, req.Branch)
	closing := map[string]struct{}{}
	for _, id := range extractClosingIdentifiers(req.Title, req.Body) {
		closing[id] = struct{}{}
	}
	prefix := c.h.getIssuePrefix(ctx, ws.ID)

	for _, id := range idents {
		issue, ok := c.h.lookupIssueByIdentifier(ctx, ws.ID, prefix, id)
		if !ok {
			continue
		}
		_, declared := closing[id]
		if err := c.h.Queries.LinkIssueToPullRequest(ctx, db.LinkIssueToPullRequestParams{
			IssueID:             issue.ID,
			PullRequestID:       pr.ID,
			CloseIntent:         declared,
			PreserveCloseIntent: false,
			LinkedByType:        strToText("system"),
			LinkedByID:          pgtype.UUID{},
		}); err != nil {
			slog.Warn("cerebro github link: link failed", "err", err, "issue_id", uuidToString(issue.ID))
			continue
		}
		resp.LinkedIssueIDs = append(resp.LinkedIssueIDs, uuidToString(issue.ID))
	}

	// Tell any open issue-detail page to re-query its PR list, same as the
	// webhook path does.
	c.h.publish(protocol.EventPullRequestUpdated, uuidToString(ws.ID), "system", "", map[string]any{
		"pull_request":     githubPullRequestToResponse(pr),
		"linked_issue_ids": resp.LinkedIssueIDs,
	})

	c.writeJSON(w, resp)
}

func (c *CerebroGitHubPRLinkHandler) writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// hasBearerToken / isDirectLoopbackRequest mirror cerebro/pricing's auth policy
// (constant-time compare; loopback-only fallback when no key is configured).
func hasBearerToken(r *http.Request, want string) bool {
	auth := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(auth) <= len(prefix) || !strings.EqualFold(auth[:len(prefix)], prefix) {
		return false
	}
	got := strings.TrimSpace(auth[len(prefix):])
	if got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func isDirectLoopbackRequest(r *http.Request) bool {
	for _, hdr := range []string{"X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "X-Real-Ip", "Forwarded"} {
		if strings.TrimSpace(r.Header.Get(hdr)) != "" {
			return false
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if host == "" {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}
