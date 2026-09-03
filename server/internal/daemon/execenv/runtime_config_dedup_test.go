package execenv

import (
	"encoding/json"
	"strings"
	"testing"
)

func githubResource(url, label string) ProjectResourceForEnv {
	ref, _ := json.Marshal(map[string]string{"url": url})
	return ProjectResourceForEnv{ResourceType: "github_repo", ResourceRef: ref, Label: label}
}

// The brief is messages[0]. A field that changes between runs changes the
// cached prefix, which throws away the prompt cache for the whole resumed
// history (MUL-5377) — the same reason the issue workflow carries no
// per-run identifier. Autopilot run data belongs in the per-turn prompt,
// which already carries every one of these fields.
func TestAutopilotBriefIsIdenticalAcrossRuns(t *testing.T) {
	t.Parallel()

	first := buildMetaSkillContentSlim("claude", TaskContextForEnv{
		AutopilotRunID:          "run-1",
		AutopilotID:             "ap-1",
		AutopilotTitle:          "Nightly sync",
		AutopilotSource:         "schedule",
		AutopilotTriggerPayload: `{"ref":"main"}`,
		AutopilotDescription:    "Sync the mirror.",
	})
	second := buildMetaSkillContentSlim("claude", TaskContextForEnv{
		AutopilotRunID:          "run-2",
		AutopilotID:             "ap-2",
		AutopilotTitle:          "Hourly report",
		AutopilotSource:         "webhook",
		AutopilotTriggerPayload: `{"ref":"release"}`,
		AutopilotDescription:    "Post the report.",
	})

	if first != second {
		t.Fatalf("the brief still varies with per-run autopilot data:\n--- run 1 ---\n%s\n--- run 2 ---\n%s", first, second)
	}
	for _, leaked := range []string{"run-1", "ap-1", "Nightly sync", "schedule", `{"ref":"main"}`, "Sync the mirror."} {
		if strings.Contains(first, leaked) {
			t.Errorf("per-run value %q is still rendered into the brief", leaked)
		}
	}
}

// The guard is the one autopilot line that must stay: the brief is its
// single emission point, and buildAutopilotPrompt deliberately does not
// restate it (MUL-5696).
func TestAutopilotBriefKeepsTheIssueCommandGuard(t *testing.T) {
	t.Parallel()

	brief := buildMetaSkillContentSlim("claude", TaskContextForEnv{AutopilotRunID: "run-1"})
	if !strings.Contains(brief, AutopilotIssueCommandsGuard) {
		t.Fatalf("autopilot brief dropped the issue-command guard:\n%s", brief)
	}
	if !strings.Contains(brief, "run-only mode") {
		t.Fatalf("autopilot brief dropped the run-only framing:\n%s", brief)
	}
}

// A repo carried by a project resource is already rendered in Project
// Context with its ref, branch hint and label. The bare URL in
// ## Repositories says strictly less and costs tokens twice.
func TestRepositoriesOmitsReposAlreadyInProjectContext(t *testing.T) {
	t.Parallel()

	ctx := TaskContextForEnv{
		IssueID: "i1",
		Repos: []RepoContextForEnv{
			{URL: "https://github.com/acme/api", Description: "workspace copy"},
			{URL: "https://github.com/acme/tools"},
		},
		ProjectID:        "p1",
		ProjectResources: []ProjectResourceForEnv{githubResource("https://github.com/acme/api", "API")},
	}
	brief := buildMetaSkillContentSlim("claude", ctx)

	if strings.Count(brief, "https://github.com/acme/api") != 1 {
		t.Errorf("the project repo appears %d times, want exactly 1:\n%s",
			strings.Count(brief, "https://github.com/acme/api"), brief)
	}
	// Unmatched workspace repos are still reachable and must survive.
	if !strings.Contains(brief, "https://github.com/acme/tools") {
		t.Errorf("an unmatched workspace repo was dropped:\n%s", brief)
	}
	if !strings.Contains(brief, "## Repositories") {
		t.Errorf("the section vanished even though a repo was left to list:\n%s", brief)
	}
}

// Whole-section elision: with nothing left after dedup, a heading and its
// one-line preamble would be pure overhead.
func TestRepositoriesSectionElidedWhenEveryRepoIsAProjectResource(t *testing.T) {
	t.Parallel()

	brief := buildMetaSkillContentSlim("claude", TaskContextForEnv{
		IssueID:          "i1",
		Repos:            []RepoContextForEnv{{URL: "https://github.com/acme/api"}},
		ProjectID:        "p1",
		ProjectResources: []ProjectResourceForEnv{githubResource("https://github.com/acme/api", "API")},
	})

	if strings.Contains(brief, "## Repositories") {
		t.Errorf("empty Repositories section still rendered:\n%s", brief)
	}
	if strings.Count(brief, "https://github.com/acme/api") != 1 {
		t.Errorf("project repo should still appear once, in Project Context:\n%s", brief)
	}
}

// The two sides store the same repository differently in practice. Matching
// only exact strings would miss the pair that motivated the dedup.
func TestProjectRepoDedupIgnoresGitSuffixAndTrailingSlash(t *testing.T) {
	t.Parallel()

	for name, resourceURL := range map[string]string{
		"git suffix":     "https://github.com/acme/api.git",
		"trailing slash": "https://github.com/acme/api/",
		"mixed case":     "https://github.com/Acme/API",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			brief := buildMetaSkillContentSlim("claude", TaskContextForEnv{
				IssueID:          "i1",
				Repos:            []RepoContextForEnv{{URL: "https://github.com/acme/api"}},
				ProjectID:        "p1",
				ProjectResources: []ProjectResourceForEnv{githubResource(resourceURL, "API")},
			})
			if strings.Contains(brief, "## Repositories") {
				t.Errorf("%s was not recognized as the same repo:\n%s", name, brief)
			}
		})
	}
}

// Narrow on purpose: normalization must never merge two repositories that
// only look alike.
func TestProjectRepoDedupKeepsDistinctRepos(t *testing.T) {
	t.Parallel()

	brief := buildMetaSkillContentSlim("claude", TaskContextForEnv{
		IssueID:          "i1",
		Repos:            []RepoContextForEnv{{URL: "https://github.com/acme/api-v2"}},
		ProjectID:        "p1",
		ProjectResources: []ProjectResourceForEnv{githubResource("https://github.com/acme/api", "API")},
	})

	if !strings.Contains(brief, "https://github.com/acme/api-v2") {
		t.Errorf("a different repo was deduped away:\n%s", brief)
	}
}

// A non-github resource carries no URL to match on; it must not silently
// hide a workspace repo.
func TestRepositoriesUnaffectedByNonGithubResources(t *testing.T) {
	t.Parallel()

	ref, _ := json.Marshal(map[string]string{"path": "/srv/app"})
	brief := buildMetaSkillContentSlim("claude", TaskContextForEnv{
		IssueID:   "i1",
		Repos:     []RepoContextForEnv{{URL: "https://github.com/acme/api"}},
		ProjectID: "p1",
		ProjectResources: []ProjectResourceForEnv{
			{ResourceType: "local_directory", ResourceRef: ref, Label: "app"},
		},
	})

	if !strings.Contains(brief, "https://github.com/acme/api") {
		t.Errorf("workspace repo dropped by an unrelated resource type:\n%s", brief)
	}
}
