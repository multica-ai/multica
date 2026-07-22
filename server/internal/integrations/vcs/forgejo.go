package vcs

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// forgejoProvider implements Provider for Forgejo and its upstream Gitea, which
// are wire-identical: same /api/v1 REST surface, same X-Gitea-Signature
// HMAC-SHA256 webhooks, same pull_request / status event shapes. One struct
// serves both; it is registered under KindForgejo and KindGitea so the only
// user-visible difference is the provider label.
type forgejoProvider struct{ kind Kind }

func init() {
	register(forgejoProvider{kind: KindForgejo})
	register(forgejoProvider{kind: KindGitea})
}

func (p forgejoProvider) Kind() Kind { return p.kind }

func (p forgejoProvider) EventKind(h http.Header) EventKind {
	event := h.Get("X-Gitea-Event")
	if event == "" {
		event = h.Get("X-GitHub-Event") // Gitea mirrors this header too
	}
	switch event {
	case "pull_request":
		return EventPullRequest
	case "status":
		return EventCIStatus
	default:
		return EventOther
	}
}

// VerifySignature checks X-Gitea-Signature, a bare hex HMAC-SHA256 of the body
// (no "sha256=" prefix — that is GitHub's convention; tolerate it anyway).
func (p forgejoProvider) VerifySignature(secret string, h http.Header, body []byte) bool {
	// HMAC with an empty key is forgeable, so reject an empty secret outright
	// (mirrors the GitLab verifier). Not reachable today — the secret is always
	// 32 random bytes — but keep the auth boundary safe regardless.
	if secret == "" {
		return false
	}
	sig := strings.TrimSpace(h.Get("X-Gitea-Signature"))
	sig = strings.TrimPrefix(sig, "sha256=")
	want, err := hex.DecodeString(sig)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), want)
}

type fjPullRequestPayload struct {
	Action      string `json:"action"`
	PullRequest struct {
		Number       int32  `json:"number"`
		Title        string `json:"title"`
		Body         string `json:"body"`
		State        string `json:"state"`
		Merged       bool   `json:"merged"`
		Draft        bool   `json:"draft"`
		HTMLURL      string `json:"html_url"`
		Additions    int32  `json:"additions"`
		Deletions    int32  `json:"deletions"`
		ChangedFiles int32  `json:"changed_files"`
		MergedAt     string `json:"merged_at"`
		ClosedAt     string `json:"closed_at"`
		CreatedAt    string `json:"created_at"`
		UpdatedAt    string `json:"updated_at"`
		User         struct {
			Login     string `json:"login"`
			UserName  string `json:"username"`
			AvatarURL string `json:"avatar_url"`
		} `json:"user"`
		Head struct {
			Ref string `json:"ref"`
			Sha string `json:"sha"`
		} `json:"head"`
	} `json:"pull_request"`
	Repository struct {
		Name     string `json:"name"`
		FullName string `json:"full_name"`
		Owner    struct {
			Login    string `json:"login"`
			UserName string `json:"username"`
		} `json:"owner"`
	} `json:"repository"`
}

