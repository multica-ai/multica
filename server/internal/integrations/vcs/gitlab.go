package vcs

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// gitlabProvider implements Provider for GitLab, which differs from
// Forgejo/Gitea on every axis: /api/v4 with a PRIVATE-TOKEN header, webhooks
// authenticated by a plaintext X-Gitlab-Token compare (no HMAC), an
// X-Gitlab-Event header, "merge request" terminology, and pipeline events for
// CI. The normalized PullRequestEvent/CIStatusEvent hide all of that from the
// handler.
type gitlabProvider struct{}

func init() { register(gitlabProvider{}) }

func (gitlabProvider) Kind() Kind { return KindGitLab }

func (gitlabProvider) EventKind(h http.Header) EventKind {
	switch h.Get("X-Gitlab-Event") {
	case "Merge Request Hook":
		return EventPullRequest
	case "Pipeline Hook":
		return EventCIStatus
	default:
		return EventOther
	}
}

// VerifySignature compares the X-Gitlab-Token header to the stored secret in
// constant time. GitLab does not HMAC-sign webhook bodies; the shared token is
// the whole authentication, so an empty stored secret never validates.
func (gitlabProvider) VerifySignature(secret string, h http.Header, _ []byte) bool {
	if secret == "" {
		return false
	}
	got := h.Get("X-Gitlab-Token")
	return subtle.ConstantTimeCompare([]byte(got), []byte(secret)) == 1
}

type glMergeRequestPayload struct {
	ObjectKind string `json:"object_kind"`
	User       struct {
		Username  string `json:"username"`
		AvatarURL string `json:"avatar_url"`
	} `json:"user"`
	Project struct {
		PathWithNamespace string `json:"path_with_namespace"`
	} `json:"project"`
	ObjectAttributes struct {
		IID            int32  `json:"iid"`
		Title          string `json:"title"`
		Description    string `json:"description"`
		State          string `json:"state"` // opened|closed|merged|locked
		Action         string `json:"action"`
		SourceBranch   string `json:"source_branch"`
		URL            string `json:"url"`
		Draft          bool   `json:"draft"`
		WorkInProgress bool   `json:"work_in_progress"`
		CreatedAt      string `json:"created_at"`
		UpdatedAt      string `json:"updated_at"`
		LastCommit     struct {
			ID string `json:"id"`
		} `json:"last_commit"`
	} `json:"object_attributes"`
}

func (gitlabProvider) ParsePullRequest(body []byte) (PullRequestEvent, error) {
	var d glMergeRequestPayload
	if err := json.Unmarshal(body, &d); err != nil {
		return PullRequestEvent{}, err
	}
	owner, name := splitNamespace(d.Project.PathWithNamespace)
	draft := d.ObjectAttributes.Draft || d.ObjectAttributes.WorkInProgress ||
		strings.HasPrefix(strings.ToLower(d.ObjectAttributes.Title), "draft:")
	return PullRequestEvent{
		Action:          d.ObjectAttributes.Action,
		RepoOwner:       owner,
		RepoName:        name,
		Number:          d.ObjectAttributes.IID,
		Title:           d.ObjectAttributes.Title,
		Body:            d.ObjectAttributes.Description,
		State:           normalizeGitLabMRState(d.ObjectAttributes.State, draft),
		HTMLURL:         d.ObjectAttributes.URL,
		Branch:          d.ObjectAttributes.SourceBranch,
		HeadSHA:         d.ObjectAttributes.LastCommit.ID,
		AuthorLogin:     d.User.Username,
		AuthorAvatarURL: d.User.AvatarURL,
		CreatedAt:       d.ObjectAttributes.CreatedAt,
		UpdatedAt:       d.ObjectAttributes.UpdatedAt,
	}, nil
}

// normalizeGitLabMRState maps GitLab MR states onto open/closed/merged/draft.
// "locked" is a transient open sub-state, so it reads as open.
func normalizeGitLabMRState(state string, draft bool) string {
	switch state {
	case "merged":
		return "merged"
	case "closed":
		return "closed"
	default: // opened, locked
		if draft {
			return "draft"
		}
		return "open"
	}
}

type glPipelinePayload struct {
	ObjectKind       string `json:"object_kind"`
	ObjectAttributes struct {
		SHA    string `json:"sha"`
		Status string `json:"status"`
		URL    string `json:"url"`
	} `json:"object_attributes"`
}

func (gitlabProvider) ParseCIStatus(body []byte) (CIStatusEvent, error) {
	var d glPipelinePayload
	if err := json.Unmarshal(body, &d); err != nil {
		return CIStatusEvent{}, err
	}
	return CIStatusEvent{
		SHA: d.ObjectAttributes.SHA,
		// GitLab pipelines are one per commit, not per named check, so a stable
		// synthetic context keys the single status row.
		Context:   "gitlab/pipeline",
		State:     normalizeGitLabPipelineState(d.ObjectAttributes.Status),
		TargetURL: d.ObjectAttributes.URL,
	}, nil
}

// normalizeGitLabPipelineState maps pipeline statuses onto passed/failed/
// pending. skipped is a pass (nothing failed); canceled is a failure-class
// terminal, matching how GitHub treats cancelled.
func normalizeGitLabPipelineState(s string) string {
	switch s {
	case "success", "skipped":
		return "passed"
	case "failed", "canceled":
		return "failed"
	default: // created, waiting_for_resource, preparing, pending, running, manual, scheduled
		return "pending"
	}
}

func (gitlabProvider) ValidateToken(ctx context.Context, instanceURL, token string) (Account, error) {
	endpoint := NormalizeInstanceURL(instanceURL) + "/api/v4/user"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Account{}, fmt.Errorf("gitlab: build request: %w", err)
	}
	req.Header.Set("PRIVATE-TOKEN", token)
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return Account{}, fmt.Errorf("gitlab: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return Account{}, ErrUnauthorized
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return Account{}, fmt.Errorf("gitlab: GET /user: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var u struct {
		Username string `json:"username"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return Account{}, fmt.Errorf("gitlab: decode user: %w", err)
	}
	if u.Username == "" {
		return Account{}, errors.New("gitlab: user response missing username")
	}
	return Account{Login: u.Username}, nil
}

// splitNamespace splits a GitLab path_with_namespace ("group/subgroup/repo")
// into owner ("group/subgroup") and repo name ("repo"). Subgroups are kept in
// the owner so the identity stays unique.
func splitNamespace(path string) (owner, name string) {
	path = strings.Trim(path, "/")
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[:i], path[i+1:]
	}
	return "", path
}
