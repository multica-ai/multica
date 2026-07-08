package handler

import (
	"encoding/json"
	"net/http"
	"bytes"
)

func (h *Handler) GetGitHubUserRepos(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	user, err := h.Queries.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to get user")
		return
	}
	if !user.GithubAccessToken.Valid || user.GithubAccessToken.String == "" {
		writeError(w, http.StatusUnauthorized, "GitHub account not connected")
		return
	}

	// Fetch repos from GitHub
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, "https://api.github.com/user/repos?sort=updated&per_page=100", nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to create request")
		return
	}
	req.Header.Set("Authorization", "Bearer "+user.GithubAccessToken.String)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Failed to fetch repos from GitHub")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		writeError(w, http.StatusBadGateway, "GitHub returned an error")
		return
	}

	var repos []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&repos); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to parse repos")
		return
	}

	// We can filter or map these if necessary, but returning the list directly is fine
	writeJSON(w, http.StatusOK, repos)
}

type createGitHubIssueRequest struct {
	RepoOwner   string `json:"repo_owner"`
	RepoName    string `json:"repo_name"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

func (h *Handler) CreateGitHubIssue(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var reqBody createGitHubIssueRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if reqBody.RepoOwner == "" || reqBody.RepoName == "" || reqBody.Title == "" {
		writeError(w, http.StatusBadRequest, "Missing required fields")
		return
	}

	user, err := h.Queries.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to get user")
		return
	}
	if !user.GithubAccessToken.Valid || user.GithubAccessToken.String == "" {
		writeError(w, http.StatusUnauthorized, "GitHub account not connected")
		return
	}

	ghReqBody, _ := json.Marshal(map[string]string{
		"title": reqBody.Title,
		"body":  reqBody.Description,
	})

	url := "https://api.github.com/repos/" + reqBody.RepoOwner + "/" + reqBody.RepoName + "/issues"
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, url, bytes.NewReader(ghReqBody))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to create request")
		return
	}
	req.Header.Set("Authorization", "Bearer "+user.GithubAccessToken.String)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Failed to create issue on GitHub")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		writeError(w, http.StatusBadGateway, "GitHub returned an error")
		return
	}

	var issue map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&issue); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to parse issue")
		return
	}

	// Maybe we want to also create a placeholder issue in Multica right away?
	// The user asked for "if we want to create a GitHub issue or normal tasks".
	// If they create a GitHub issue, they probably expect it to show up in Multica.
	// But it will show up when the webhook comes in if the repo is connected.
	// We can return the GitHub issue response.
	
	writeJSON(w, http.StatusOK, issue)
}
