package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ---------------------------------------------------------------------------
// Response types
// ---------------------------------------------------------------------------

type PullRequestResponse struct {
	ID        string  `json:"id"`
	IssueID   string  `json:"issue_id"`
	RepoOwner string  `json:"repo_owner"`
	RepoName  string  `json:"repo_name"`
	PRNumber  int32   `json:"pr_number"`
	Title     string  `json:"title"`
	Status    string  `json:"status"`
	Author    string  `json:"author"`
	URL       string  `json:"url"`
	Branch    *string `json:"branch"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

type GitHubInstallationResponse struct {
	ID             string `json:"id"`
	WorkspaceID    string `json:"workspace_id"`
	InstallationID int64  `json:"installation_id"`
	AccountLogin   string `json:"account_login"`
	AccountType    string `json:"account_type"`
	CreatedAt      string `json:"created_at"`
}

func pullRequestToResponse(pr db.IssuePullRequest) PullRequestResponse {
	return PullRequestResponse{
		ID:        uuidToString(pr.ID),
		IssueID:   uuidToString(pr.IssueID),
		RepoOwner: pr.RepoOwner,
		RepoName:  pr.RepoName,
		PRNumber:  pr.PrNumber,
		Title:     pr.Title,
		Status:    pr.Status,
		Author:    pr.Author,
		URL:       pr.Url,
		Branch:    textToPtr(pr.Branch),
		CreatedAt: timestampToString(pr.CreatedAt),
		UpdatedAt: timestampToString(pr.UpdatedAt),
	}
}

func installationToResponse(gi db.GithubInstallation) GitHubInstallationResponse {
	return GitHubInstallationResponse{
		ID:             uuidToString(gi.ID),
		WorkspaceID:    uuidToString(gi.WorkspaceID),
		InstallationID: gi.InstallationID,
		AccountLogin:   gi.AccountLogin,
		AccountType:    gi.AccountType,
		CreatedAt:      timestampToString(gi.CreatedAt),
	}
}

// ---------------------------------------------------------------------------
// GitHub webhook handler (public — no auth required, uses HMAC signature)
// ---------------------------------------------------------------------------

// issueIdentifierPattern matches issue identifiers like "MUL-82" in branch names and PR bodies.
var issueIdentifierPattern = regexp.MustCompile(`(?i)\b([A-Z]{2,5})-(\d+)\b`)

func (h *Handler) GitHubWebhook(w http.ResponseWriter, r *http.Request) {
	secret := os.Getenv("GITHUB_WEBHOOK_SECRET")
	if secret == "" {
		writeError(w, http.StatusInternalServerError, "GitHub webhook secret not configured")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read body")
		return
	}

	// Validate HMAC-SHA256 signature
	sig := r.Header.Get("X-Hub-Signature-256")
	if !verifyGitHubSignature(body, sig, secret) {
		writeError(w, http.StatusUnauthorized, "invalid signature")
		return
	}

	eventType := r.Header.Get("X-GitHub-Event")
	switch eventType {
	case "pull_request":
		h.handlePullRequestEvent(w, r, body)
	case "installation":
		h.handleInstallationEvent(w, r, body)
	default:
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ignored"}`))
	}
}

func verifyGitHubSignature(payload []byte, signature, secret string) bool {
	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}
	sig, err := hex.DecodeString(signature[7:])
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expected := mac.Sum(nil)
	return hmac.Equal(sig, expected)
}

// ---------------------------------------------------------------------------
// Pull request event handling
// ---------------------------------------------------------------------------

type ghPullRequestEvent struct {
	Action       string         `json:"action"`
	Number       int            `json:"number"`
	PullRequest  ghPullRequest  `json:"pull_request"`
	Repository   ghRepository   `json:"repository"`
	Installation ghInstallation `json:"installation"`
}

type ghPullRequest struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	State   string `json:"state"`
	Draft   bool   `json:"draft"`
	Merged  bool   `json:"merged"`
	HTMLURL string `json:"html_url"`
	User    ghUser `json:"user"`
	Head    ghRef  `json:"head"`
}

type ghRef struct {
	Ref string `json:"ref"`
}

type ghUser struct {
	Login string `json:"login"`
}

type ghRepository struct {
	FullName string  `json:"full_name"`
	Owner    ghOwner `json:"owner"`
	Name     string  `json:"name"`
}

type ghOwner struct {
	Login string `json:"login"`
}

type ghInstallation struct {
	ID int64 `json:"id"`
}

