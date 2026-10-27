package execenv

import (
	"regexp"
	"strings"
	"testing"
)

// availableCommandsSection cuts `## Available Commands` out of a brief by its
// line-start heading, so a phrase that also appears in another section (the
// Comment Formatting rules, the Repositories checkout note) cannot satisfy an
// assertion meant for this one.
func availableCommandsSection(t *testing.T, brief string) string {
	t.Helper()
	start := strings.Index(brief, "\n## Available Commands\n")
	if start < 0 {
		t.Fatalf("brief has no ## Available Commands section:\n%s", brief)
	}
	rest := brief[start+1:]
	if end := strings.Index(rest[len("## Available Commands\n"):], "\n## "); end >= 0 {
		return rest[:len("## Available Commands\n")+end]
	}
	return rest
}

// availableCommandsKinds is every task kind that renders the full (non
// quick-create) Available Commands section. The section is shared verbatim
// across them, so a pointer it carries has to resolve in each one, not only in
// the issue brief.
var availableCommandsKinds = map[string]TaskContextForEnv{
	"issue":        {IssueID: "issue-1", AgentID: "a-1", AgentName: "Eve"},
	"issue-reply":  {IssueID: "issue-1", TriggerCommentID: "c-1", AgentID: "a-1", AgentName: "Eve"},
	"chat":         {ChatSessionID: "chat-1", AgentID: "a-1", AgentName: "Eve"},
	"chat-slack":   {ChatSessionID: "chat-1", ChatChannelType: ChannelTypeSlack, AgentID: "a-1", AgentName: "Eve"},
	"autopilot":    {AutopilotRunID: "run-1", AutopilotID: "ap-1", AgentID: "a-1", AgentName: "Eve"},
	"squad-leader": {IssueID: "issue-1", IsSquadLeader: true, AgentID: "a-1", AgentName: "Eve"},
}

// TestAvailableCommandsKeepsCompressedContracts pins what MUL-8292 kept when it
// moved command detail out of the brief and into `--help` and the
// multica-platform references. Each entry is a behaviour the agent needs at
// the moment it reaches for the command, not something it can look up after
// the fact; the removed parts (wakeup condition catalogue, PR snapshot
// mechanics, actor-filter recipe) are covered where the pointer leads.
//
// Every kind and provider is checked: chat and autopilot carry the same
// section but no Workflow or Comment Formatting, so a rule kept only as a
// pointer into those sections would silently disappear there.
func TestAvailableCommandsKeepsCompressedContracts(t *testing.T) {
	t.Parallel()
	for kind, ctx := range availableCommandsKinds {
		for _, provider := range []string{"claude", "codex"} {
			kind, ctx, provider := kind, ctx, provider
			t.Run(kind+"/"+provider, func(t *testing.T) {
				t.Parallel()
				ac := availableCommandsSection(t, buildMetaSkillContent(provider, ctx))
				for _, want := range []string{
					// wakeup: the run ends after registering; conditions beat
					// polling; last_error is scoped to the failures it reports.
					"persist an event, condition or time wakeup on this issue, then finish the current run",
					"use one `--until-*` condition so no run starts before it holds",
					"check the rule's `last_error` for missing PR links or snapshots, which are retried automatically",
					"`multica issue wakeup create --help` and the multica-platform issues reference",
					// comment list: the two bounded reads, the THREADS trap and
					// the pagination pointer.
					"scan with `--roots-only --summary`, then expand one thread with `--thread <id> --tail N`",
					"it caps THREADS, not comments",
					"paging cursors, and full flag semantics: `--help`",
					// issue create: long descriptions go through a workdir file.
					"Put agent-authored long descriptions in `--description-file <path>`",
					"heredoc stdin can swallow trailing flags, #4182",
					"write that file inside your working directory, never `/tmp` or shared paths",
					// duplicate mark: why a plain cancel is not enough.
					"`multica issue status <id> cancelled --duplicate-of <original>`",
					"a plain cancel leaves no link",
					// comment add: the file-first mandate stays at the call site.
					"Agent-authored bodies MUST use `--content-file` with a file inside your working directory",
					// repo checkout: both keep conditions, the clean-checkout
					// counter-case, and the destructive flag's limit.
					"keeps a checkout that has uncommitted or unpushed work, or is already on this task's branch, and only fetches",
					"a clean, fully pushed checkout on another branch starts over on a new branch",
					"`--fresh` discards uncommitted and untracked files",
					"push any you still need first",
					// Git identity: the scoped override and the two prohibitions.
					"keep it unless asked",
					"`git config --worktree user.name` / `user.email`",
					"plain or `--local` config can leak into a shared cache",
					"never change the global identity",
				} {
					if !strings.Contains(ac, want) {
						t.Errorf("Available Commands lost %q\n---\n%s", want, ac)
					}
				}

				// Detail that now lives behind the pointers must not drift
				// back in, and the over-broad last_error claim must not return:
				// a pending check or an unmet condition fires nothing and
				// records no error.
				for _, banned := range []string{
					"--until-children-done",
					"server-side GitHub snapshots",
					"--filter-actor-id USER_ID",
					"records why in `last_error`",
					"re-running it keeps existing work and only fetches",
				} {
					if strings.Contains(ac, banned) {
						t.Errorf("Available Commands carries %q\n---\n%s", banned, ac)
					}
				}
			})
		}
	}
}

// TestAvailableCommandsReferencesResolveInEveryKind fails when the shared
// Available Commands section points at a brief section or workflow step that
// the rendering kind does not have. A pointer that resolves in the issue brief
// but not in chat or autopilot silently drops the rule it stands in for.
func TestAvailableCommandsReferencesResolveInEveryKind(t *testing.T) {
	t.Parallel()
	headingRef := regexp.MustCompile("`(##+ [^`]+)`")
	stepRef := regexp.MustCompile(`(?i)\bworkflow step \d`)
	for kind, ctx := range availableCommandsKinds {
		for _, provider := range []string{"claude", "codex"} {
			brief := buildMetaSkillContent(provider, ctx)
			ac := availableCommandsSection(t, brief)
			for _, m := range headingRef.FindAllStringSubmatch(ac, -1) {
				if !strings.HasPrefix(brief, m[1]+"\n") && !strings.Contains(brief, "\n"+m[1]+"\n") {
					t.Errorf("%s/%s: Available Commands points at %q, which this brief does not contain", kind, provider, m[1])
				}
			}
			if hasWorkflowSteps := strings.Contains(brief, "\n1. "); !hasWorkflowSteps {
				for _, m := range stepRef.FindAllString(ac, -1) {
					t.Errorf("%s/%s: Available Commands points at %q, but this brief has no numbered workflow", kind, provider, m)
				}
			}
		}
	}
}
