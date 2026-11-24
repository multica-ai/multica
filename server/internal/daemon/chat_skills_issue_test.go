package daemon

import (
	"path/filepath"
	"strings"
	"testing"
)

// issueSkillTask builds a comment-triggered issue task whose trigger comment
// was authored by a human member.
func issueSkillTask(content string) Task {
	return Task{
		IssueID: "issue-1", TriggerCommentID: "comment-trigger", TriggerThreadID: "comment-trigger",
		TriggerCommentContent: content, TriggerAuthorType: "member", TriggerAuthorName: "Member",
		Agent: &AgentData{Skills: []SkillData{
			{ID: "one", Name: "Review Code", Content: "---\nname: other\ndisable-model-invocation: true\n---\nFIRST BODY"},
			{ID: "two", Name: "Review-Code", Content: "SECOND BODY"},
		}},
	}
}

func issueSkillDirs(t *testing.T) map[string]string {
	t.Helper()
	dir := t.TempDir()
	return map[string]string{"one": filepath.Join(dir, "review-code"), "two": filepath.Join(dir, "review-code-multica-2")}
}

func TestBuildRuntimePromptIssueMemberComments(t *testing.T) {
	dirs := issueSkillDirs(t)
	task := issueSkillTask("[/spoofed](slash://skill/two) please review")
	task.CoalescedComments = []CoalescedCommentData{
		{ID: "c1", AuthorType: "member", Content: "earlier ask with [/first](slash://skill/one)"},
		{ID: "c2", AuthorType: "agent", Content: "agent also picked [/two](slash://skill/two)"},
	}
	prompt, invocation := buildRuntimePrompt(task, "claude", dirs)
	if invocation == nil || len(invocation.Skills) != 2 || invocation.Skills[0].Name != "review-code" || invocation.Skills[1].Name != "review-code-multica-2" {
		t.Fatalf("wrong selection order/identity: %+v", invocation)
	}
	for _, want := range []string{
		"Explicitly selected skills:\n- review-code\n- review-code-multica-2\n",
		"earlier ask with /review-code\n", "/review-code-multica-2 please review", "--parent comment-trigger", dirs["one"], dirs["two"],
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q: %s", want, prompt)
		}
	}
	if strings.Contains(prompt, "spoofed") {
		t.Errorf("prompt keeps acted-on trigger link: %s", prompt)
	}
	// The agent-authored coalesced comment is quoted untouched: its link was
	// never selected, so stripping it would hide an untrusted pick.
	if !strings.Contains(prompt, "agent also picked [/two](slash://skill/two)") {
		t.Errorf("agent coalesced comment must keep its link verbatim: %s", prompt)
	}
	if invocation.UserMessage != "earlier ask with /review-code\n\n/review-code-multica-2 please review" {
		// The member texts join chronologically (coalesced first, trigger
		// last), each with its acted-on link replaced by its resolved name.
		t.Fatalf("UserMessage=%q", invocation.UserMessage)
	}
	// Issue runs keep the complete comment prompt as the platform block: the
	// reply routing and issue context must survive for Claude's split.
	if invocation.PlatformPrompt != invocation.Prompt || !strings.Contains(invocation.PlatformPrompt, "--parent comment-trigger") {
		t.Fatalf("PlatformPrompt lost the comment prompt: %q", invocation.PlatformPrompt)
	}
	if strings.Contains(invocation.PlatformPrompt, "User message:\n") {
		t.Fatalf("issue platform block must not synthesize an empty chat message: %q", invocation.PlatformPrompt)
	}
}

func TestBuildRuntimePromptIssueMultiSelectConflictSuffix(t *testing.T) {
	dirs := issueSkillDirs(t)
	task := issueSkillTask("[/a](slash://skill/two) and [/b](slash://skill/one) together")
	_, invocation := buildRuntimePrompt(task, "hermes", dirs)
	if invocation == nil || len(invocation.Skills) != 2 ||
		invocation.Skills[0].Name != "review-code-multica-2" || invocation.Skills[1].Name != "review-code" {
		t.Fatalf("conflict-suffixed names must follow first-seen order: %+v", invocation)
	}
	if !strings.Contains(invocation.FallbackPrompt(), "SECOND BODY") || !strings.Contains(invocation.FallbackPrompt(), "FIRST BODY") {
		t.Fatalf("fallback lost skill bodies: %s", invocation.FallbackPrompt())
	}
}

