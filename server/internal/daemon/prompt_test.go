package daemon

import (
	"strings"
	"testing"
)

// TestBuildQuickCreatePromptRules locks in the rules that govern how the
// quick-create agent is allowed to translate raw user input into the issue
// description body. Each substring corresponds to a concrete failure mode
// observed in production output:
//   - meta-instructions ("create an issue", "cc @X") leaking into the body
//   - the Context section being misused as an apology log when no external
//     references were actually fetched
//   - hard-line rules being silently dropped on prompt rewrites
func TestBuildQuickCreatePromptRules(t *testing.T) {
	out := buildQuickCreatePrompt(Task{QuickCreatePrompt: "fix the login button color"})

	mustContain := []string{
		// high-fidelity invariant
		"Faithfully restate what the user wants",
		"Preserve specific names, identifiers, file paths",
		// strip non-spec material: verbal routing wrappers + conversational fillers
		"verbal routing wrappers about creating the issue",
		"pure conversational fillers",
		// cc routing must survive: mention link stays in description so the
		// auto-subscribe path fires (multica issue create has no --subscriber flag)
		"CC exception",
		"auto-subscribes members",
		// context section is conditional and must not be an apology log
		"include ONLY when the input cited external resources",
		"never use it as an apology log",
		// hard rules
		"never invent requirements",
		"never reduce multi-sentence input",
	}
	for _, s := range mustContain {
		if !strings.Contains(out, s) {
			t.Errorf("buildQuickCreatePrompt output missing required rule: %q", s)
		}
	}
}

// threadIssueTask is a small helper to keep the thread-issue tests
// readable — most of the Task fields are irrelevant to the prompt and
// muddy the failure output if inlined repeatedly.
func threadIssueTask(history []ChannelHistoryMessage) Task {
	return Task{
		TaskKind:    "thread_issue",
		ChannelID:   "11111111-1111-1111-1111-111111111111",
		ChannelName: "design",
		Agent: &AgentData{
			ID:   "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
			Name: "TestBot",
		},
		ChannelHistory: history,
	}
}

// TestBuildThreadIssuePrompt_AllowsIssueCommands locks in the contract
// that thread-issue tasks run in full issue-task mode: the prompt must
// tell the agent to call `multica issue create` and must NOT inherit
// the channel-mention prompt's "Do not call multica issue" guardrail.
func TestBuildThreadIssuePrompt_AllowsIssueCommands(t *testing.T) {
	out := BuildPrompt(threadIssueTask(nil))

	if !strings.Contains(out, "multica issue create") {
		t.Errorf("thread-issue prompt must tell the agent to call `multica issue create`. got:\n%s", out)
	}
	// The channel-mention prompt has the line:
	//   - This is NOT an issue task. Do not call `multica issue ...` commands.
	// That exact instruction must NOT survive into the thread-issue prompt.
	if strings.Contains(out, "Do not call `multica issue") {
		t.Errorf("thread-issue prompt must NOT forbid `multica issue` (that's the channel-mention guard). got:\n%s", out)
	}
	if !strings.Contains(out, "issue-creation assistant") {
		t.Errorf("thread-issue prompt should self-identify as an issue-creation assistant. got:\n%s", out)
	}
	// Output format invariant: one Created MUL-... line per issue, no chat reply.
	if !strings.Contains(out, "Created MUL-") {
		t.Errorf("thread-issue prompt must specify the `Created MUL-<n>: <title>` output line. got:\n%s", out)
	}
	if !strings.Contains(out, "Do NOT post a chat reply") {
		t.Errorf("thread-issue prompt must forbid posting a chat reply. got:\n%s", out)
	}
}

// TestBuildThreadIssuePrompt_EmbedsThreadHistory verifies the channel
// history hydrated by loadThreadHistoryForTask actually surfaces in the
// rendered prompt. Without this the agent has no idea what the user
// wants to convert.
func TestBuildThreadIssuePrompt_EmbedsThreadHistory(t *testing.T) {
	history := []ChannelHistoryMessage{
		{
			ID:         "msg-parent",
			CreatedAt:  "2025-01-02T03:04:05Z",
			AuthorType: "member",
			AuthorName: "Alice",
			Content:    "we keep hitting a flaky test in the auth suite",
		},
		{
			ID:         "msg-reply-1",
			CreatedAt:  "2025-01-02T03:05:00Z",
			AuthorType: "agent",
			AuthorName: "DiagBot",
			Content:    "could be a race in the redis fixture",
		},
	}
	out := BuildPrompt(threadIssueTask(history))

	if !strings.Contains(out, "flaky test in the auth suite") {
		t.Errorf("prompt missing parent message content. got:\n%s", out)
	}
	if !strings.Contains(out, "race in the redis fixture") {
		t.Errorf("prompt missing reply content. got:\n%s", out)
	}
	if !strings.Contains(out, "Alice") || !strings.Contains(out, "DiagBot") {
		t.Errorf("prompt missing author names. got:\n%s", out)
	}
	// Agent role disambiguation — the prompt should mark agent authors
	// so the LLM doesn't confuse them with humans.
	if !strings.Contains(out, "DiagBot (agent)") {
		t.Errorf("agent author should be tagged `(agent)`. got:\n%s", out)
	}
}

// TestBuildThreadIssuePrompt_ParentIssueAndProject verifies the prompt
// surfaces the optional project + parent-issue context when the
// requester picked them in the dispatch dialog. Both fields control
// CLI flag emission, so a missing render means the agent will create
// orphan issues.
func TestBuildThreadIssuePrompt_ParentIssueAndProject(t *testing.T) {
	task := threadIssueTask(nil)
	task.ThreadIssueProjectID = "33333333-3333-3333-3333-333333333333"
	task.ThreadIssueProjectTitle = "Q3 Reliability"
	task.ThreadIssueParentIssueID = "44444444-4444-4444-4444-444444444444"
	task.ThreadIssueParentIssueKey = "MUL-99"
	task.ThreadIssueInstruction = "Capture every concrete bug as its own issue."

	out := BuildPrompt(task)

	if !strings.Contains(out, "Q3 Reliability") {
		t.Errorf("prompt missing project title. got:\n%s", out)
	}
	if !strings.Contains(out, "MUL-99") {
		t.Errorf("prompt missing parent issue key. got:\n%s", out)
	}
	if !strings.Contains(out, "--parent") {
		t.Errorf("prompt should instruct agent to pass --parent. got:\n%s", out)
	}
	if !strings.Contains(out, "--project-id") && !strings.Contains(out, "--project ") {
		t.Errorf("prompt should instruct agent to pass --project or --project-id. got:\n%s", out)
	}
	if !strings.Contains(out, "Capture every concrete bug") {
		t.Errorf("prompt missing requester instruction. got:\n%s", out)
	}
}
