package execenv

import (
	"strings"
	"testing"
)

// A project pins a repo to a branch because its work lives on that line
// (MUL-7504). The daemon already checks the repo out there, so the two things
// the BRIEF has to add are the ones the agent cannot infer from a working
// directory: which line it is on, and that delivery goes back to the same one.
//
// The second matters most. Nothing in the platform creates pull requests —
// `gh pr create` is the agent's own call, and without --base it targets the
// repository's default branch. A PR opened that way asks to merge into the
// wrong place AND carries every commit the pinned branch has that the default
// lacks.
func TestRepositoriesSectionCarriesPinnedRefAndDeliveryTarget(t *testing.T) {
	t.Parallel()
	ctx := TaskContextForEnv{
		IssueID: "i-1", AgentName: "Eve", AgentID: "eve-1",
		Repos: []RepoContextForEnv{
			{URL: "https://github.com/multica-ai/multica", Ref: "release/2026-09"},
		},
	}
	out := buildMetaSkillContent("claude", ctx)

	if !strings.Contains(out, "https://github.com/multica-ai/multica (starts from `release/2026-09`)") {
		t.Errorf("Repositories section should name the pinned ref beside its repo; got:\n%s", out)
	}
	if !strings.Contains(out, "gh pr create --base") {
		t.Errorf("brief should tell the agent to target the same branch when delivering; got:\n%s", out)
	}
	// A pin is already applied on checkout. An agent that re-passes --ref to
	// "get back to" the branch is doing redundant work at best, and pinning a
	// stale value at worst.
	if !strings.Contains(out, "do not pass `--ref` to get back to it") {
		t.Errorf("brief should say the checkout is already at the pinned ref; got:\n%s", out)
	}
}

// The delivery rule is stated only when something is actually pinned. An
// unpinned workspace repo already starts from its default branch, and
// `gh pr create` already targets that — an unconditional --base instruction
// would be noise in the common case.
func TestRepositoriesSectionOmitsDeliveryRuleWhenNothingIsPinned(t *testing.T) {
	t.Parallel()
	ctx := TaskContextForEnv{
		IssueID: "i-1", AgentName: "Eve", AgentID: "eve-1",
		Repos: []RepoContextForEnv{
			{URL: "https://github.com/multica-ai/multica", Description: "the platform"},
		},
	}
	out := buildMetaSkillContent("claude", ctx)

	if !strings.Contains(out, "https://github.com/multica-ai/multica — the platform") {
		t.Errorf("unpinned repo should still render its description; got:\n%s", out)
	}
	if strings.Contains(out, "starts from") || strings.Contains(out, "gh pr create --base") {
		t.Errorf("no repo is pinned, so the brief must not mention a starting point or --base; got:\n%s", out)
	}
}

// A pin is not necessarily a branch — the field accepts anything git resolves,
// and neither the server nor the daemon can tell a branch from a tag or a
// commit without asking the remote, which the product deliberately does not do.
// A tag has no branch to merge back into, so the delivery rule has to be stated
// conditionally rather than as "always --base whatever is pinned".
func TestDeliveryRuleDoesNotAssumeThePinIsABranch(t *testing.T) {
	t.Parallel()
	ctx := TaskContextForEnv{
		IssueID: "i-1", AgentName: "Eve", AgentID: "eve-1",
		Repos: []RepoContextForEnv{{URL: "https://github.com/o/r", Ref: "v1.4.0"}},
	}
	out := buildMetaSkillContent("claude", ctx)

	if !strings.Contains(out, "tag or a commit rather than a branch") {
		t.Errorf("brief should carve out the tag/commit case; got:\n%s", out)
	}
	if !strings.Contains(out, "confirm the target branch") {
		t.Errorf("brief should tell the agent to confirm rather than guess a base; got:\n%s", out)
	}
}

// The Project Context bullet used to read as though --ref were how you reach
// the configured revision. It is not: the daemon applies it, and --ref is the
// override. An agent following the old wording would re-specify a value it
// already had — and would keep using a stale one after the project changed.
func TestProjectContextSaysThePinIsAlreadyApplied(t *testing.T) {
	t.Parallel()
	ctx := TaskContextForEnv{
		IssueID: "i-1", AgentName: "Eve", AgentID: "eve-1",
		ProjectID: "p-1", ProjectTitle: "Release line",
		ProjectResources: []ProjectResourceForEnv{{
			ResourceType: "github_repo",
			ResourceRef:  []byte(`{"url":"https://github.com/o/r","ref":"release/2026-09"}`),
		}},
	}
	out := buildMetaSkillContent("claude", ctx)

	if !strings.Contains(out, "checked out there automatically") {
		t.Errorf("Project Context should say the configured start is applied for you; got:\n%s", out)
	}
	if !strings.Contains(out, "only to override it") {
		t.Errorf("Project Context should frame --ref as the override; got:\n%s", out)
	}
}

