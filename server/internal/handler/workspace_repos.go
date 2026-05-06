package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// WorkspaceRepoEntry is the canonical wire/storage shape for a single repo
// entry under workspace.repos JSONB. Status is "pending" or "approved";
// "approved" is the only state that participates in project pickers and
// daemon claim responses.
type WorkspaceRepoEntry struct {
	URL    string `json:"url"`
	Status string `json:"status,omitempty"`
}

const (
	repoStatusPending  = "pending"
	repoStatusApproved = "approved"
)

func parseRepoEntries(reposJSON []byte) []WorkspaceRepoEntry {
	if len(reposJSON) == 0 {
		return nil
	}
	var entries []WorkspaceRepoEntry
	if err := json.Unmarshal(reposJSON, &entries); err != nil {
		return nil
	}
	return entries
}

// isApprovedStatus accepts "approved" and (defensively) "" — the latter
// covers any unmigrated row written before the status field was introduced.
func isApprovedStatus(s string) bool {
	return s == "" || s == repoStatusApproved
}

func normalizeRepoURL(s string) string {
	return strings.TrimSpace(s)
}

// scpURLPattern matches the SCP-style git URL syntax: `[user@]host:path`.
// Per git-clone(8), the path part must NOT start with a slash (otherwise the
// URL is parsed as scheme-less http). The host must contain no slashes.
var scpURLPattern = regexp.MustCompile(`^(?:[A-Za-z0-9._-]+@)?([A-Za-z0-9.\-]+):[^/].*$`)

// extractRepoHost returns the hostname for either an http(s) URL or an
// SCP-style git URL (e.g. `git@github.com:org/repo.git`). Empty string +
// error means the input wasn't a recognizable repo URL.
func extractRepoHost(raw string) (string, error) {
	if m := scpURLPattern.FindStringSubmatch(raw); m != nil {
		return m[1], nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https" && parsed.Scheme != "ssh" && parsed.Scheme != "git") || parsed.Host == "" {
		return "", errors.New("repo url must be http(s)://, ssh://, git://, or git@host:path/repo.git")
	}
	return parsed.Hostname(), nil
}

// validateRepoURL ensures the URL is a recognizable repo URL (http(s), ssh,
// git, or SCP-style `git@host:path`) and (when configured) that its host is
// on the allowlist. Returns the normalized URL.
func (h *Handler) validateRepoURL(raw string) (string, error) {
	u := normalizeRepoURL(raw)
	if u == "" {
		return "", errors.New("repo url is required")
	}
	host, err := extractRepoHost(u)
	if err != nil {
		return "", err
	}
	if len(h.cfg.AllowedRepoDomains) > 0 {
		host = strings.ToLower(host)
		ok := false
		for _, d := range h.cfg.AllowedRepoDomains {
			d = strings.ToLower(strings.TrimSpace(d))
			if d == "" {
				continue
			}
			if host == d || strings.HasSuffix(host, "."+d) {
				ok = true
				break
			}
		}
		if !ok {
			return "", fmt.Errorf("repo domain %q is not allowed", host)
		}
	}
	return u, nil
}

// normalizeIncomingRepos merges a client-submitted repos array against the
// existing stored array:
//   - URLs absent from `existing` are new → status set per RepoApprovalRequired.
//   - URLs already present in `existing` keep their server-side status (clients
//     cannot upgrade pending → approved through this endpoint).
//
// Domain validation runs over every entry; duplicates within the request are
// collapsed to the first occurrence.
func (h *Handler) normalizeIncomingRepos(incoming []WorkspaceRepoEntry, existing []WorkspaceRepoEntry) ([]WorkspaceRepoEntry, error) {
	prevStatus := make(map[string]string, len(existing))
	for _, e := range existing {
		prevStatus[normalizeRepoURL(e.URL)] = e.Status
	}

	defaultStatus := repoStatusApproved
	if h.cfg.RepoApprovalRequired {
		defaultStatus = repoStatusPending
	}

	seen := make(map[string]struct{}, len(incoming))
	out := make([]WorkspaceRepoEntry, 0, len(incoming))
	for _, e := range incoming {
		u, err := h.validateRepoURL(e.URL)
		if err != nil {
			return nil, err
		}
		if _, dup := seen[u]; dup {
			continue
		}
		seen[u] = struct{}{}

		status := defaultStatus
		if prev, ok := prevStatus[u]; ok && prev != "" {
			status = prev
		}
		out = append(out, WorkspaceRepoEntry{URL: u, Status: status})
	}
	return out, nil
}

// loadApprovedWorkspaceRepoURLs fetches the workspace and returns the set of
// attachable repo URLs. When RepoApprovalRequired is on, only "approved"
// entries qualify. When off, status is meaningless so every entry is
// attachable — including legacy "pending" rows from a prior config flip.
func (h *Handler) loadApprovedWorkspaceRepoURLs(ctx context.Context, wsID string) (map[string]struct{}, error) {
	wsUUID, err := parseUUIDLoose(wsID)
	if err != nil {
		return nil, err
	}
	ws, err := h.Queries.GetWorkspace(ctx, wsUUID)
	if err != nil {
		return nil, err
	}
	entries := parseRepoEntries(ws.Repos)
	set := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		if h.cfg.RepoApprovalRequired && !isApprovedStatus(e.Status) {
			continue
		}
		set[normalizeRepoURL(e.URL)] = struct{}{}
	}
	return set, nil
}

// approvedWorkspaceRepoData converts the JSONB blob to the daemon-facing
// RepoData slice. When RepoApprovalRequired is on, only "approved" entries
// are returned. When off, every entry is returned — status is irrelevant.
func (h *Handler) approvedWorkspaceRepoData(reposJSON []byte) []RepoData {
	entries := parseRepoEntries(reposJSON)
	if len(entries) == 0 {
		return nil
	}
	out := make([]RepoData, 0, len(entries))
	for _, e := range entries {
		if h.cfg.RepoApprovalRequired && !isApprovedStatus(e.Status) {
			continue
		}
		out = append(out, RepoData{URL: normalizeRepoURL(e.URL)})
	}
	return out
}

// findRepoIndex locates a repo by URL, returning -1 if not present.
func findRepoIndex(entries []WorkspaceRepoEntry, target string) int {
	t := normalizeRepoURL(target)
	for i, e := range entries {
		if normalizeRepoURL(e.URL) == t {
			return i
		}
	}
	return -1
}
