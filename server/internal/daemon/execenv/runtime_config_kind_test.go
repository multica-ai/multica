package execenv

import (
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/pkg/featureflag"
)

// withSlimBrief enables the `runtime_brief_slim` feature flag for the
// duration of the test, then restores whatever provider was wired before.
// Tests that exercise the slim path must call this; everything else gets
// the default-off behaviour and exercises the legacy path.
//
// The helper is NOT t.Parallel-safe because runtimeFlags is a process-wide
// atomic.Pointer. Tests that need the slim path stay serial. Hardly any
// slim test takes more than a few ms — serial is fine.
func withSlimBrief(t *testing.T) {
	t.Helper()
	saved := runtimeFlags.Load()
	provider := featureflag.NewStaticProvider()
	provider.Set(runtimeBriefSlimFlag, featureflag.Rule{Default: true})
	runtimeFlags.Store(featureflag.NewService(provider))
	t.Cleanup(func() { runtimeFlags.Store(saved) })
}

// TestClassifyTask pins the precedence rule on classifyTask. All five
// kinds plus tiebreak cases for safety.
func TestClassifyTask(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		ctx  TaskContextForEnv
		want taskKind
	}{
		{"chat", TaskContextForEnv{ChatSessionID: "c"}, kindChat},
		{"quick-create", TaskContextForEnv{QuickCreatePrompt: "p"}, kindQuickCreate},
		{"autopilot", TaskContextForEnv{AutopilotRunID: "r"}, kindAutopilotRunOnly},
		{"comment-triggered", TaskContextForEnv{IssueID: "i", TriggerCommentID: "c"}, kindCommentTriggered},
		{"assignment-triggered", TaskContextForEnv{IssueID: "i"}, kindAssignmentTriggered},
		{"assignment-bare", TaskContextForEnv{}, kindAssignmentTriggered},
		{"tiebreak-chat-vs-quick", TaskContextForEnv{ChatSessionID: "c", QuickCreatePrompt: "p"}, kindChat},
		{"tiebreak-quick-vs-autopilot", TaskContextForEnv{QuickCreatePrompt: "p", AutopilotRunID: "r"}, kindQuickCreate},
		{"tiebreak-autopilot-vs-comment", TaskContextForEnv{AutopilotRunID: "r", IssueID: "i", TriggerCommentID: "c"}, kindAutopilotRunOnly},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := classifyTask(tc.ctx); got != tc.want {
				t.Errorf("classifyTask: got %d, want %d", got, tc.want)
			}
		})
	}
}

// TestTaskKindHasIssueContext pins the predicate that gates Project
// Context / Issue Metadata / Sub-issue Creation in the slim dispatcher.
func TestTaskKindHasIssueContext(t *testing.T) {
	t.Parallel()
	cases := []struct {
		kind taskKind
		want bool
	}{
		{kindCommentTriggered, true},
		{kindAssignmentTriggered, true},
		{kindAutopilotRunOnly, false},
		{kindQuickCreate, false},
		{kindChat, false},
	}
	for _, tc := range cases {
		if got := tc.kind.hasIssueContext(); got != tc.want {
			t.Errorf("kind=%d hasIssueContext: got %v, want %v", tc.kind, got, tc.want)
		}
	}
}

// TestSlimFlagOffUsesLegacy is the canary that production stays on the
// legacy brief by default. If a future change accidentally flips the flag
// default to true (or breaks the dispatcher), this test catches it before
// it ships.
func TestSlimFlagOffUsesLegacy(t *testing.T) {
	// Not parallel: reads runtimeFlags without enabling slim, so any
	// test that races us by enabling slim would invalidate the assertion.
	saved := runtimeFlags.Load()
	t.Cleanup(func() { runtimeFlags.Store(saved) })
	runtimeFlags.Store(nil)

	out := buildMetaSkillContent("claude", TaskContextForEnv{
		IssueID:          "issue-1",
		TriggerCommentID: "comment-1",
		AgentName:        "Eve",
		AgentID:          "eve-1",
	})

	// The legacy brief carries verbose Available Commands prose that the
	// slim brief drops — `Get full issue details.` is the legacy "issue
	// get" description, not in slim.
	if !strings.Contains(out, "Get full issue details.") {
		t.Errorf("flag-off path should render LEGACY brief, but the legacy `issue get` description is missing — did the dispatcher leak to slim?\n---\n%s", out)
	}
}

