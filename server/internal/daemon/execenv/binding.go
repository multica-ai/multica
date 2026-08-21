package execenv

import (
	"encoding/json"
	"sort"
	"strings"
)

// BindingRepoURLs returns the canonical repository URLs a task is bound to:
// workspace repos plus github_repo project resources. Order is sorted and
// de-duplicated so two tasks that bind the same set compare equal.
func BindingRepoURLs(repos []RepoContextForEnv, resources []ProjectResourceForEnv) []string {
	seen := map[string]struct{}{}
	var urls []string
	add := func(raw string) {
		u := canonicalizeRepoURL(raw)
		if u == "" {
			return
		}
		if _, ok := seen[u]; ok {
			return
		}
		seen[u] = struct{}{}
		urls = append(urls, u)
	}
	for _, repo := range repos {
		add(repo.URL)
	}
	for _, res := range resources {
		if !strings.EqualFold(strings.TrimSpace(res.ResourceType), "github_repo") {
			continue
		}
		add(githubRepoURL(res.ResourceRef))
	}
	sort.Strings(urls)
	return urls
}

func githubRepoURL(ref json.RawMessage) string {
	if len(ref) == 0 {
		return ""
	}
	var parsed struct {
		URL string `json:"url"`
	}
	if json.Unmarshal(ref, &parsed) != nil {
		return ""
	}
	return parsed.URL
}

func canonicalizeRepoURL(raw string) string {
	u := strings.TrimSpace(strings.ToLower(raw))
	u = strings.TrimSuffix(u, ".git")
	u = strings.TrimRight(u, "/")
	return u
}

// ProvenanceMatchesBinding reports whether a managed-env provenance file is
// safe to reuse for a task with this project and repository binding.
//
// Empty provenance project/repo fields are treated as "not yet recorded"
// (files written before this check existed) and do not fail the match — a
// follow-up on an older env root would otherwise start fresh on every
// machine that has not recycled the directory. Once both sides have a
// value, a mismatch fails closed.
func ProvenanceMatchesBinding(p *ManagedEnvProvenance, projectID string, repoURLs []string) bool {
	if p == nil {
		return false
	}
	wantProject := strings.TrimSpace(projectID)
	gotProject := strings.TrimSpace(p.ProjectID)
	if gotProject != "" && gotProject != wantProject {
		return false
	}
	if len(p.RepoURLs) == 0 {
		return true
	}
	want := append([]string(nil), repoURLs...)
	sort.Strings(want)
	got := append([]string(nil), p.RepoURLs...)
	sort.Strings(got)
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if canonicalizeRepoURL(got[i]) != canonicalizeRepoURL(want[i]) {
			return false
		}
	}
	return true
}