func (h *Handler) handlePullRequestEvent(w http.ResponseWriter, r *http.Request, body []byte) {
	var event ghPullRequestEvent
	if err := json.Unmarshal(body, &event); err != nil {
		writeError(w, http.StatusBadRequest, "invalid pull_request payload")
		return
	}

	switch event.Action {
	case "opened", "reopened", "synchronize", "edited", "closed":
		// Process these actions
	default:
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ignored"}`))
		return
	}

	// Find workspace via installation ID
	installation, err := h.Queries.GetGitHubInstallationByInstallationID(r.Context(), event.Installation.ID)
	if err != nil {
		slog.Warn("github webhook: installation not found", "installation_id", event.Installation.ID)
		writeError(w, http.StatusNotFound, "installation not found")
		return
	}

	workspaceID := uuidToString(installation.WorkspaceID)

	// Determine PR status
	prStatus := "open"
	if event.PullRequest.Draft {
		prStatus = "draft"
	}
	if event.PullRequest.Merged {
		prStatus = "merged"
	} else if event.PullRequest.State == "closed" {
		prStatus = "closed"
	}

	// Try to find linked issues by:
	// 1. Branch name (e.g. mul-82/fix-login)
	// 2. PR title/body containing issue identifier (e.g. MUL-82)
	issueNumbers := findIssueIdentifiers(
		event.PullRequest.Head.Ref,
		event.PullRequest.Title,
		event.PullRequest.Body,
	)

	linked := 0
	for _, num := range issueNumbers {
		issue, err := h.Queries.FindIssueByIdentifierNumber(r.Context(), db.FindIssueByIdentifierNumberParams{
			WorkspaceID: installation.WorkspaceID,
			Number:      num,
		})
		if err != nil {
			continue
		}

		branch := event.PullRequest.Head.Ref
		pr, err := h.Queries.UpsertIssuePullRequest(r.Context(), db.UpsertIssuePullRequestParams{
			IssueID:   issue.ID,
			WorkspaceID: installation.WorkspaceID,
			RepoOwner: event.Repository.Owner.Login,
			RepoName:  event.Repository.Name,
			PrNumber:  int32(event.PullRequest.Number),
			Title:     event.PullRequest.Title,
			Status:    prStatus,
			Author:    event.PullRequest.User.Login,
			Url:       event.PullRequest.HTMLURL,
			Branch:    strToText(branch),
		})
		if err != nil {
			slog.Warn("github webhook: failed to upsert PR", "error", err, "issue_id", uuidToString(issue.ID))
			continue
		}

		prResp := pullRequestToResponse(pr)
		h.publish(protocol.EventPullRequestLinked, workspaceID, "system", "github", map[string]any{
			"pull_request": prResp,
			"issue_id":     uuidToString(issue.ID),
		})

		// Auto-transition issue status on merge
		if prStatus == "merged" && issue.Status != "done" {
			if updated, err := h.Queries.UpdateIssueStatus(r.Context(), db.UpdateIssueStatusParams{
				ID:     issue.ID,
				Status: "done",
			}); err == nil {
				prefix := h.getIssuePrefix(r.Context(), updated.WorkspaceID)
				resp := issueToResponse(updated, prefix)
				h.publish(protocol.EventIssueUpdated, workspaceID, "system", "github", map[string]any{
					"issue":          resp,
					"status_changed": true,
					"prev_status":    issue.Status,
				})
			}
		}

		linked++
	}

	// If no issues were linked by identifier, also update any existing PR records
	// that match this repo+number (e.g. manually linked PRs)
	if linked == 0 {
		h.Queries.UpdatePullRequestStatus(r.Context(), db.UpdatePullRequestStatusParams{
			Status:    prStatus,
			RepoOwner: event.Repository.Owner.Login,
			RepoName:  event.Repository.Name,
			PrNumber:  int32(event.PullRequest.Number),
		})
	}

	slog.Info("github webhook: pull_request processed",
		"action", event.Action,
		"repo", event.Repository.FullName,
		"pr", event.PullRequest.Number,
		"linked_issues", linked,
	)

	writeJSON(w, http.StatusOK, map[string]any{
		"status":        "ok",
		"linked_issues": linked,
	})
}

// findIssueIdentifiers extracts issue numbers from branch name, PR title, and body.
func findIssueIdentifiers(branch, title, body string) []int32 {
	seen := map[int32]bool{}
	var numbers []int32

	for _, text := range []string{branch, title, body} {
		matches := issueIdentifierPattern.FindAllStringSubmatch(text, -1)
		for _, match := range matches {
			if len(match) >= 3 {
				num, err := strconv.Atoi(match[2])
				if err == nil && num > 0 && !seen[int32(num)] {
					seen[int32(num)] = true
					numbers = append(numbers, int32(num))
				}
			}
		}
	}

	return numbers
}

// ---------------------------------------------------------------------------
// Installation event handling
// ---------------------------------------------------------------------------

type ghInstallationEvent struct {
	Action       string              `json:"action"`
	Installation ghInstallationFull  `json:"installation"`
}

type ghInstallationFull struct {
	ID      int64   `json:"id"`
	AppID   int64   `json:"app_id"`
	Account ghOwner `json:"account"`
	TargetType string `json:"target_type"`
}