// TestSlimFlagOnUsesSlim is the symmetric canary: when the flag is on,
// buildMetaSkillContent must route to the slim path. Asserts a sentinel
// substring that exists only in the slim brief.
func TestSlimFlagOnUsesSlim(t *testing.T) {
	withSlimBrief(t)

	out := buildMetaSkillContent("claude", TaskContextForEnv{
		IssueID:          "issue-1",
		TriggerCommentID: "comment-1",
		AgentName:        "Eve",
		AgentID:          "eve-1",
	})

	// Slim Available Commands description for `issue get` is "full
	// issue."; legacy is "Get full issue details." Distinct enough that
	// either is decisive.
	if !strings.Contains(out, "- `multica issue get <id> --output json` — full issue.\n") {
		t.Errorf("flag-on path should render SLIM brief, but the slim `issue get` one-liner is missing\n---\n%s", out)
	}
	if strings.Contains(out, "Get full issue details.") {
		t.Errorf("flag-on path leaked the LEGACY `issue get` description into the slim brief\n---\n%s", out)
	}
}

// TestBuildMetaSkillContentSlimKindMatrix locks in which sections the
// slim brief emits per task kind, machine-checking the matrix documented
// on `buildMetaSkillContentSlim`. Heading is matched as a discrete line
// (preceded by newline + followed by newline) so inline references like
// "see ## Comment Formatting" do not trip the absence assertions.
func TestBuildMetaSkillContentSlimKindMatrix(t *testing.T) {
	withSlimBrief(t)

	baseRepo := []RepoContextForEnv{{URL: "https://example.com/x.git", Description: "x"}}
	baseSkill := []SkillContextForEnv{{Name: "skill-x", Description: "x"}}

	type sectionCheck struct {
		heading  string
		mustHave map[taskKind]bool
	}
	allKinds := map[taskKind]bool{
		kindCommentTriggered: true, kindAssignmentTriggered: true,
		kindAutopilotRunOnly: true, kindQuickCreate: true, kindChat: true,
	}
	issueKinds := map[taskKind]bool{
		kindCommentTriggered: true, kindAssignmentTriggered: true,
	}
	checks := []sectionCheck{
		{"# Multica Agent Runtime", allKinds},
		{"## Background Task Safety", allKinds},
		{"## Agent Identity", allKinds},
		{"## Available Commands", allKinds},
		{"### Workflow", allKinds},
		{"## Important: Always Use the `multica` CLI", allKinds},
		{"## Output", allKinds},
		{"## Comment Formatting", issueKinds},
		{"## Repositories", map[taskKind]bool{
			kindCommentTriggered: true, kindAssignmentTriggered: true,
			kindAutopilotRunOnly: true, kindChat: true,
		}},
		{"## Issue Metadata", issueKinds},
		{"## Instruction Precedence", map[taskKind]bool{kindAssignmentTriggered: true}},
		{"## Sub-issue Creation", issueKinds},
		{"## Skills", map[taskKind]bool{
			kindCommentTriggered: true, kindAssignmentTriggered: true,
			kindAutopilotRunOnly: true, kindChat: true,
		}},
		{"## Mentions", issueKinds},
		{"## Attachments", issueKinds},
	}

	fixtures := map[taskKind]TaskContextForEnv{
		kindChat: {ChatSessionID: "c-1", AgentName: "Eve", AgentID: "eve-1",
			Repos: baseRepo, AgentSkills: baseSkill},
		kindQuickCreate: {QuickCreatePrompt: "p", AgentName: "Eve", AgentID: "eve-1",
			Repos: baseRepo, AgentSkills: baseSkill},
		kindAutopilotRunOnly: {AutopilotRunID: "r-1", AgentName: "Eve", AgentID: "eve-1",
			Repos: baseRepo, AgentSkills: baseSkill},
		kindCommentTriggered: {IssueID: "i-1", TriggerCommentID: "tc-1",
			AgentName: "Eve", AgentID: "eve-1", Repos: baseRepo, AgentSkills: baseSkill},
		kindAssignmentTriggered: {IssueID: "i-1", AgentName: "Eve", AgentID: "eve-1",
			Repos: baseRepo, AgentSkills: baseSkill},
	}

	for kind, ctx := range fixtures {
		out := buildMetaSkillContent("claude", ctx)
		for _, c := range checks {
			needle := "\n" + c.heading + "\n"
			firstLine := c.heading + "\n"
			present := strings.HasPrefix(out, firstLine) || strings.Contains(out, needle)
			want := c.mustHave[kind]
			if want && !present {
				t.Errorf("kind=%d: expected heading %q in slim brief", kind, c.heading)
			}
			if !want && present {
				t.Errorf("kind=%d: heading %q should NOT be in slim brief (matrix gating regression)", kind, c.heading)
			}
		}
	}
}

