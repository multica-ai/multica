package handler

import (
	"encoding/json"
	"net/http"
	"strings"
)

// gitHubProvider is the GitProvider implementation for GitHub.com. It owns the
// GitHub-specific webhook edges — HMAC verification, X-GitHub-Event decoding,
// and PR-state mapping — while the neutral mirror pipeline in github.go drives
// the rest. The webhook secret is read from the environment on demand (same
// source as the Connect/state-token flow), so the struct is stateless.
type gitHubProvider struct{}

func newGitHubProvider() gitHubProvider { return gitHubProvider{} }

func (gitHubProvider) Name() string { return "github" }

func (gitHubProvider) InstanceBaseURL() string { return "https://github.com" }

func (gitHubProvider) DerivePRState(state string, draft, merged bool) string {
	return derivePRState(state, draft, merged)
}

func (gitHubProvider) VerifySignature(headers http.Header, body []byte) bool {
	secret := githubWebhookSecret()
	if secret == "" {
		return false
	}
	return verifyWebhookSignature(secret, headers.Get("X-Hub-Signature-256"), body)
}

func (gitHubProvider) ParseEvent(headers http.Header, body []byte) (GitEvent, error) {
	switch headers.Get("X-GitHub-Event") {
	case "ping":
		return GitEvent{Kind: GitEventPing}, nil
	case "installation":
		var p ghInstallationPayload
		if err := json.Unmarshal(body, &p); err != nil {
			return GitEvent{}, err
		}
		login, accountType, avatar, _ := githubInstallationAccountFromPayload(p)
		return GitEvent{Kind: GitEventInstallation, Installation: &GitInstallationEvent{
			Action:           p.Action,
			InstallationID:   p.Installation.ID,
			AccountLogin:     login,
			AccountType:      accountType,
			AccountAvatarURL: avatar,
		}}, nil
	case "pull_request":
		var p ghPullRequestPayload
		if err := json.Unmarshal(body, &p); err != nil {
			return GitEvent{}, err
		}
		return GitEvent{Kind: GitEventPullRequest, PullRequest: &GitPullRequestEvent{
			Action:          p.Action,
			InstallationID:  p.Installation.ID,
			RepoOwner:       p.Repository.Owner.Login,
			RepoName:        p.Repository.Name,
			Number:          p.PullRequest.Number,
			HTMLURL:         p.PullRequest.HTMLURL,
			Title:           p.PullRequest.Title,
			Body:            p.PullRequest.Body,
			State:           p.PullRequest.State,
			Draft:           p.PullRequest.Draft,
			Merged:          p.PullRequest.Merged,
			MergedAt:        p.PullRequest.MergedAt,
			ClosedAt:        p.PullRequest.ClosedAt,
			CreatedAt:       p.PullRequest.CreatedAt,
			UpdatedAt:       p.PullRequest.UpdatedAt,
			MergeableState:  p.PullRequest.MergeableState,
			BaseRefChanged:  baseRefChanged(p.Changes),
			Additions:       p.PullRequest.Additions,
			Deletions:       p.PullRequest.Deletions,
			ChangedFiles:    p.PullRequest.ChangedFiles,
			HeadRef:         p.PullRequest.Head.Ref,
			HeadSHA:         p.PullRequest.Head.SHA,
			AuthorLogin:     p.PullRequest.User.Login,
			AuthorAvatarURL: p.PullRequest.User.AvatarURL,
		}}, nil
	case "check_suite":
		var p ghCheckSuitePayload
		if err := json.Unmarshal(body, &p); err != nil {
			return GitEvent{}, err
		}
		numbers := make([]int32, 0, len(p.CheckSuite.PullRequests))
		for _, pr := range p.CheckSuite.PullRequests {
			numbers = append(numbers, pr.Number)
		}
		return GitEvent{Kind: GitEventCheckSuite, CheckSuite: &GitCheckSuiteEvent{
			Action:         p.Action,
			InstallationID: p.Installation.ID,
			RepoOwner:      p.Repository.Owner.Login,
			RepoName:       p.Repository.Name,
			SuiteID:        p.CheckSuite.ID,
			HeadSHA:        p.CheckSuite.HeadSHA,
			Status:         p.CheckSuite.Status,
			Conclusion:     p.CheckSuite.Conclusion,
			UpdatedAt:      p.CheckSuite.UpdatedAt,
			AppID:          p.CheckSuite.App.ID,
			PRNumbers:      numbers,
		}}, nil
	default:
		return GitEvent{Kind: GitEventUnknown}, nil
	}
}

// ── GitHub webhook payloads ─────────────────────────────────────────────────

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