func (h *Handler) handleInstallationEvent(w http.ResponseWriter, r *http.Request, body []byte) {
	var event ghInstallationEvent
	if err := json.Unmarshal(body, &event); err != nil {
		writeError(w, http.StatusBadRequest, "invalid installation payload")
		return
	}

	switch event.Action {
	case "deleted", "suspend":
		// Remove installation record
		h.Queries.DeleteGitHubInstallationByInstallationID(r.Context(), event.Installation.ID)
		slog.Info("github webhook: installation removed", "installation_id", event.Installation.ID)
	default:
		slog.Info("github webhook: installation event ignored", "action", event.Action)
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

// ---------------------------------------------------------------------------
// GitHub App OAuth callback — links installation to workspace
// ---------------------------------------------------------------------------

func (h *Handler) GitHubInstallationCallback(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	installationIDStr := r.URL.Query().Get("installation_id")
	if installationIDStr == "" {
		writeError(w, http.StatusBadRequest, "installation_id is required")
		return
	}

	installationID, err := strconv.ParseInt(installationIDStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid installation_id")
		return
	}

	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	// Verify user is admin/owner of the workspace
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}

	// Fetch installation details from query params (set by GitHub redirect)
	// In production, you'd verify this with GitHub API. For now, trust the redirect params.
	accountLogin := r.URL.Query().Get("account_login")
	accountType := r.URL.Query().Get("account_type")
	if accountLogin == "" {
		accountLogin = "unknown"
	}
	if accountType == "" {
		accountType = "Organization"
	}

	appIDStr := os.Getenv("GITHUB_APP_ID")
	appID, _ := strconv.ParseInt(appIDStr, 10, 64)

	gi, err := h.Queries.CreateGitHubInstallation(r.Context(), db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(workspaceID),
		InstallationID: installationID,
		AccountLogin:   accountLogin,
		AccountType:    accountType,
		AppID:          appID,
	})
	if err != nil {
		slog.Warn("failed to create github installation", "error", err, "workspace_id", workspaceID)
		writeError(w, http.StatusInternalServerError, "failed to save installation")
		return
	}

	resp := installationToResponse(gi)
	h.publish(protocol.EventGitHubInstallationCreated, workspaceID, "member", userID, map[string]any{
		"installation": resp,
	})

	slog.Info("github installation linked",
		"workspace_id", workspaceID,
		"installation_id", installationID,
		"account", accountLogin,
	)

	// Redirect to frontend settings page
	frontendOrigin := os.Getenv("FRONTEND_ORIGIN")
	if frontendOrigin == "" {
		frontendOrigin = "http://localhost:3000"
	}
	http.Redirect(w, r, fmt.Sprintf("%s/settings?tab=integrations&github=connected", frontendOrigin), http.StatusTemporaryRedirect)
}

// ---------------------------------------------------------------------------
// REST API: List installations for a workspace
// ---------------------------------------------------------------------------

func (h *Handler) ListGitHubInstallations(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)

	installations, err := h.Queries.ListGitHubInstallations(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list installations")
		return
	}

	resp := make([]GitHubInstallationResponse, len(installations))
	for i, gi := range installations {
		resp[i] = installationToResponse(gi)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"installations": resp,
	})
}

// ---------------------------------------------------------------------------
// REST API: Delete an installation
// ---------------------------------------------------------------------------

func (h *Handler) DeleteGitHubInstallation(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	workspaceID := resolveWorkspaceID(r)
	installationIDStr := r.URL.Query().Get("installation_id")
	if installationIDStr == "" {
		writeError(w, http.StatusBadRequest, "installation_id is required")
		return
	}

	installationID, err := strconv.ParseInt(installationIDStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid installation_id")
		return
	}

	gi, err := h.Queries.GetGitHubInstallationByInstallationID(r.Context(), installationID)
	if err != nil {
		writeError(w, http.StatusNotFound, "installation not found")
		return
	}

	if uuidToString(gi.WorkspaceID) != workspaceID {
		writeError(w, http.StatusNotFound, "installation not found")
		return
	}

	h.Queries.DeleteGitHubInstallation(r.Context(), gi.ID)

	h.publish(protocol.EventGitHubInstallationDeleted, workspaceID, "member", userID, map[string]any{
		"installation_id": uuidToString(gi.ID),
	})

	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// REST API: List pull requests for an issue
// ---------------------------------------------------------------------------

func (h *Handler) ListIssuePullRequests(w http.ResponseWriter, r *http.Request) {
	issueID := r.URL.Query().Get("issue_id")
	if issueID == "" {
		writeError(w, http.StatusBadRequest, "issue_id is required")
		return
	}

	prs, err := h.Queries.ListPullRequestsByIssue(r.Context(), parseUUID(issueID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list pull requests")
		return
	}

	resp := make([]PullRequestResponse, len(prs))
	for i, pr := range prs {
		resp[i] = pullRequestToResponse(pr)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"pull_requests": resp,
	})
}