// A resumed task can outlive a change to the project's starting point. The
// checkout is kept as it was — holding work branched off the OLD value — while
// the brief, rebuilt from the current claim, names the new one. An agent that
// retargeted on the brief alone would deliver the old line's work into the new
// line, and would contradict what the edit dialog promises: work already
// underway keeps the branch it started on.
//
// The brief cannot resolve this itself; whether a checkout was reused is only
// known once `repo checkout` runs. So it warns, and is careful about what it
// claims the checkout can tell the agent — see the next test.
func TestDeliveryRuleWarnsAboutAKeptCheckout(t *testing.T) {
	t.Parallel()
	ctx := TaskContextForEnv{
		IssueID: "i-1", AgentName: "Eve", AgentID: "eve-1",
		Repos: []RepoContextForEnv{{URL: "https://github.com/o/r", Ref: "release/b"}},
	}
	out := buildMetaSkillContent("claude", ctx)

	if !strings.Contains(out, "KEPT an existing checkout") {
		t.Errorf("brief should name the kept-checkout case; got:\n%s", out)
	}
	if !strings.Contains(out, "Do not retarget it to a starting point listed above") {
		t.Errorf("brief should say the listed starting point is not authoritative on a resume; got:\n%s", out)
	}
}

// A kept checkout reports the branch the worktree is ON — the task's own
// `agent/...` branch, resolved by `git symbolic-ref`. That is the HEAD of a
// pull request, never its base, and nothing in the result carries the ref the
// branch was cut from.
//
// An earlier revision of this text said to deliver "to the branch it actually
// started from, which the checkout names". An agent following that would read
// the `agent/...` branch and pass it as --base, producing a pull request whose
// base equals its head. The brief must not name the checkout as a source for
// the original target at all.
func TestKeptCheckoutIsNotOfferedAsTheDeliveryTarget(t *testing.T) {
	t.Parallel()
	ctx := TaskContextForEnv{
		IssueID: "i-1", AgentName: "Eve", AgentID: "eve-1",
		Repos: []RepoContextForEnv{{URL: "https://github.com/o/r", Ref: "release/b"}},
	}
	out := buildMetaSkillContent("claude", ctx)

	if strings.Contains(out, "which the checkout names") {
		t.Errorf("brief must not point at the checkout for the original target; got:\n%s", out)
	}
	if !strings.Contains(out, "head of a pull request, never its base") {
		t.Errorf("brief should say what the reported branch actually is; got:\n%s", out)
	}
	// The sources that DO settle it.
	if !strings.Contains(out, "the base of its existing pull request") {
		t.Errorf("brief should point at the existing PR's base; got:\n%s", out)
	}
	if !strings.Contains(out, "ask if neither settles it") {
		t.Errorf("brief should tell the agent to ask rather than guess; got:\n%s", out)
	}
}

// The resume warning does NOT ride with the pin. A project cleared back to its
// default branch renders no starting point at all, yet a task resumed after
// that change still holds a checkout cut from the old one — so gating the
// warning on `pinned` would drop it exactly where the mismatch is invisible.
func TestResumeWarningSurvivesTheStartingPointBeingCleared(t *testing.T) {
	t.Parallel()
	ctx := TaskContextForEnv{
		IssueID: "i-1", AgentName: "Eve", AgentID: "eve-1",
		Repos: []RepoContextForEnv{{URL: "https://github.com/o/r"}}, // A -> cleared
	}
	out := buildMetaSkillContent("claude", ctx)

	if !strings.Contains(out, "KEPT an existing checkout") {
		t.Errorf("resume warning must survive a cleared starting point; got:\n%s", out)
	}
	// The pinned-only halves stay out: there is no branch to target, and
	// nothing listed above to be warned off retargeting to.
	if strings.Contains(out, "gh pr create --base") {
		t.Errorf("nothing is pinned, so the --base rule is noise; got:\n%s", out)
	}
	if strings.Contains(out, "Do not retarget it to a starting point listed above") {
		t.Errorf("nothing is listed above, so the sentence points at nothing; got:\n%s", out)
	}
}

// No repos, nothing to say — the section is skipped whole.
func TestResumeWarningAbsentWithoutRepos(t *testing.T) {
	t.Parallel()
	out := buildMetaSkillContent("claude", TaskContextForEnv{
		IssueID: "i-1", AgentName: "Eve", AgentID: "eve-1",
	})
	if strings.Contains(out, "KEPT an existing checkout") {
		t.Errorf("no repositories, so no checkout guidance; got:\n%s", out)
	}
}