func TestBuildRuntimePromptIssueDoesNotPromoteUntrustedInput(t *testing.T) {
	link := "[/review](slash://skill/one)"
	for _, tc := range []struct {
		name string
		edit func(*Task)
	}{
		{"agent trigger comment", func(t *Task) { t.TriggerAuthorType = "agent" }},
		{"system trigger comment", func(t *Task) { t.TriggerAuthorType = "system" }},
		{"legacy empty author type", func(t *Task) { t.TriggerAuthorType = "" }},
		{"wakeup task", func(t *Task) { t.WakeupID = "wakeup-1" }},
		{"autopilot task", func(t *Task) { t.AutopilotRunID = "run-1" }},
		{"assignment task without comment", func(t *Task) { t.TriggerCommentID = ""; t.TriggerCommentContent = "" }},
		{"quoted trigger comment", func(t *Task) { t.TriggerCommentContent = "> " + link }},
		{"inline code trigger comment", func(t *Task) { t.TriggerCommentContent = "`" + link + "`" }},
		{"fenced code trigger comment", func(t *Task) { t.TriggerCommentContent = "```\n" + link + "\n```" }},
		{"unbound skill id", func(t *Task) { t.TriggerCommentContent = "[/x](slash://skill/stranger)" }},
		{"plain slash text", func(t *Task) { t.TriggerCommentContent = "/compact" }},
		{"stale chat_message payload", func(t *Task) { t.TriggerCommentContent = ""; t.ChatMessage = link }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := issueSkillTask(link)
			tc.edit(&task)
			prompt, invocation := buildRuntimePrompt(task, "claude", issueSkillDirs(t))
			if invocation != nil || strings.Contains(prompt, "FIRST BODY") || strings.HasPrefix(prompt, "/") ||
				strings.Contains(prompt, "Explicitly selected skills:") {
				t.Fatalf("untrusted input became an invocation: %+v, %s", invocation, prompt)
			}
		})
	}
}

func TestBuildRuntimePromptIssueCoalescedAuthorGate(t *testing.T) {
	dirs := issueSkillDirs(t)
	// Only the coalesced member comment carries a link; the trigger is an
	// agent forward. The member pick still selects, the agent pick does not.
	task := issueSkillTask("forwarding [/mine](slash://skill/two)")
	task.TriggerAuthorType = "agent"
	task.CoalescedComments = []CoalescedCommentData{
		{ID: "c1", AuthorType: "member", Content: "[/first](slash://skill/one) run review"},
		{ID: "c2", AuthorType: "system", Content: "[/two](slash://skill/two) platform note"},
	}
	prompt, invocation := buildRuntimePrompt(task, "pi", dirs)
	if invocation == nil || len(invocation.Skills) != 1 || invocation.Skills[0].Name != "review-code" {
		t.Fatalf("member coalesced pick ignored: %+v", invocation)
	}
	if invocation.UserMessage != "/review-code run review" {
		t.Fatalf("UserMessage must carry only the rewritten member text: %q", invocation.UserMessage)
	}
	if !strings.Contains(prompt, "Explicitly selected skills:\n- review-code\n") {
		t.Errorf("prompt missing selection list: %s", prompt)
	}
	if !strings.Contains(prompt, "[/two](slash://skill/two)") || !strings.Contains(prompt, "[/mine](slash://skill/two)") {
		t.Errorf("unselected links must stay quoted: %s", prompt)
	}
}

func TestBuildRuntimePromptIssueMissingInstallation(t *testing.T) {
	link := "[/review](slash://skill/one)"
	for _, dirs := range []map[string]string{nil, {}} {
		task := issueSkillTask(link)
		prompt, invocation := buildRuntimePrompt(task, "claude", dirs)
		if invocation != nil || strings.Contains(prompt, "FIRST BODY") {
			t.Fatalf("missing directory must not invoke: %+v", invocation)
		}
		// The selection stays visible as a hint, mirroring chat behavior.
		if !strings.Contains(prompt, "Explicitly selected skills:\n- Review Code\n") {
			t.Errorf("hint must fall back to the agent skill name: %s", prompt)
		}
		if !strings.Contains(prompt, link) {
			t.Errorf("unmaterialized link must stay in the quoted comment: %s", prompt)
		}
	}
}

