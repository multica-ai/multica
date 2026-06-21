package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
)

var mrCmd = &cobra.Command{
	Use:   "mr",
	Short: "Work with merge requests",
}

var mrCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a merge request (optionally push first)",
	RunE:  runMRCreate,
}

func init() {
	mrCmd.AddCommand(mrCreateCmd)

	mrCreateCmd.Flags().String("source-branch", "", "Source branch (required)")
	mrCreateCmd.Flags().String("title", "", "Merge request title (required)")
	mrCreateCmd.Flags().String("description", "", "Merge request description")
	mrCreateCmd.Flags().String("target-branch", "main", "Target branch")
	mrCreateCmd.Flags().Bool("push", true, "Push the source branch before creating the MR")
}

func runMRCreate(cmd *cobra.Command, _ []string) error {
	sourceBranch, _ := cmd.Flags().GetString("source-branch")
	if sourceBranch == "" {
		return fmt.Errorf("--source-branch is required")
	}

	title, _ := cmd.Flags().GetString("title")
	if title == "" {
		return fmt.Errorf("--title is required")
	}

	description, _ := cmd.Flags().GetString("description")
	targetBranch, _ := cmd.Flags().GetString("target-branch")
	shouldPush, _ := cmd.Flags().GetBool("push")

	// Read environment variables.
	token := os.Getenv("MULTICA_TOKEN")
	if token == "" {
		return fmt.Errorf("MULTICA_TOKEN environment variable is required")
	}
	serverURL := os.Getenv("MULTICA_SERVER_URL")
	if serverURL == "" {
		return fmt.Errorf("MULTICA_SERVER_URL environment variable is required")
	}
	workspaceID := os.Getenv("MULTICA_WORKSPACE_ID")
	if workspaceID == "" {
		return fmt.Errorf("MULTICA_WORKSPACE_ID environment variable is required")
	}

	// Trim trailing slash from server URL.
	serverURL = strings.TrimRight(serverURL, "/")

	// Step 1: Fetch GitLab credential from Multica server.
	cred, err := fetchGitlabCredential(serverURL, token, workspaceID)
	if err != nil {
		return err
	}

	// Step 2: Determine the GitLab base URL and remote URL.
	gitlabBase := strings.TrimRight(cred.BaseURL, "/")
	remoteURL, err := getGitRemoteURL()
	if err != nil {
		return fmt.Errorf("detect remote URL: %w", err)
	}
	gitlabBase = deriveGitlabBase(remoteURL, gitlabBase)

	// Step 3: Push the source branch if requested.
	if shouldPush {
		if err := pushBranch(remoteURL, sourceBranch, buildAuthURL(remoteURL, cred.Token)); err != nil {
			return fmt.Errorf("push branch: %w", err)
		}
	}

	// Step 4: Find the GitLab project by its path from the remote URL.
	repoPath := strings.TrimSuffix(extractPathFromRemote(remoteURL), ".git")
	projectID, err := findProject(gitlabBase, cred.Token, repoPath)
	if err != nil {
		return err
	}

	// Step 5: Create the merge request.
	mrURL, err := createMergeRequest(gitlabBase, cred.Token, projectID, sourceBranch, targetBranch, title, description)
	if err != nil {
		return err
	}

	fmt.Println(mrURL)
	return nil
}

func fetchGitlabCredential(serverURL, token, workspaceID string) (struct {
	BaseURL string `json:"base_url"`
	Token   string `json:"token"`
}, error) {
	var cred struct {
		BaseURL string `json:"base_url"`
		Token   string `json:"token"`
	}

	credReq, err := http.NewRequest(http.MethodGet, serverURL+"/api/gitlab/credential", nil)
	if err != nil {
		return cred, fmt.Errorf("build credential request: %w", err)
	}
	credReq.Header.Set("Authorization", "Bearer "+token)
	credReq.Header.Set("X-Workspace-ID", workspaceID)

	credResp, err := http.DefaultClient.Do(credReq)
	if err != nil {
		return cred, fmt.Errorf("fetch GitLab credential: %w", err)
	}
	defer credResp.Body.Close()

	if credResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(credResp.Body)
		return cred, fmt.Errorf("fetch GitLab credential: HTTP %d: %s", credResp.StatusCode, strings.TrimSpace(string(body)))
	}

	if err := json.NewDecoder(credResp.Body).Decode(&cred); err != nil {
		return cred, fmt.Errorf("parse credential response: %w", err)
	}

	if cred.BaseURL == "" || cred.Token == "" {
		return cred, fmt.Errorf("invalid credential response: base_url and token are required")
	}

	return cred, nil
}

