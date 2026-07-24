package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// giteaProvider is the GitProvider implementation for a self-hosted Gitea
// instance. It owns the Gitea-specific webhook edges — X-Gitea-Signature
// verification, X-Gitea-Event decoding, and PR-state mapping — while the
// neutral mirror pipeline in github.go drives the rest.
//
// Unlike GitHub (a single github.com singleton), a Gitea provider is scoped to
// one instance: baseURL is the scheme+host derived from the delivery's repo
// html_url and stamps the base_host on mirrored rows, so the same
// owner/name/number on different instances stay distinct.
type giteaProvider struct {
	baseURL string
}

func newGiteaProvider(baseURL string) giteaProvider { return giteaProvider{baseURL: baseURL} }

func (giteaProvider) Name() string { return "gitea" }

func (p giteaProvider) InstanceBaseURL() string { return p.baseURL }

func (giteaProvider) DerivePRState(state string, draft, merged bool) string {
	return derivePRState(state, draft, merged)
}

func (giteaProvider) VerifySignature(headers http.Header, body []byte) bool {
	secret := giteaWebhookSecret()
	if secret == "" {
		return false
	}
	return verifyGiteaSignature(secret, headers.Get("X-Gitea-Signature"), body)
}

func (giteaProvider) ParseEvent(headers http.Header, body []byte) (GitEvent, error) {
	switch headers.Get("X-Gitea-Event") {
	case "ping":
		return GitEvent{Kind: GitEventPing}, nil
	case "pull_request":
		var p giteaPullRequestPayload
		if err := json.Unmarshal(body, &p); err != nil {
			return GitEvent{}, err
		}
		return GitEvent{Kind: GitEventPullRequest, PullRequest: &GitPullRequestEvent{
			Action: p.Action,
			// Gitea has no installation id; the mirror pipeline attributes the
			// delivery by instance base URL instead (handleGiteaPullRequestEvent).
			InstallationID:  0,
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
			MergeableState:  giteaMergeableState(p.PullRequest.Mergeable),
			BaseRefChanged:  false,
			Additions:       p.PullRequest.Additions,
			Deletions:       p.PullRequest.Deletions,
			ChangedFiles:    p.PullRequest.ChangedFiles,
			HeadRef:         p.PullRequest.Head.Ref,
			HeadSHA:         p.PullRequest.Head.SHA,
			AuthorLogin:     p.PullRequest.User.Login,
			AuthorAvatarURL: p.PullRequest.User.AvatarURL,
		}}, nil
	default:
		return GitEvent{Kind: GitEventUnknown}, nil
	}
}

// giteaWebhookSecret is the shared HMAC secret configured on each Gitea repo's
// webhook. One value across the deployment (mirroring GITHUB_WEBHOOK_SECRET):
// the operator sets the same secret when wiring each repo's webhook.
func giteaWebhookSecret() string { return strings.TrimSpace(os.Getenv("MULTICA_GITEA_WEBHOOK_SECRET")) }

// verifyGiteaSignature checks Gitea's X-Gitea-Signature header, a raw hex
// HMAC-SHA256 of the body (no "sha256=" prefix, unlike GitHub's
// X-Hub-Signature-256).
func verifyGiteaSignature(secret, sigHex string, body []byte) bool {
	if sigHex == "" {
		return false
	}
	want, err := hex.DecodeString(sigHex)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), want)
}

// giteaMergeableState maps Gitea's boolean `mergeable` to the clean/dirty
// vocabulary the PR mirror already stores (GitHub ships an enum string; Gitea
// ships a bool — the difference the task called out). A nil pointer means the
// payload omitted the field, which stays unknown rather than defaulting to a
// false "conflict" verdict.
func giteaMergeableState(mergeable *bool) string {
	if mergeable == nil {
		return ""
	}
	if *mergeable {
		return "clean"
	}
	return "dirty"
}

// giteaInstanceBaseURL derives the instance identity (scheme://host) from a
// delivery's repository html_url. Empty when the body has no parseable repo
// URL — the delivery then matches no connection and is dropped. Safe to call
// on an unverified body: it only extracts a routing string and takes no action.
func giteaInstanceBaseURL(body []byte) string {
	var p struct {
		Repository struct {
			HTMLURL string `json:"html_url"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return ""
	}
	u, err := url.Parse(p.Repository.HTMLURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// ── Gitea webhook payloads ──────────────────────────────────────────────────

type giteaPullRequestPayload struct {
	Action      string `json:"action"`
	PullRequest struct {
		Number       int32  `json:"number"`
		HTMLURL      string `json:"html_url"`
		Title        string `json:"title"`
		Body         string `json:"body"`
		State        string `json:"state"`
		Draft        bool   `json:"draft"`
		Merged       bool   `json:"merged"`
		Mergeable    *bool  `json:"mergeable"`
		MergedAt     string `json:"merged_at"`
		ClosedAt     string `json:"closed_at"`
		CreatedAt    string `json:"created_at"`
		UpdatedAt    string `json:"updated_at"`
		Additions    int32  `json:"additions"`
		Deletions    int32  `json:"deletions"`
		ChangedFiles int32  `json:"changed_files"`
		Head         struct {
			Ref string `json:"ref"`
			SHA string `json:"sha"`
		} `json:"head"`
		User struct {
			Login     string `json:"login"`
			AvatarURL string `json:"avatar_url"`
		} `json:"user"`
	} `json:"pull_request"`
	Repository struct {
		Name  string `json:"name"`
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
}
