package daemon

import (
	"testing"
)

// TestResolveWorkspaceRepoURLMatchesAcrossURLForms pins the normalization that
// lets a checkout request in one URL form match a repo registered in another
// (http vs ssh vs scp shorthand). Agents routinely paste the HTTP address
// from the Git host's web UI while the workspace registers the SSH form.
func TestResolveWorkspaceRepoURLMatchesAcrossURLForms(t *testing.T) {
	d := newGCTestDaemon(t, nil)

	const wsID = "11111111-1111-1111-1111-111111111111"
	const registered = "git@gitlab.example.com:group/project.git"
	const taskURL = "ssh://git@gitlab.example.com:2222/group/task-only.git"
	d.mu.Lock()
	d.workspaces[wsID] = &workspaceState{
		workspaceID: wsID,
		allowedRepoURLs: map[string]struct{}{
			registered:              {},
			"/local/repo":           {},
			"https://bad%host/repo": {},
		},
		taskRepoURLs: map[string]struct{}{
			taskURL: {},
		},
	}
	d.mu.Unlock()

	for _, tc := range []struct {
		name string
		url  string
		want string
	}{
		{"exact registered form", registered, registered},
		{"https form of registered ssh repo", "https://gitlab.example.com/group/project.git", registered},
		{"http form of registered ssh repo", "http://gitlab.example.com/group/project.git", registered},
		{"ssh form of registered scp shorthand", "ssh://git@gitlab.example.com/group/project.git", registered},
		{"trailing slash and .git differences", "https://gitlab.example.com/group/project/", registered},
		{"host and suffix case", "https://GITLAB.EXAMPLE.COM/group/project.GIT", registered},
		{"default http port", "http://gitlab.example.com:80/group/project.git", registered},
		{"default https port", "https://gitlab.example.com:443/group/project.git", registered},
		{"default ssh port", "ssh://git@gitlab.example.com:22/group/project.git", registered},
		{"default git port", "git://gitlab.example.com:9418/group/project.git", registered},
		{"exact task repo form", taskURL, taskURL},
		{"https form of task repo keeps non-default port", "https://gitlab.example.com:2222/group/task-only.git", taskURL},
		{"same host path without the task repo's port", "https://gitlab.example.com/group/task-only.git", ""},
		{"non-default https port", "https://gitlab.example.com:8443/group/project.git", ""},
		{"http using https port is not default", "http://gitlab.example.com:443/group/project.git", ""},
		{"different repo on the same host", "https://gitlab.example.com/group/other.git", ""},
		{"different host", "https://other.example.com/group/project.git", ""},
		{"different path case", "https://gitlab.example.com/group/Project.git", ""},
		{"local path exact match", "/local/repo", "/local/repo"},
		{"local path stays exact only", "/local/repo.git", ""},
		{"malformed URL exact match", "https://bad%host/repo", "https://bad%host/repo"},
		{"malformed URL stays exact only", "https://bad%host/repo.git", ""},
		{"empty URL", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := d.resolveWorkspaceRepoURL(wsID, tc.url)
			if got != tc.want {
				t.Errorf("resolveWorkspaceRepoURL(%q) = %q, want %q", tc.url, got, tc.want)
			}
		})
	}

	if d.resolveWorkspaceRepoURL("22222222-2222-2222-2222-222222222222", registered) != "" {
		t.Error("an unwatched workspace must not allow the repo")
	}
}

func TestResolveWorkspaceRepoURLPrefersExactThenStableSource(t *testing.T) {
	d := newGCTestDaemon(t, nil)
	const workspaceURL = "https://gitlab.example.com/group/project.git"
	const taskURL = "git@gitlab.example.com:group/project.git"
	ws := newWorkspaceState("ws-1", nil, "", []RepoData{{URL: workspaceURL}}, nil)
	ws.taskRepoURLs = map[string]struct{}{taskURL: {}}
	d.workspaces["ws-1"] = ws
	if got := d.resolveWorkspaceRepoURL("ws-1", taskURL); got != taskURL {
		t.Fatalf("exact task URL = %q, want %q", got, taskURL)
	}
	const alias = "ssh://git@gitlab.example.com/group/project.git"
	if got := d.resolveWorkspaceRepoURL("ws-1", alias); got != workspaceURL {
		t.Fatalf("alias = %q, want workspace binding %q", got, workspaceURL)
	}
	// Old workspace data can contain aliases that the settings UI now dedupes.
	ws.allowedRepoURLs[taskURL] = struct{}{}
	for range 50 {
		if got := d.resolveWorkspaceRepoURL("ws-1", alias); got != taskURL {
			t.Fatalf("ambiguous alias = %q, want lexically first URL %q", got, taskURL)
		}
	}
}

func TestTaskRepoDefaultRefMatchesOnlyCurrentTaskAliases(t *testing.T) {
	d := newGCTestDaemon(t, nil)
	const scpURL = "git@gitlab.example.com:group/project.git"
	const httpsURL = "https://gitlab.example.com/group/project.git"
	const alias = "ssh://git@gitlab.example.com:22/group/project.git"
	d.workspaces["ws-1"] = &workspaceState{taskRepoRefs: map[string]map[string]string{
		"task-a": {scpURL: "release/a", httpsURL: "release/https"},
		"task-b": {scpURL: "release/b"},
	}}
	for _, tc := range []struct{ task, url, want string }{
		{"task-a", httpsURL, "release/https"},
		{"task-a", alias, "release/a"},
		{"task-b", httpsURL, "release/b"},
		{"task-c", httpsURL, ""},
		{"task-a", "https://gitlab.example.com:8443/group/project.git", ""},
	} {
		if got := d.taskRepoDefaultRef("ws-1", tc.task, tc.url); got != tc.want {
			t.Errorf("taskRepoDefaultRef(%q, %q) = %q, want %q", tc.task, tc.url, got, tc.want)
		}
	}
}