// getGitRemoteURL returns the fetch URL of the "origin" remote.
func getGitRemoteURL() (string, error) {
	out, err := exec.Command("git", "remote", "get-url", "origin").Output()
	if err != nil {
		return "", fmt.Errorf("git remote get-url origin: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// deriveGitlabBase extracts the GitLab instance URL from a remote URL.
// Falls back to the server-provided base URL.
func deriveGitlabBase(remoteURL, fallback string) string {
	base := extractBaseFromRemote(remoteURL)
	if base != "" {
		return base
	}
	return fallback
}

// extractBaseFromRemote tries to extract the GitLab instance base URL from a
// git remote URL. Handles HTTPS, SSH, and git:// protocols.
// Examples:
//
//	https://gitlab.com/group/project.git → https://gitlab.com
//	git@gitlab.com:group/project.git     → https://gitlab.com
//	http://host.docker.internal:8929/group/project.git → http://host.docker.internal:8929
func extractBaseFromRemote(remoteURL string) string {
	// HTTPS / HTTP: https://host/path → https://host
	if strings.HasPrefix(remoteURL, "https://") || strings.HasPrefix(remoteURL, "http://") {
		u, err := url.Parse(remoteURL)
		if err != nil {
			return ""
		}
		return fmt.Sprintf("%s://%s", u.Scheme, u.Host)
	}

	// SSH: git@host:path → https://host
	sshRe := regexp.MustCompile(`^git@([^:]+):(.+)$`)
	if m := sshRe.FindStringSubmatch(remoteURL); m != nil {
		return "https://" + m[1]
	}

	// git:// protocol
	if strings.HasPrefix(remoteURL, "git://") {
		u, err := url.Parse(remoteURL)
		if err != nil {
			return ""
		}
		return "https://" + u.Host
	}

	return ""
}

// buildAuthURL inserts the PAT into the remote URL for authenticated git push.
//
//	https://gitlab.com/group/project.git → https://oauth2:<token>@gitlab.com/group/project.git
func buildAuthURL(remoteURL, token string) string {
	if !strings.HasPrefix(remoteURL, "http://") && !strings.HasPrefix(remoteURL, "https://") {
		// For SSH URLs, convert to HTTPS with auth.
		base := extractBaseFromRemote(remoteURL)
		path := extractPathFromRemote(remoteURL)
		if base != "" && path != "" {
			return base + "/" + strings.TrimSuffix(path, ".git") + ".git"
		}
		return remoteURL
	}
	u, err := url.Parse(remoteURL)
	if err != nil {
		return remoteURL
	}
	u.User = url.UserPassword("oauth2", token)
	return u.String()
}

// extractPathFromRemote extracts the repo path from a remote URL.
func extractPathFromRemote(remoteURL string) string {
	if strings.HasPrefix(remoteURL, "https://") || strings.HasPrefix(remoteURL, "http://") {
		u, err := url.Parse(remoteURL)
		if err != nil {
			return ""
		}
		return strings.TrimPrefix(u.Path, "/")
	}
	sshRe := regexp.MustCompile(`^git@[^:]+:(.+)$`)
	if m := sshRe.FindStringSubmatch(remoteURL); m != nil {
		return m[1]
	}
	return ""
}

// pushBranch pushes the source branch to the remote using the authenticated URL.
func pushBranch(remoteURL, branch, authURL string) error {
	fmt.Fprintf(os.Stderr, "Pushing %s...\n", branch)
	cmd := exec.Command("git", "push", authURL, branch)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git push: %w", err)
	}
	return nil
}

func findProject(gitlabBase, token, projectPath string) (int, error) {
	gitlabAPI := gitlabBase + "/api/v4"
	// Use URL-encoded project path to look up the project directly.
	encodedPath := url.PathEscape(projectPath)
	lookupURL := fmt.Sprintf("%s/projects/%s", gitlabAPI, encodedPath)
	projReq, err := http.NewRequest(http.MethodGet, lookupURL, nil)
	if err != nil {
		return 0, fmt.Errorf("build project lookup request: %w", err)
	}
	projReq.Header.Set("PRIVATE-TOKEN", token)

	projResp, err := http.DefaultClient.Do(projReq)
	if err != nil {
		return 0, fmt.Errorf("lookup project: %w", err)
	}
	defer projResp.Body.Close()

	if projResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(projResp.Body)
		return 0, fmt.Errorf("lookup project: HTTP %d: %s", projResp.StatusCode, strings.TrimSpace(string(body)))
	}

	var project struct {
		ID int `json:"id"`
	}
	if err := json.NewDecoder(projResp.Body).Decode(&project); err != nil {
		return 0, fmt.Errorf("parse project response: %w", err)
	}

	return project.ID, nil
}

func createMergeRequest(gitlabBase, token string, projectID int, sourceBranch, targetBranch, title, description string) (string, error) {
	gitlabAPI := gitlabBase + "/api/v4"

	mrBody := map[string]string{
		"source_branch": sourceBranch,
		"target_branch": targetBranch,
		"title":         title,
	}
	if description != "" {
		mrBody["description"] = description
	}

	mrJSON, err := json.Marshal(mrBody)
	if err != nil {
		return "", fmt.Errorf("marshal MR body: %w", err)
	}

	mrURL := fmt.Sprintf("%s/projects/%d/merge_requests", gitlabAPI, projectID)
	mrReq, err := http.NewRequest(http.MethodPost, mrURL, strings.NewReader(string(mrJSON)))
	if err != nil {
		return "", fmt.Errorf("build MR create request: %w", err)
	}
	mrReq.Header.Set("PRIVATE-TOKEN", token)
	mrReq.Header.Set("Content-Type", "application/json")

	mrResp, err := http.DefaultClient.Do(mrReq)
	if err != nil {
		return "", fmt.Errorf("create merge request: %w", err)
	}
	defer mrResp.Body.Close()

	if mrResp.StatusCode < 200 || mrResp.StatusCode >= 300 {
		body, _ := io.ReadAll(mrResp.Body)
		return "", fmt.Errorf("create merge request: HTTP %d: %s", mrResp.StatusCode, strings.TrimSpace(string(body)))
	}

	var mrResult struct {
		WebURL string `json:"web_url"`
	}
	if err := json.NewDecoder(mrResp.Body).Decode(&mrResult); err != nil {
		return "", fmt.Errorf("parse MR response: %w", err)
	}

	return mrResult.WebURL, nil
}