func (p forgejoProvider) ParsePullRequest(body []byte) (PullRequestEvent, error) {
	var d fjPullRequestPayload
	if err := json.Unmarshal(body, &d); err != nil {
		return PullRequestEvent{}, err
	}
	owner := coalesce(d.Repository.Owner.UserName, d.Repository.Owner.Login)
	if owner == "" {
		if i := strings.Index(d.Repository.FullName, "/"); i > 0 {
			owner = d.Repository.FullName[:i]
		}
	}
	return PullRequestEvent{
		Action:          d.Action,
		RepoOwner:       owner,
		RepoName:        d.Repository.Name,
		Number:          d.PullRequest.Number,
		Title:           d.PullRequest.Title,
		Body:            d.PullRequest.Body,
		State:           derivePRState(d.PullRequest.State, d.PullRequest.Draft, d.PullRequest.Merged),
		HTMLURL:         d.PullRequest.HTMLURL,
		Branch:          d.PullRequest.Head.Ref,
		HeadSHA:         d.PullRequest.Head.Sha,
		AuthorLogin:     coalesce(d.PullRequest.User.UserName, d.PullRequest.User.Login),
		AuthorAvatarURL: d.PullRequest.User.AvatarURL,
		Additions:       d.PullRequest.Additions,
		Deletions:       d.PullRequest.Deletions,
		ChangedFiles:    d.PullRequest.ChangedFiles,
		MergedAt:        d.PullRequest.MergedAt,
		ClosedAt:        d.PullRequest.ClosedAt,
		CreatedAt:       d.PullRequest.CreatedAt,
		UpdatedAt:       d.PullRequest.UpdatedAt,
	}, nil
}

type fjStatusPayload struct {
	SHA         string `json:"sha"`
	Context     string `json:"context"`
	State       string `json:"state"`
	TargetURL   string `json:"target_url"`
	Description string `json:"description"`
	// Forgejo/Gitea send these as RFC3339 on the commit-status object.
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func (p forgejoProvider) ParseCIStatus(body []byte) (CIStatusEvent, error) {
	var d fjStatusPayload
	if err := json.Unmarshal(body, &d); err != nil {
		return CIStatusEvent{}, err
	}
	// Prefer the status' own updated_at (RFC3339) so the monotonic guard is
	// real; fall back to created_at, then empty (handler uses ingestion time).
	updatedAt := d.UpdatedAt
	if updatedAt == "" {
		updatedAt = d.CreatedAt
	}
	return CIStatusEvent{
		SHA:         d.SHA,
		Context:     d.Context,
		State:       normalizeForgejoState(d.State),
		TargetURL:   d.TargetURL,
		Description: d.Description,
		UpdatedAt:   updatedAt,
	}, nil
}

// normalizeForgejoState maps Forgejo/Gitea commit-status states onto the shared
// passed/failed/pending vocabulary. "warning" is treated as a pass (it does not
// block), mirroring how GitHub's neutral/skipped count as passed.
func normalizeForgejoState(s string) string {
	switch s {
	case "success", "warning":
		return "passed"
	case "failure", "error":
		return "failed"
	default: // pending, and anything unknown
		return "pending"
	}
}

func (p forgejoProvider) ValidateToken(ctx context.Context, instanceURL, token string) (Account, error) {
	endpoint := NormalizeInstanceURL(instanceURL) + "/api/v1/user"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Account{}, fmt.Errorf("forgejo: build request: %w", err)
	}
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return Account{}, fmt.Errorf("forgejo: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		// Log the upstream status + body snippet so a bad token (401) is
		// distinguishable from an insufficient-scope token (403) without
		// leaking the secret into the HTTP response.
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		slog.Warn("forgejo: token validation rejected",
			"endpoint", endpoint,
			"status", resp.StatusCode,
			"body", strings.TrimSpace(string(b)))
		return Account{}, ErrUnauthorized
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return Account{}, fmt.Errorf("forgejo: GET /user: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var u struct {
		Login    string `json:"login"`
		UserName string `json:"username"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return Account{}, fmt.Errorf("forgejo: decode user: %w", err)
	}
	login := coalesce(u.Login, u.UserName)
	if login == "" {
		return Account{}, errors.New("forgejo: user response missing login")
	}
	return Account{Login: login}, nil
}

// ── shared helpers ──────────────────────────────────────────────────────────

var httpClient = &http.Client{Timeout: 15 * time.Second}

// NormalizeInstanceURL trims whitespace and any trailing slash so stored
// instance URLs and derived webhook URLs are stable regardless of input.
func NormalizeInstanceURL(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}

// derivePRState collapses (state, draft, merged) into the normalized PR state.
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

func coalesce(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