// TestSlimQuickCreateAvailableCommands locks the minimal-variant content
// for quick-create's Available Commands: `issue create` present, every
// other Core command absent (the hard guardrails forbid the call).
func TestSlimQuickCreateAvailableCommands(t *testing.T) {
	withSlimBrief(t)

	out := buildMetaSkillContent("codex", TaskContextForEnv{
		QuickCreatePrompt: "create an issue about flaky tests",
		AgentName:         "Eve", AgentID: "eve-1",
	})

	for _, want := range []string{
		"## Available Commands",
		"multica issue create --title",
		"`multica --help`",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("quick_create slim Available Commands missing %q", want)
		}
	}

	for _, banned := range []string{
		"multica issue get <id>",
		"multica issue comment list <issue-id>",
		"multica issue update <id>",
		"multica issue status <id> <status>",
		"multica issue comment add <issue-id>",
		"multica issue metadata list <issue-id>",
		"multica issue metadata set <issue-id>",
		"multica issue metadata delete <issue-id>",
		"multica issue children <id>",
		"multica repo checkout <url>",
		"### Squad maintenance",
		"multica squad member set-role",
	} {
		if strings.Contains(out, banned) {
			t.Errorf("quick_create slim Available Commands should NOT advertise %q (hard guardrails forbid the call)", banned)
		}
	}
}

// TestSlimBriefIsSubstantiallyShorter is the headline check: on a
// realistic comment-triggered fixture, the slim brief is at least 30%
// shorter than the legacy brief. The exact number is in flux as we
// continue to tune; the assertion just guards against a future change
// that accidentally bloats the slim path back up to legacy levels.
func TestSlimBriefIsSubstantiallyShorter(t *testing.T) {
	// Not t.Parallel-safe because we toggle the global flag inside.
	ctx := TaskContextForEnv{
		IssueID: "11111111-2222-3333-4444-555555555555", TriggerCommentID: "66666666-7777-8888-9999-aaaaaaaaaaaa", TriggerThreadID: "66666666-7777-8888-9999-aaaaaaaaaaaa",
		AgentName: "Eve", AgentID: "eve-1",
		InitiatorName: "Yushen", InitiatorType: "member", InitiatorEmail: "yushen@devv.ai",
		Repos: []RepoContextForEnv{
			{URL: "https://github.com/multica-ai/multica", Description: "Managed agents platform"},
			{URL: "git@github.com:multica-ai/multica-cloud.git", Description: "Internal cloud platform"},
		},
		AgentSkills: []SkillContextForEnv{
			{Name: "Multica Git Workflow", Description: "Multica development workflow"},
			{Name: "PR review", Description: "Review PRs"},
		},
	}

	saved := runtimeFlags.Load()
	t.Cleanup(func() { runtimeFlags.Store(saved) })

	runtimeFlags.Store(nil)
	legacy := buildMetaSkillContent("claude", ctx)

	withSlimBrief(t)
	slim := buildMetaSkillContent("claude", ctx)

	if len(slim) >= len(legacy) {
		t.Fatalf("slim brief (%d chars) should be shorter than legacy (%d chars)", len(slim), len(legacy))
	}
	ratio := 1.0 - float64(len(slim))/float64(len(legacy))
	if ratio < 0.30 {
		t.Errorf("slim brief reduction is only %.1f%% (slim=%d, legacy=%d); expected >= 30%%", ratio*100, len(slim), len(legacy))
	}
	t.Logf("slim brief reduction: %.1f%% (legacy=%d, slim=%d, Δ=%d)", ratio*100, len(legacy), len(slim), len(legacy)-len(slim))
}
