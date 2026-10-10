package vcs

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Gongfeng uses v3 REST APIs and X-Event/X-Token webhook headers. Its MR
// target.web_url carries repository identity; target.name is a display name.
type gongfengProvider struct{}

func init() { register(gongfengProvider{}) }

func (gongfengProvider) Kind() Kind { return KindGongfeng }

func (gongfengProvider) EventKind(h http.Header) EventKind {
	if h.Get("X-Event") == "Merge Request Hook" {
		return EventPullRequest
	}
	return EventOther
}

func (gongfengProvider) VerifySignature(secret string, h http.Header, _ []byte) bool {
	return secret != "" && subtle.ConstantTimeCompare([]byte(h.Get("X-Token")), []byte(secret)) == 1
}

type gongfengMRPayload struct {
	ObjectKind string `json:"object_kind"`
	User       struct {
		Username  string `json:"username"`
		AvatarURL string `json:"avatar_url"`
	} `json:"user"`
	ObjectAttributes struct {
		IID            int32  `json:"iid"`
		Title          string `json:"title"`
		Description    string `json:"description"`
		State          string `json:"state"`
		Action         string `json:"action"`
		SourceBranch   string `json:"source_branch"`
		URL            string `json:"url"`
		WorkInProgress bool   `json:"work_in_progress"`
		CreatedAt      string `json:"created_at"`
		UpdatedAt      string `json:"updated_at"`
		Target         struct {
			WebURL string `json:"web_url"`
		} `json:"target"`
		LastCommit struct {
			ID string `json:"id"`
		} `json:"last_commit"`
	} `json:"object_attributes"`
}

func (gongfengProvider) ParsePullRequest(body []byte) (PullRequestEvent, error) {
	var payload gongfengMRPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return PullRequestEvent{}, err
	}
	a := payload.ObjectAttributes
	if payload.ObjectKind != "merge_request" || a.IID <= 0 {
		return PullRequestEvent{}, errors.New("gongfeng: invalid merge request identity")
	}
	repoURL, err := gongfengWebURL(a.Target.WebURL)
	if err != nil {
		return PullRequestEvent{}, fmt.Errorf("gongfeng: target repository: %w", err)
	}
	owner, name := splitNamespace(repoURL.Path)
	if owner == "" || name == "" {
		return PullRequestEvent{}, errors.New("gongfeng: target repository path missing namespace or name")
	}
	if _, err := gongfengWebURL(a.URL); err != nil {
		return PullRequestEvent{}, fmt.Errorf("gongfeng: merge request URL: %w", err)
	}
	createdAt, err := parseGongfengTime(a.CreatedAt)
	if err != nil {
		return PullRequestEvent{}, err
	}
	updatedAt, err := parseGongfengTime(a.UpdatedAt)
	if err != nil {
		return PullRequestEvent{}, err
	}
	var state string
	switch a.State {
	case "opened", "reopened":
		state = "open"
		title := strings.ToLower(a.Title)
		if a.WorkInProgress || strings.HasPrefix(title, "wip:") || strings.HasPrefix(title, "draft:") {
			state = "draft"
		}
	case "closed", "merged":
		state = a.State
	default:
		return PullRequestEvent{}, fmt.Errorf("gongfeng: unknown MR state %q", a.State)
	}
	pr := PullRequestEvent{
		Action: a.Action, RepoOwner: owner, RepoName: name, Number: a.IID,
		Title: a.Title, Body: a.Description, State: state, HTMLURL: a.URL,
		Branch: a.SourceBranch, HeadSHA: a.LastCommit.ID,
		CreatedAt: createdAt, UpdatedAt: updatedAt,
	}
	// The hook's user is the actor. Only the creation event identifies the
	// author; updates/merges must not overwrite it with the actor's identity.
	if a.Action == "open" {
		pr.AuthorLogin, pr.AuthorAvatarURL = payload.User.Username, payload.User.AvatarURL
	}
	if state == "merged" {
		pr.MergedAt = updatedAt
	}
	if state == "closed" {
		pr.ClosedAt = updatedAt
	}
	return pr, nil
}

func gongfengWebURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("expected an absolute HTTP(S) URL without credentials, query or fragment")
	}
	return u, nil
}

func parseGongfengTime(raw string) (string, error) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999-0700"} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.UTC().Format(time.RFC3339Nano), nil
		}
	}
	// Missing/invalid event times would bypass the shared stale-event guard
	// through ingestion-time fallback, so reject them at the provider boundary.
	return "", fmt.Errorf("gongfeng: invalid event timestamp %q", raw)
}

func (gongfengProvider) ParseCIStatus(_ []byte) (CIStatusEvent, error) {
	return CIStatusEvent{}, errors.New("gongfeng: CI events are not supported")
}

func (gongfengProvider) ValidateToken(ctx context.Context, instanceURL, token string) (Account, error) {
	if _, err := gongfengWebURL(instanceURL); err != nil {
		return Account{}, fmt.Errorf("gongfeng: instance URL: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, NormalizeInstanceURL(instanceURL)+"/api/v3/user", nil)
	if err != nil {
		return Account{}, fmt.Errorf("gongfeng: build request: %w", err)
	}
	req.Header.Set("PRIVATE-TOKEN", token)
	req.Header.Set("Accept", "application/json")
	// PRIVATE-TOKEN is not one of net/http's sensitive redirect headers.
	// Do not forward it to a redirect destination chosen by the instance.
	client := *httpClient
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return Account{}, fmt.Errorf("gongfeng: request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return Account{}, ErrUnauthorized
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Account{}, fmt.Errorf("gongfeng: GET /user: status %d", resp.StatusCode)
	}
	var user struct {
		Username string `json:"username"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return Account{}, fmt.Errorf("gongfeng: decode user: %w", err)
	}
	if user.Username == "" {
		return Account{}, errors.New("gongfeng: user response missing username")
	}
	return Account{Login: user.Username}, nil
}