// TestBuildRuntimePromptIssuePureCapsuleTrigger pins the skill-capsule-only
// comment: stripping its link must not collapse the comment section the way a
// server fetch failure does — the header, placeholder quote and every
// coalesced comment still render, and the earlier member ask survives into
// UserMessage.
func TestBuildRuntimePromptIssuePureCapsuleTrigger(t *testing.T) {
	dirs := issueSkillDirs(t)
	task := issueSkillTask("[/review](slash://skill/one)")
	task.CoalescedComments = []CoalescedCommentData{
		{ID: "c-earlier", ThreadID: "c-earlier", AuthorType: "member", Content: "Please check the Safari login redirect bug"},
	}
	prompt, invocation := buildRuntimePrompt(task, "claude", dirs)
	if invocation == nil || len(invocation.Skills) != 1 {
		t.Fatalf("pure capsule must still select: %+v", invocation)
	}
	for _, want := range []string{
		"[NEW COMMENT]", "(explicit skill selection only",
		"Please check the Safari login redirect bug",
		"--parent c-earlier", "Explicitly selected skills:\n- review-code\n",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q: %s", want, prompt)
		}
	}
	if strings.Contains(prompt, "slash://skill") {
		t.Errorf("acted-on link must be stripped everywhere: %s", prompt)
	}
	if invocation.UserMessage != "Please check the Safari login redirect bug" {
		t.Fatalf("UserMessage must carry the earlier member ask: %q", invocation.UserMessage)
	}
	if !strings.Contains(invocation.PlatformPrompt, "Please check the Safari login redirect bug") {
		t.Fatalf("PlatformPrompt lost the coalesced member ask: %q", invocation.PlatformPrompt)
	}
}

// TestBuildRuntimePromptIssuePureCapsuleEditedTrigger keeps the edited-comment
// notice alive when a resumed session's trigger was edited down to a bare
// capsule.
func TestBuildRuntimePromptIssuePureCapsuleEditedTrigger(t *testing.T) {
	dirs := issueSkillDirs(t)
	task := issueSkillTask("[/review](slash://skill/one)")
	task.PriorSessionID = "session-1"
	task.TriggerCommentEditedAt = "2026-11-16T09:00:00Z"
	prompt, invocation := buildRuntimePrompt(task, "claude", dirs)
	if invocation == nil {
		t.Fatal("pure capsule must still select")
	}
	if !strings.Contains(prompt, "[EDITED COMMENT]") || !strings.Contains(prompt, "was edited at") {
		t.Errorf("edited-trigger notice lost: %s", prompt)
	}
	if !strings.Contains(prompt, "(explicit skill selection only") {
		t.Errorf("placeholder quote lost: %s", prompt)
	}
	if invocation.UserMessage != "" {
		t.Fatalf("no member ask text exists; UserMessage=%q", invocation.UserMessage)
	}
}

// TestBuildRuntimePromptIssueUserMessageJoinsMemberTexts covers the join: a
// capsule-only coalesced comment contributes nothing (no blank segment) and a
// member comment without any link still reaches UserMessage.
func TestBuildRuntimePromptIssueUserMessageJoinsMemberTexts(t *testing.T) {
	dirs := issueSkillDirs(t)
	task := issueSkillTask("[/two](slash://skill/two) please review")
	task.CoalescedComments = []CoalescedCommentData{
		{ID: "c1", AuthorType: "member", Content: "[/first](slash://skill/one)"},
		{ID: "c2", AuthorType: "member", Content: "and double-check the tests"},
	}
	_, invocation := buildRuntimePrompt(task, "claude", dirs)
	if invocation == nil {
		t.Fatal("selection missing")
	}
	if invocation.UserMessage != "and double-check the tests\n\n/review-code-multica-2 please review" {
		t.Fatalf("UserMessage must join member asks without blank segments: %q", invocation.UserMessage)
	}
}

// TestBuildRuntimePromptIssuePureCapsuleAlone verifies the bare single-comment
// case: the invocation still exists and the placeholder keeps the comment
// section from collapsing.
func TestBuildRuntimePromptIssuePureCapsuleAlone(t *testing.T) {
	dirs := issueSkillDirs(t)
	prompt, invocation := buildRuntimePrompt(issueSkillTask("[/review](slash://skill/one)"), "claude", dirs)
	if invocation == nil || invocation.UserMessage != "" {
		t.Fatalf("invocation=%+v UserMessage=%q", invocation, invocation.UserMessage)
	}
	if !strings.Contains(prompt, "[NEW COMMENT]") || !strings.Contains(prompt, "(explicit skill selection only") {
		t.Errorf("comment section collapsed: %s", prompt)
	}
	if !strings.Contains(prompt, "Explicitly selected skills:\n- review-code\n") {
		t.Errorf("selection list missing: %s", prompt)
	}
}

func TestMemberSkillTextsChatGateUnchanged(t *testing.T) {
	// A chat task on an issue-less run keeps exactly one member text; an
	// issue task never consults ChatSkillMessage even when a stale server
	// populates it.
	message := "[/x](slash://skill/one)"
	chat := chatSkillTask(message)
	if texts := memberSkillTexts(chat); len(texts) != 1 || texts[0].text != message {
		t.Fatalf("chat member texts changed: %+v", texts)
	}
	issue := issueSkillTask("")
	issue.ChatSessionID = ""
	issue.ChatSkillMessage = &message
	if texts := memberSkillTexts(issue); len(texts) != 0 {
		t.Fatalf("issue task must ignore ChatSkillMessage and empty comments: %+v", texts)
	}
}
