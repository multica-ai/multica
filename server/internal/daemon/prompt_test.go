package daemon

import (
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
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
		// output/reporting must be workspace-prefix agnostic. Workspaces can
		// use custom issue prefixes, so a successful issue creation should
		// not look failed merely because the identifier does not match one
		// fixed prefix.
		"multica issue create --output json",
		"JSON response",
		"identifier",
		"Do not scrape human output",
		"do not assume any workspace issue prefix",
		"Created <identifier-or-id>: <title>",
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

// TestBuildQuickCreatePromptAssigneeIncludesSquads locks in the MUL-2165
// fix: the assignee-resolution rules must tell the agent to consult the
// squad list alongside members and agents. Before this, a quick-create
// input like "assign to <SquadName>" silently fell through to
// "Unrecognized assignee" because squads were never queried.
func TestBuildQuickCreatePromptAssigneeIncludesSquads(t *testing.T) {
	out := buildQuickCreatePrompt(Task{QuickCreatePrompt: "fix the login button color"})
	mustContain := []string{
		"multica squad list",
		"Squads are first-class assignees",
		"Treat bare @-routing as an assignee directive",
		"让 @独立团 review 这个 PR",
		"pass the squad's `id` as `--assignee-id`",
	}
	for _, s := range mustContain {
		if !strings.Contains(out, s) {
			t.Errorf("buildQuickCreatePrompt assignee block missing %q\n--- output ---\n%s", s, out)
		}
	}
}

// TestBuildQuickCreatePromptSquadDefaultsToSquad locks in the MUL-2203
// fix: when the picker was a squad, the task runs on the squad's leader
// agent, but the default assignee for issues created by this run must
// point at the SQUAD's UUID — not the leader agent's UUID. The previous
// "default to YOURSELF" instruction made squad-created issues land under
// the leader, hiding them from the squad's delegation flow.
func TestBuildQuickCreatePromptSquadDefaultsToSquad(t *testing.T) {
	const (
		squadID   = "aaaa1111-2222-3333-4444-555555555555"
		squadName = "独立团"
		leaderID  = "bbbb1111-2222-3333-4444-666666666666"
	)
	out := buildQuickCreatePrompt(Task{
		QuickCreatePrompt: "fix the login button color",
		Agent:             &AgentData{ID: leaderID, Name: "leader-agent"},
		SquadID:           squadID,
		SquadName:         squadName,
	})

	// The default-assignee instruction must point at the squad UUID.
	if !strings.Contains(out, "--assignee-id \""+squadID+"\"") {
		t.Errorf("buildQuickCreatePrompt with SquadID must default to the squad's UUID, got:\n%s", out)
	}
	// And it must NOT tell the agent to default to itself (the leader).
	if strings.Contains(out, "--assignee-id \""+leaderID+"\"") {
		t.Errorf("buildQuickCreatePrompt with SquadID must NOT default to the leader agent's UUID, got:\n%s", out)
	}
	// The squad name should appear in the instruction so the agent has
	// human-readable context for the routing decision.
	if !strings.Contains(out, squadName) {
		t.Errorf("buildQuickCreatePrompt with SquadID should mention the squad name %q, got:\n%s", squadName, out)
	}
	// And the prompt must explicitly call out the squad-vs-leader rule
	// so the agent does not silently regress to "default to YOURSELF".
	mustContain := []string{
		"picker SQUAD",
		"running on the squad's behalf",
		"do not assign it to your own agent UUID",
	}
	for _, s := range mustContain {
		if !strings.Contains(out, s) {
			t.Errorf("buildQuickCreatePrompt with SquadID missing %q\n--- output ---\n%s", s, out)
		}
	}
}

// TestBuildQuickCreatePromptProjectPinning verifies that when the user
// pins a project in the quick-create modal, the prompt instructs the agent
// to pass `--project <uuid>` exactly. Without this, the agent would re-read
// the workspace default and silently drop the user's selection — the same
// "I have to retype 'in project X' every time" failure mode the modal
// addition was meant to fix.
func TestBuildQuickCreatePromptProjectPinning(t *testing.T) {
	const projectID = "11111111-2222-3333-4444-555555555555"
	out := buildQuickCreatePrompt(Task{
		QuickCreatePrompt: "fix the login button color",
		ProjectID:         projectID,
		ProjectTitle:      "Web App",
	})
	mustContain := []string{
		"--project \"" + projectID + "\"",
		"Web App",
		"modal selection is authoritative",
	}
	for _, s := range mustContain {
		if !strings.Contains(out, s) {
			t.Errorf("buildQuickCreatePrompt with project missing %q\n--- output ---\n%s", s, out)
		}
	}

	// Without a project, the prompt must keep the legacy "omit" instruction
	// so the agent doesn't accidentally start passing --project on plain
	// quick-create runs.
	plain := buildQuickCreatePrompt(Task{QuickCreatePrompt: "fix the login button color"})
	if !strings.Contains(plain, "**project**: omit") {
		t.Errorf("buildQuickCreatePrompt without project must keep the omit instruction, got:\n%s", plain)
	}
	if strings.Contains(plain, "--project") {
		t.Errorf("buildQuickCreatePrompt without project must NOT mention --project, got:\n%s", plain)
	}
}

func TestBuildQuickCreatePromptExplicitPriorityAndDueDate(t *testing.T) {
	out := buildQuickCreatePrompt(Task{
		QuickCreatePrompt:   "fix the login button color",
		QuickCreatePriority: "urgent",
		QuickCreateDueDate:  "2026-08-01",
	})
	for _, want := range []string{
		"--priority urgent",
		"--due-date 2026-08-01",
		"quick-create selection is authoritative",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("buildQuickCreatePrompt with explicit fields missing %q\n--- output ---\n%s", want, out)
		}
	}
	if strings.Contains(out, "Map P0/P1") {
		t.Errorf("explicit priority must replace inference rules, got:\n%s", out)
	}
}

// TestBuildQuickCreatePromptParentPinning verifies that when the user
// opened quick-create from "Add sub issue" on an existing issue, the prompt
// instructs the agent to pass `--parent <uuid>` so the new issue is filed
// as a sub-issue. The frontend already seeds parent_issue_id silently
// through the manual→agent switch, so this is the last hop that has to
// hold up — without the prompt instruction the agent would create a
// standalone issue and the sub-issue relationship would be silently
// dropped.
func TestBuildQuickCreatePromptParentPinning(t *testing.T) {
	const (
		parentID         = "33333333-2222-1111-4444-555555555555"
		parentIdentifier = "MUL-2534"
	)
	out := buildQuickCreatePrompt(Task{
		QuickCreatePrompt:     "fix the login button color",
		ParentIssueID:         parentID,
		ParentIssueIdentifier: parentIdentifier,
	})
	mustContain := []string{
		"--parent \"" + parentID + "\"",
		parentIdentifier,
		"modal entry point is authoritative",
		"filed as a sub-issue",
	}
	for _, s := range mustContain {
		if !strings.Contains(out, s) {
			t.Errorf("buildQuickCreatePrompt with parent missing %q\n--- output ---\n%s", s, out)
		}
	}

	// When only the UUID is available (identifier lookup failed on claim),
	// the agent must still get the --parent instruction so the sub-issue
	// intent isn't silently dropped.
	uuidOnly := buildQuickCreatePrompt(Task{
		QuickCreatePrompt: "fix the login button color",
		ParentIssueID:     parentID,
	})
	if !strings.Contains(uuidOnly, "--parent \""+parentID+"\"") {
		t.Errorf("buildQuickCreatePrompt with parent UUID only must still pin --parent, got:\n%s", uuidOnly)
	}

	// Without a parent, the prompt must NOT mention --parent at all — a
	// plain quick-create run should not start filing sub-issues.
	plain := buildQuickCreatePrompt(Task{QuickCreatePrompt: "fix the login button color"})
	if strings.Contains(plain, "--parent") {
		t.Errorf("buildQuickCreatePrompt without parent must NOT mention --parent, got:\n%s", plain)
	}
}

// TestBuildPromptSquadLeaderNoActionForMemberTrigger verifies that the
// squad leader no_action prohibition is injected in the per-turn prompt
// regardless of whether the triggering comment was posted by an agent or
// a member. This was the root cause of the "LGTM is a pure acknowledgment
// — no reply needed. Exiting silently." noise comment: the prohibition
// only fired for agent-triggered comments, so member-triggered ones
// (like "LGTM") bypassed it.
func TestBuildPromptSquadLeaderNoActionForMemberTrigger(t *testing.T) {
	task := Task{
		IssueID:               "issue-123",
		TriggerCommentID:      "comment-456",
		TriggerCommentContent: "LGTM",
		TriggerAuthorType:     "member",
		TriggerAuthorName:     "Bohan",
		Agent: &AgentData{
			Instructions: "Some instructions\n\n## Squad Operating Protocol\n\nYou are the LEADER...",
		},
	}
	out := BuildPrompt(task, "claude")
	if !strings.Contains(out, "Squad leader no_action rule") {
		t.Errorf("buildCommentPrompt must inject squad leader no_action rule for member-triggered comments, got:\n%s", out)
	}
	if !strings.Contains(out, "DO NOT post any comment") {
		t.Errorf("buildCommentPrompt must contain DO NOT post prohibition for member-triggered squad leader, got:\n%s", out)
	}
}

// TestBuildPromptSquadLeaderNoActionForAgentTrigger verifies the rule also
// fires for agent-triggered comments (the original path that already worked).
func TestBuildPromptSquadLeaderNoActionForAgentTrigger(t *testing.T) {
	task := Task{
		IssueID:               "issue-123",
		TriggerCommentID:      "comment-456",
		TriggerCommentContent: "Deploy complete.",
		TriggerAuthorType:     "agent",
		TriggerAuthorName:     "deploy-boy",
		Agent: &AgentData{
			Instructions: "Some instructions\n\n## Squad Operating Protocol\n\nYou are the LEADER...",
		},
	}
	out := BuildPrompt(task, "claude")
	if !strings.Contains(out, "Squad leader no_action rule") {
		t.Errorf("buildCommentPrompt must inject squad leader no_action rule for agent-triggered comments, got:\n%s", out)
	}
}

func TestBuildChatPromptAttachmentIDsCanBeBoundToCreatedIssues(t *testing.T) {
	task := Task{
		ChatSessionID: "sess-1",
		ChatMessage:   "please create an issue with this screenshot",
		ChatMessageAttachments: []ChatAttachmentMeta{
			{ID: "019ec09d-6222-722b-bdfa-427b105d80be", Filename: "shot.png", ContentType: "image/png"},
		},
	}
	out := BuildPrompt(task, "claude")
	for _, want := range []string{
		"Attachments on this message:",
		"id=019ec09d-6222-722b-bdfa-427b105d80be",
		"multica attachment download <id>",
		"--attachment-id <id>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("chat prompt missing %q\n--- output ---\n%s", want, out)
		}
	}
}

func TestBuildChatPromptChannelAwareness(t *testing.T) {
	t.Run("slack-backed prompt teaches both read commands", func(t *testing.T) {
		out := buildChatPrompt(Task{
			ChatSessionID:   "sess-1",
			ChatChannelType: "slack",
			ChatMessage:     "你刚刚和 xxx 聊了什么",
		})
		for _, want := range []string{"Slack", "NOT in Multica", "multica chat history", "multica chat thread", "Do NOT narrate"} {
			if !strings.Contains(out, want) {
				t.Fatalf("slack-backed prompt missing %q\n--- output ---\n%s", want, out)
			}
		}
	})

	t.Run("top-level mention starts with history", func(t *testing.T) {
		out := buildChatPrompt(Task{ChatSessionID: "s", ChatChannelType: "slack", ChatInThread: false, ChatMessage: "hi"})
		if !strings.Contains(out, "top level: start with `multica chat history`") {
			t.Fatalf("expected top-level guidance, got:\n%s", out)
		}
	})

	t.Run("in-thread mention starts with thread", func(t *testing.T) {
		out := buildChatPrompt(Task{ChatSessionID: "s", ChatChannelType: "slack", ChatInThread: true, ChatMessage: "hi"})
		if !strings.Contains(out, "inside a thread: start with `multica chat thread`") {
			t.Fatalf("expected in-thread guidance, got:\n%s", out)
		}
	})

	t.Run("web-only session has no channel block", func(t *testing.T) {
		out := buildChatPrompt(Task{
			ChatSessionID: "sess-1",
			ChatMessage:   "hi",
		})
		if strings.Contains(out, "multica chat history") {
			t.Fatalf("web-only chat prompt should not mention channel history, got:\n%s", out)
		}
	})
}

// TestBuildChatPromptNoNarrationOnEveryChannel pins the THIRD axis of the chat
// channel policy: the no-narration delivery rule keys off "is there a channel at
// all", like the upload axis and unlike the Slack-only history axis.
//
// Regression guard for GH #6006. #4776 introduced the rule for every channel;
// the MUL-4899 split moved it into the Slack branch along with the read commands
// its wording happened to mention, so Feishu/Lark replies silently went back to
// carrying interim narration. The two-layer matrix below could not catch that —
// it only ever asserted the rule on the Slack case.
//
// The carve-out is pinned alongside the prohibition on purpose. A rule phrased
// as "don't say what you just did" reads as forbidding "已创建 Issue X" — the
// actual deliverable for a do-this request — so the two must move together: the
// prohibition covers PROCESS, never the completed outcome.
func TestBuildChatPromptNoNarrationOnEveryChannel(t *testing.T) {
	const (
		prohibition = "Do NOT narrate planned or in-progress steps"
		carveOut    = "completed actions are part of the outcome"
	)

	for _, tc := range []struct {
		name        string
		channelType string
		want        bool
	}{
		{name: "slack", channelType: execenv.ChannelTypeSlack, want: true},
		{name: "feishu", channelType: execenv.ChannelTypeFeishu, want: true},
		{name: "direct chat has no channel to deliver into", channelType: "", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := buildChatPrompt(Task{
				ChatSessionID:   "sess-1",
				ChatChannelType: tc.channelType,
				ChatMessage:     "hi",
			})
			for _, phrase := range []string{prohibition, carveOut} {
				if got := strings.Contains(out, phrase); got != tc.want {
					t.Errorf("%q present=%v, want %v\n--- output ---\n%s", phrase, got, tc.want, out)
				}
			}
			if !tc.want {
				return
			}
			// The prohibition must not read as a blanket ban on past tense. If a
			// future edit drops the carve-out, an agent asked to create an issue
			// has no way left to report that it did.
			if strings.Contains(out, "must not say what you are about to do or just did") {
				t.Errorf("prohibition must scope to process, not completed outcomes\n--- output ---\n%s", out)
			}
		})
	}
}

// TestBuildChatPromptTwoLayerChannelPolicy pins the two INDEPENDENT axes of the
// chat channel policy (MUL-4899). Collapsing them into one condition is exactly
// the bug this matrix exists to catch:
//
//   - delivery: `attachment upload` guidance is injected iff there is NO channel.
//     Any IM reply leaves Multica, where the upload has nothing to bind to.
//   - history: the `chat history` / `chat thread` commands are injected iff the
//     channel is Slack. Those endpoints are hardwired to h.SlackHistory
//     (handler/chat_history.go) — on Feishu they answer "no channel
//     integration", so teaching them there sends the agent down a dead path.
//
// Feishu is the case that proves the axes are separate: no upload AND no
// history. A single `ChatChannelType != ""` gate cannot express it.
func TestBuildChatPromptTwoLayerChannelPolicy(t *testing.T) {
	// Match the IMPERATIVE, not the bare command name. An IM prompt names
	// `multica attachment upload` on purpose — to state that it does not apply
	// here. That negation is the useful copy (the agent knows the command exists
	// from the brief's Available Commands; silence would leave it guessing), so
	// asserting on the bare name would forbid the very sentence we want.
	const uploadGuidance = "run `multica attachment upload <local-path>`"
	const historyGuidance = "multica chat history"

	cases := []struct {
		name        string
		channelType string
		wantUpload  bool
		wantHistory bool
		wantPhrases []string
	}{
		{
			name:        "direct chat: upload, no history",
			channelType: "",
			wantUpload:  true,
			wantHistory: false,
		},
		{
			name:        "slack: no upload, has history",
			channelType: execenv.ChannelTypeSlack,
			wantUpload:  false,
			wantHistory: true,
			wantPhrases: []string{"Slack", "delivered to Slack as text", "You cannot attach a file to it"},
		},
		{
			name:        "feishu: no upload, no history",
			channelType: execenv.ChannelTypeFeishu,
			wantUpload:  false,
			wantHistory: false,
			wantPhrases: []string{
				"Feishu/Lark",
				"no history reader for Feishu/Lark",
				"delivered to Feishu/Lark as text",
				"You cannot attach a file to it",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := buildChatPrompt(Task{
				ChatSessionID:   "sess-1",
				ChatChannelType: tc.channelType,
				ChatMessage:     "hi",
			})
			if got := strings.Contains(out, uploadGuidance); got != tc.wantUpload {
				t.Errorf("upload guidance present=%v, want %v\n--- output ---\n%s", got, tc.wantUpload, out)
			}
			if got := strings.Contains(out, historyGuidance); got != tc.wantHistory {
				t.Errorf("history guidance present=%v, want %v\n--- output ---\n%s", got, tc.wantHistory, out)
			}
			for _, phrase := range tc.wantPhrases {
				if !strings.Contains(out, phrase) {
					t.Errorf("missing %q\n--- output ---\n%s", phrase, out)
				}
			}
		})
	}
}

// ChatInThread only ever selects between `chat history` and `chat thread`. With
// no Feishu history reader there is nothing to select between, so the flag must
// not leak either command into a Feishu prompt even if the server sets it.
func TestBuildChatPromptFeishuIgnoresChatInThread(t *testing.T) {
	out := buildChatPrompt(Task{
		ChatSessionID:   "sess-1",
		ChatChannelType: execenv.ChannelTypeFeishu,
		ChatInThread:    true,
		ChatMessage:     "hi",
	})
	for _, unwanted := range []string{"multica chat thread", "multica chat history"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("feishu prompt must not teach %q (no Feishu history reader exists)\n--- output ---\n%s", unwanted, out)
		}
	}
}

func TestBuildChatPromptAgentIntro(t *testing.T) {
	// The proactive self-introduction chat (MUL-4230) has no user message: the
	// prompt must tell the agent to open the conversation itself, and must NOT
	// carry the generic "respond to their message" framing or an empty
	// "User message:" section that would confuse the agent.
	out := buildChatPrompt(Task{ChatSessionID: "sess-1", ChatIntro: true})
	for _, want := range []string{
		"You were just created",
		"you are opening the conversation",
		"introduce yourself",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("intro prompt missing %q\n--- output ---\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"Respond to their message", "User message:"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("intro prompt should not contain %q\n--- output ---\n%s", unwanted, out)
		}
	}
}

func TestBuildChatPromptSlashSkills(t *testing.T) {
	t.Run("injects selected skills block", func(t *testing.T) {
		task := Task{
			ChatSessionID: "sess-1",
			ChatMessage:   "please [/deploy](slash://skill/abc-123) this",
			Agent: &AgentData{
				Skills: []SkillData{{ID: "abc-123", Name: "deploy"}},
			},
		}
		out := buildChatPrompt(task)
		if !strings.Contains(out, "Explicitly selected skills:\n- deploy\n") {
			t.Fatalf("expected selected skills block, got:\n%s", out)
		}
		if !strings.Contains(out, "User message:\nplease [/deploy](slash://skill/abc-123) this") {
			t.Fatalf("expected raw user message preserved, got:\n%s", out)
		}
	})

	t.Run("ignores skills not belonging to agent", func(t *testing.T) {
		task := Task{
			ChatSessionID: "sess-1",
			ChatMessage:   "[/hacker-skill](slash://skill/evil-id)",
			Agent: &AgentData{
				Skills: []SkillData{{ID: "good-id", Name: "deploy"}},
			},
		}
		out := buildChatPrompt(task)
		if strings.Contains(out, "Explicitly selected skills") {
			t.Fatalf("should not inject block for unknown skill ID, got:\n%s", out)
		}
	})

	t.Run("validates by ID not label", func(t *testing.T) {
		task := Task{
			ChatSessionID: "sess-1",
			ChatMessage:   "[/deploy](slash://skill/wrong-id)",
			Agent: &AgentData{
				Skills: []SkillData{{ID: "real-id", Name: "deploy"}},
			},
		}
		out := buildChatPrompt(task)
		if strings.Contains(out, "Explicitly selected skills") {
			t.Fatalf("matching label with wrong ID must not pass, got:\n%s", out)
		}
	})

	t.Run("uses canonical name not label", func(t *testing.T) {
		task := Task{
			ChatSessionID: "sess-1",
			ChatMessage:   "[/spoofed-name](slash://skill/real-id)",
			Agent: &AgentData{
				Skills: []SkillData{{ID: "real-id", Name: "deploy"}},
			},
		}
		out := buildChatPrompt(task)
		if !strings.Contains(out, "- deploy\n") {
			t.Fatalf("expected canonical name 'deploy', got:\n%s", out)
		}
		if strings.Contains(out, "- spoofed-name\n") {
			t.Fatalf("selected skills block must not use spoofed label, got:\n%s", out)
		}
		if !strings.Contains(out, "User message:\n[/spoofed-name](slash://skill/real-id)") {
			t.Fatalf("expected raw user message with spoofed label preserved, got:\n%s", out)
		}
	})

	t.Run("deduplicates skills", func(t *testing.T) {
		task := Task{
			ChatSessionID: "sess-1",
			ChatMessage:   "[/deploy](slash://skill/a) and [/deploy](slash://skill/a) again",
			Agent: &AgentData{
				Skills: []SkillData{{ID: "a", Name: "deploy"}},
			},
		}
		out := buildChatPrompt(task)
		if strings.Count(out, "- deploy") != 1 {
			t.Fatalf("expected exactly 1 '- deploy', got:\n%s", out)
		}
	})

	t.Run("omits block when no valid skills", func(t *testing.T) {
		task := Task{
			ChatSessionID: "sess-1",
			ChatMessage:   "just a normal message",
			Agent:         &AgentData{Skills: []SkillData{{ID: "a", Name: "deploy"}}},
		}
		out := buildChatPrompt(task)
		if strings.Contains(out, "Explicitly selected skills") {
			t.Fatalf("should not inject block when no slash links, got:\n%s", out)
		}
	})

	t.Run("omits block when agent has no skills", func(t *testing.T) {
		task := Task{
			ChatSessionID: "sess-1",
			ChatMessage:   "[/deploy](slash://skill/abc-123)",
			Agent:         &AgentData{},
		}
		out := buildChatPrompt(task)
		if strings.Contains(out, "Explicitly selected skills") {
			t.Fatalf("should not inject block for agent with no skills, got:\n%s", out)
		}
	})
}

// TestBuildPromptDefaultScansRootsFirst pins that the catch-all fallback
// prompt (no trigger comment, no chat, no autopilot, no quick-create)
// starts assignment-triggered comment catch-up with a bounded roots scan and
// only then offers the full-thread read, while still keeping older history
// available through pagination.
func TestBuildPromptDefaultScansRootsFirst(t *testing.T) {
	out := BuildPrompt(Task{IssueID: "issue-default-1"}, "claude")
	for _, s := range []string{
		"multica issue comment list issue-default-1 --roots-only --summary --output json",
		"--since",
	} {
		if !strings.Contains(out, s) {
			t.Errorf("default BuildPrompt missing %q\n--- output ---\n%s", s, out)
		}
	}
	// MUL-5372: the per-turn prompt names only the reads it wants run. Flag
	// mechanics — cursors, the --recent saturation trap — live once in the
	// runtime workflow file's `## Available Commands`, so restating them here
	// would put the same reference text on every turn.
	if strings.Contains(out, "--recent") {
		t.Errorf("default BuildPrompt should not restate the --recent surface\n--- output ---\n%s", out)
	}
	if strings.Contains(out, "Next thread cursor:") {
		t.Errorf("default BuildPrompt should not restate pagination mechanics\n--- output ---\n%s", out)
	}
	// MUL-5372: this path now leads with a cheap roots scan, and the scan is
	// what supplies thread ids, so a generic `--thread <thread-id>` drill-down
	// is well-founded here. What must still never appear is a CONCRETE anchor —
	// the default path has no trigger comment to derive one from, and an
	// interpolated id would send the agent after a thread that does not exist.
	for _, seg := range strings.Split(out, "--thread")[1:] {
		if !strings.HasPrefix(seg, " <thread-id>") {
			t.Errorf("default BuildPrompt must only use the generic --thread <thread-id> placeholder, never a concrete anchor\n--- output ---\n%s", out)
		}
	}
	// The legacy "If you need comment history" soft phrasing conflicts with
	// the assignment-trigger runtime workflow, which treats reading comments
	// as mandatory. Guard against it sneaking back in.
	if strings.Contains(out, "If you need comment history") {
		t.Errorf("default BuildPrompt still carries the legacy 'If you need' soft phrasing that conflicts with the mandatory workflow\n--- output ---\n%s", out)
	}
	if strings.Contains(out, "multica issue comment list issue-default-1 --output json") {
		t.Errorf("default BuildPrompt still presents the unbounded flat read as the assignment catch-up command\n--- output ---\n%s", out)
	}
}

// TestBuildPromptNonSquadLeaderNoRule verifies that non-squad-leader agents
// do NOT get the squad leader no_action rule injected.
func TestBuildPromptNonSquadLeaderNoRule(t *testing.T) {
	task := Task{
		IssueID:               "issue-123",
		TriggerCommentID:      "comment-456",
		TriggerCommentContent: "LGTM",
		TriggerAuthorType:     "member",
		TriggerAuthorName:     "Bohan",
		Agent: &AgentData{
			Instructions: "Some instructions without the squad marker",
		},
	}
	out := BuildPrompt(task, "claude")
	if strings.Contains(out, "Squad leader no_action rule") {
		t.Errorf("buildCommentPrompt must NOT inject squad leader no_action rule for non-squad-leader agents, got:\n%s", out)
	}
}

// TestBuildPromptNewCommentsHint pins that a comment-triggered task whose agent
// ran before on this issue (NewCommentsSince set, NewCommentCount > 0) gets the
// since-delta hint with the ISSUE-WIDE new-comment count, but is steered to read
// the triggering (parent) thread first rather than blindly pulling every new
// comment.
func TestBuildPromptNewCommentsHint(t *testing.T) {
	const (
		issueID = "issue-new-1"
		since   = "2026-05-28T11:00:00Z"
	)
	task := Task{
		IssueID:               issueID,
		TriggerCommentID:      "trigger-1",
		TriggerThreadID:       "thread-root-1",
		TriggerCommentContent: "please look",
		TriggerAuthorType:     "member",
		NewCommentCount:       3,
		NewCommentsSince:      since,
	}
	out := BuildPrompt(task, "claude")

	// Issue-wide count (reverted from the thread-scoped wording).
	if !strings.Contains(out, "3 new comment(s) on this issue since your last run") {
		t.Errorf("hint must report the issue-wide new-comment count, got:\n%s", out)
	}
	// Don't-blindly-read-all guidance.
	if !strings.Contains(out, "blindly") {
		t.Errorf("hint must discourage blindly reading every new comment, got:\n%s", out)
	}
	// Parent thread first: the --thread <trigger> read is the prioritized action.
	if !strings.Contains(out, "multica issue comment list "+issueID+" --thread thread-root-1 --since "+since+" --output json") {
		t.Errorf("hint must point at the triggering (parent) thread --since read first, got:\n%s", out)
	}
	if !strings.Contains(out, "--tail 30") {
		t.Errorf("hint must offer the full-thread (--tail 30) option, got:\n%s", out)
	}
	// Issue-wide catch-up is demoted to an only-if-needed fallback.
	if !strings.Contains(out, "multica issue comment list "+issueID+" --since "+since+" --output json") {
		t.Errorf("hint must keep the issue-wide --since catch-up as a fallback, got:\n%s", out)
	}
	// The old cursor-heavy paragraph must be gone.
	if strings.Contains(out, "Next reply cursor") || strings.Contains(out, "--before-id") {
		t.Errorf("the old cursor-pagination paragraph must not render, got:\n%s", out)
	}
}

// TestBuildPromptColdStartThreadRead pins the cold-start case: no prior run means
// no since anchor (NewCommentsSince empty), so we suppress the delta hint and
// instead point the agent at the triggering CONVERSATION (--thread <trigger>
// --tail 30) rather than dumping the flat timeline.
func TestBuildPromptColdStartThreadRead(t *testing.T) {
	const issueID = "issue-cold-1"
	task := Task{
		IssueID:               issueID,
		TriggerCommentID:      "trigger-1",
		TriggerThreadID:       "thread-root-1",
		TriggerCommentContent: "hi",
		TriggerAuthorType:     "member",
		NewCommentCount:       0,
		NewCommentsSince:      "",
	}
	out := BuildPrompt(task, "claude")
	if strings.Contains(out, "new comment(s) since your last run") {
		t.Errorf("no since-delta hint should render on cold start, got:\n%s", out)
	}
	if !strings.Contains(out, "multica issue comment list "+issueID+" --thread thread-root-1 --tail 30 --output json") {
		t.Errorf("cold start must point at the triggering thread read, got:\n%s", out)
	}
	// MUL-5372: cross-thread background is a cheap roots scan. The hint names
	// only the reads it wants run — `--recent` and its saturation trap are
	// documented once in the brief's `## Available Commands`, so restating the
	// flag surface here would put reference text on every cold turn.
	if !strings.Contains(out, "multica issue comment list "+issueID+" --roots-only --summary --output json") {
		t.Errorf("cold start should offer the cheap roots scan for cross-thread background, got:\n%s", out)
	}
	if strings.Contains(out, "--recent") {
		t.Errorf("cold start hint should not restate the --recent surface, got:\n%s", out)
	}
}

// TestBuildPromptResumedNoDeltaDoesNotForceThreadRead pins the warm/no-delta
// path: when a prior provider session is actually being resumed, the triggering
// comment is already embedded in the per-turn prompt, so the agent should not
// be told to re-read the triggering thread's latest 30 replies by default.
func TestBuildPromptResumedNoDeltaDoesNotForceThreadRead(t *testing.T) {
	const issueID = "issue-resumed-1"
	task := Task{
		IssueID:               issueID,
		TriggerCommentID:      "trigger-1",
		TriggerThreadID:       "thread-root-1",
		TriggerCommentContent: "hi again",
		TriggerAuthorType:     "member",
		PriorSessionID:        "session-123",
		NewCommentCount:       0,
		NewCommentsSince:      "",
	}
	out := BuildPrompt(task, "claude")

	for _, want := range []string{
		"triggering comment is already included above",
		"No other new comments on this issue since your last run",
		"active thread anchor `thread-root-1` and triggering comment ID `trigger-1`",
		"If your reply depends on thread context",
		"do not rely only on resumed session memory",
		"multica issue comment list " + issueID + " --thread thread-root-1 --tail 30 --output json",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("resumed/no-delta prompt missing %q\n--- output ---\n%s", want, out)
		}
	}
	// The stale thread-scoped wording (since-delta used to be thread-scoped)
	// must not reappear.
	if strings.Contains(out, "scoped to the triggering thread") {
		t.Errorf("resumed/no-delta prompt must not claim the delta is thread-scoped, got:\n%s", out)
	}
	if strings.Contains(out, "Read the triggering conversation first") {
		t.Errorf("resumed/no-delta prompt must not use the cold-start forced-read wording, got:\n%s", out)
	}
}

// TestBuildCommentPromptCoalescedCrossThread pins MUL-4195 review should-fix #3:
// when a run coalesces comments that span MULTIPLE threads, the prompt must
// embed each folded comment's content with its OWN thread id instead of
// claiming they all live in the triggering thread. The earlier version told the
// agent "they are in the triggering thread" and handed a single `--thread`
// command — wrong (and lossy) when the folded comments came from different
// threads.
func TestBuildCommentPromptCoalescedCrossThread(t *testing.T) {
	task := Task{
		IssueID:               "issue-xthread-1",
		TriggerCommentID:      "trigger-newest",
		TriggerThreadID:       "thread-root-A",
		TriggerCommentContent: "latest instruction",
		TriggerAuthorType:     "member",
		CoalescedCommentIDs:   []string{"c-old-1", "c-old-2"},
		CoalescedComments: []CoalescedCommentData{
			{ID: "c-old-1", ThreadID: "thread-root-A", AuthorType: "member", AuthorName: "Alice", Content: "first earlier comment", CreatedAt: "2026-07-08T01:00:00Z"},
			{ID: "c-old-2", ThreadID: "thread-root-B", AuthorType: "member", AuthorName: "Bob", Content: "comment in a different thread", CreatedAt: "2026-07-08T02:00:00Z"},
		},
	}
	out := BuildPrompt(task, "claude")

	// The stale same-thread assumption must be gone.
	if strings.Contains(out, "they are in the triggering thread") {
		t.Errorf("prompt must not assume coalesced comments share the triggering thread, got:\n%s", out)
	}
	// Each folded comment's content is embedded directly, so the agent never
	// has to guess which thread to read to find it.
	for _, want := range []string{"first earlier comment", "comment in a different thread"} {
		if !strings.Contains(out, want) {
			t.Errorf("prompt must embed coalesced comment content %q, got:\n%s", want, out)
		}
	}
	// Each distinct thread id is surfaced so a follow-up fetch targets the
	// right thread — including the OTHER thread (B), not just the trigger's.
	for _, want := range []string{"thread-root-A", "thread-root-B"} {
		if !strings.Contains(out, want) {
			t.Errorf("prompt must surface coalesced comment thread id %q, got:\n%s", want, out)
		}
	}
	// Both coalesced comment ids remain referenced.
	for _, id := range []string{"c-old-1", "c-old-2"} {
		if !strings.Contains(out, id) {
			t.Errorf("prompt must reference coalesced comment id %s, got:\n%s", id, out)
		}
	}
}

// TestBuildCommentPromptCoalescedIDsOnlyFallback pins the old-server fallback:
// when only coalesced ids are shipped (no embedded detail), the prompt must
// still NOT assume a shared thread and must point at an issue-wide fetch.
func TestBuildCommentPromptCoalescedIDsOnlyFallback(t *testing.T) {
	task := Task{
		IssueID:               "issue-fallback-1",
		TriggerCommentID:      "trigger-newest",
		TriggerThreadID:       "thread-root-A",
		TriggerCommentContent: "latest instruction",
		TriggerAuthorType:     "member",
		CoalescedCommentIDs:   []string{"c-old-1", "c-old-2"},
	}
	out := BuildPrompt(task, "claude")

	if strings.Contains(out, "they are in the triggering thread") {
		t.Errorf("id-only fallback must not assume a shared thread, got:\n%s", out)
	}
	if !strings.Contains(out, "--recent 30") {
		t.Errorf("id-only fallback must point at an issue-wide fetch (--recent 30), got:\n%s", out)
	}
	for _, id := range []string{"c-old-1", "c-old-2"} {
		if !strings.Contains(out, id) {
			t.Errorf("id-only fallback must reference coalesced comment id %s, got:\n%s", id, out)
		}
	}
}

// TestCommentReplyThreadsGrouping pins the server-side grouping that drives
// per-thread reply routing (MUL-4348). The invariants:
//   - three distinct root threads → three targets, each replying to its own
//     thread (the trigger's thread replies under the trigger comment itself).
//   - multiple coalesced follow-ups in the SAME thread → a single group, so the
//     caller keeps the single-parent path and the reply is never duplicated.
//   - no coalesced comments (ordinary single comment) → nil.
func TestCommentReplyThreadsGrouping(t *testing.T) {
	t.Run("three distinct root threads fan out", func(t *testing.T) {
		task := Task{
			TriggerCommentID: "c3",
			TriggerThreadID:  "c3", // a root comment is its own thread
			CoalescedComments: []CoalescedCommentData{
				{ID: "c1", ThreadID: "c1", Content: "背一首宋词"},
				{ID: "c2", ThreadID: "c2", Content: "毛泽东诗词背一首"},
			},
		}
		targets := commentReplyThreads(task)
		if len(targets) != 3 {
			t.Fatalf("want 3 targets, got %d: %+v", len(targets), targets)
		}
		wantParent := map[string]string{"c1": "c1", "c2": "c2", "c3": "c3"}
		for _, tgt := range targets {
			if wantParent[tgt.ThreadID] != tgt.ParentID {
				t.Errorf("thread %s: parent = %s, want %s", tgt.ThreadID, tgt.ParentID, wantParent[tgt.ThreadID])
			}
		}
	})

	t.Run("same-thread follow-ups consolidate to a single group", func(t *testing.T) {
		task := Task{
			TriggerCommentID: "c3",
			TriggerThreadID:  "thread-A",
			CoalescedComments: []CoalescedCommentData{
				{ID: "c1", ThreadID: "thread-A", Content: "追问 1"},
				{ID: "c2", ThreadID: "thread-A", Content: "追问 2"},
			},
		}
		if targets := commentReplyThreads(task); targets != nil {
			t.Fatalf("same-thread follow-ups must not fan out; got %d targets: %+v", len(targets), targets)
		}
	})

	t.Run("mixed: trigger thread plus one other thread", func(t *testing.T) {
		task := Task{
			TriggerCommentID: "c3",
			TriggerThreadID:  "thread-A",
			CoalescedComments: []CoalescedCommentData{
				{ID: "c1", ThreadID: "thread-A", Content: "same-thread follow-up"},
				{ID: "c2", ThreadID: "thread-B", Content: "other thread"},
			},
		}
		targets := commentReplyThreads(task)
		if len(targets) != 2 {
			t.Fatalf("want 2 targets (thread-A, thread-B), got %d: %+v", len(targets), targets)
		}
		got := map[string]string{}
		for _, tgt := range targets {
			got[tgt.ThreadID] = tgt.ParentID
		}
		// The trigger's own thread replies under the trigger comment, not its root.
		if got["thread-A"] != "c3" {
			t.Errorf("trigger thread parent = %q, want c3 (the trigger comment)", got["thread-A"])
		}
		// The other thread replies under the specific comment that mentioned the
		// agent (a mid-thread reply), not the thread root — fixes the placement
		// asymmetry from the first cut.
		if got["thread-B"] != "c2" {
			t.Errorf("other thread parent = %q, want c2 (the specific mentioning comment)", got["thread-B"])
		}
	})

	t.Run("no coalesced comments → nil", func(t *testing.T) {
		task := Task{TriggerCommentID: "c1", TriggerThreadID: "thread-A"}
		if targets := commentReplyThreads(task); targets != nil {
			t.Fatalf("ordinary single-comment run must not fan out; got %+v", targets)
		}
	})

	t.Run("non-trigger thread replies under its newest mention, not root", func(t *testing.T) {
		// Two mid-thread mentions in thread-B (oldest c1, newer c2); the reply
		// should target the newest specific comment (c2), not the root thread-B.
		task := Task{
			TriggerCommentID: "c9",
			TriggerThreadID:  "thread-A",
			CoalescedComments: []CoalescedCommentData{
				{ID: "c1", ThreadID: "thread-B", Content: "older mention", CreatedAt: "2026-07-10T01:00:00Z"},
				{ID: "c2", ThreadID: "thread-B", Content: "newer mention", CreatedAt: "2026-07-10T02:00:00Z"},
			},
		}
		targets := commentReplyThreads(task)
		got := map[string]string{}
		for _, tgt := range targets {
			got[tgt.ThreadID] = tgt.ParentID
		}
		if got["thread-B"] != "c2" {
			t.Errorf("thread-B parent = %q, want newest mention c2 (not root)", got["thread-B"])
		}
		if got["thread-A"] != "c9" {
			t.Errorf("trigger thread parent = %q, want trigger c9", got["thread-A"])
		}
	})
}

// TestBuildCommentPromptCrossThreadFansOutReplies is the end-to-end prompt
// assertion for the screenshot scenario: three separate root comments coalesced
// into one run must produce a per-thread reply plan (one reply per thread),
// explicitly overriding the "one comment per run" rule, instead of the single
// --parent cookbook.
func TestBuildCommentPromptCrossThreadFansOutReplies(t *testing.T) {
	task := Task{
		IssueID:               "issue-xthread-2",
		TriggerCommentID:      "c3",
		TriggerThreadID:       "c3",
		TriggerCommentContent: "莎士比亚名言来一句",
		TriggerAuthorType:     "member",
		CoalescedCommentIDs:   []string{"c1", "c2"},
		CoalescedComments: []CoalescedCommentData{
			{ID: "c1", ThreadID: "c1", AuthorType: "member", AuthorName: "Yushen", Content: "背一首宋词", CreatedAt: "2026-07-10T01:00:00Z"},
			{ID: "c2", ThreadID: "c2", AuthorType: "member", AuthorName: "Yushen", Content: "毛泽东诗词背一首", CreatedAt: "2026-07-10T02:00:00Z"},
		},
	}
	out := BuildPrompt(task, "claude")

	for _, want := range []string{
		"3 DISTINCT threads",
		"Post ONE reply per thread",
		"OVERRIDES",
		"--parent c1",
		"--parent c2",
		"--parent c3",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("cross-thread prompt must contain %q, got:\n%s", want, out)
		}
	}
	// The single-parent cookbook must NOT be used when fanning out.
	if strings.Contains(out, "always use the trigger comment ID below") {
		t.Errorf("cross-thread prompt must not emit the single-parent reply cookbook, got:\n%s", out)
	}

	// Chronological ordering (MUL-4348 test-round-2 problem #1): replies must be
	// posted oldest thread first, the newest (triggering) thread last — so the
	// coalesced comments c1 (oldest) and c2 come before the trigger c3.
	if !strings.Contains(out, "OLDEST thread first") {
		t.Errorf("cross-thread prompt must instruct oldest-first chronological order, got:\n%s", out)
	}
	posC1 := strings.Index(out, "--parent c1")
	posC2 := strings.Index(out, "--parent c2")
	posC3 := strings.Index(out, "--parent c3")
	if !(posC1 >= 0 && posC1 < posC2 && posC2 < posC3) {
		t.Errorf("reply targets must be listed oldest-first (c1 < c2 < c3); got positions c1=%d c2=%d c3=%d\n%s", posC1, posC2, posC3, out)
	}
}

// TestBuildCommentPromptSameThreadKeepsSingleReply pins the hard requirement:
// multiple @mentions coalesced from the SAME thread must keep the ordinary
// single-parent reply path (one reply, under the trigger comment) and must NOT
// trigger the multi-thread fan-out.
func TestBuildCommentPromptSameThreadKeepsSingleReply(t *testing.T) {
	task := Task{
		IssueID:               "issue-samethread-1",
		TriggerCommentID:      "c3",
		TriggerThreadID:       "thread-A",
		TriggerCommentContent: "追问 3",
		TriggerAuthorType:     "member",
		CoalescedCommentIDs:   []string{"c1", "c2"},
		CoalescedComments: []CoalescedCommentData{
			{ID: "c1", ThreadID: "thread-A", AuthorType: "member", AuthorName: "Yushen", Content: "追问 1", CreatedAt: "2026-07-10T01:00:00Z"},
			{ID: "c2", ThreadID: "thread-A", AuthorType: "member", AuthorName: "Yushen", Content: "追问 2", CreatedAt: "2026-07-10T02:00:00Z"},
		},
	}
	out := BuildPrompt(task, "claude")

	if strings.Contains(out, "DISTINCT threads") {
		t.Errorf("same-thread coalescing must not emit the multi-thread fan-out block, got:\n%s", out)
	}
	// The single-parent cookbook is used, threading the one reply under the
	// trigger comment.
	if !strings.Contains(out, "--parent c3 --content-file ./reply.md") {
		t.Errorf("same-thread run must keep the single --parent=trigger reply cookbook, got:\n%s", out)
	}
}

// TestPerTurnContextBlocksCarryMovedBriefSections is the other half of
// MUL-5377: the per-run context that was removed from the runtime brief must
// still reach the agent, now via the per-turn user message. Losing it silently
// would be a worse regression than the cache cost it fixes.
func TestPerTurnContextBlocksCarryMovedBriefSections(t *testing.T) {
	t.Parallel()

	task := Task{
		IssueID:                       "issue-1",
		TriggerCommentID:              "comment-1",
		TriggerCommentContent:         "please look at this",
		PriorSessionResumeUnavailable: true,
		InitiatorType:                 "member",
		InitiatorName:                 "Bohan",
		InitiatorEmail:                "bohan@example.com",
		ConnectedApps: []ConnectedAppData{{
			Provider:    "composio",
			ServerName:  "composio",
			ToolkitSlug: "notion",
			ToolkitName: "Notion",
		}},
	}

	prompt := BuildPrompt(task, "claude")
	for _, want := range []string{
		"## Session Continuity Notice",
		"could NOT be restored",
		"## Task Initiator",
		"initiated by **Bohan** (bohan@example.com), a member of this workspace",
		"credentials stay scoped to the runtime owner",
		"## Connected Apps",
		"- Notion (`notion`) via MCP server `composio`",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("per-turn prompt lost moved brief content %q\n---\n%s", want, prompt)
		}
	}
}

// The blocks are per-run, so they must be absent when their preconditions are.
func TestPerTurnContextBlocksOmittedWhenEmpty(t *testing.T) {
	t.Parallel()

	prompt := BuildPrompt(Task{IssueID: "issue-1"}, "claude")
	for _, banned := range []string{
		"## Session Continuity Notice",
		"## Task Initiator",
		"## Connected Apps",
	} {
		if strings.Contains(prompt, banned) {
			t.Errorf("per-turn prompt must not emit %q with no data\n---\n%s", banned, prompt)
		}
	}
}

// An assignment-triggered run carries the initiator too — it is not a
// comment-path-only block.
func TestPerTurnContextBlocksOnAssignmentPath(t *testing.T) {
	t.Parallel()

	prompt := BuildPrompt(Task{
		IssueID:       "issue-1",
		InitiatorType: "agent",
		InitiatorName: "GPT-Boy",
	}, "claude")
	if !strings.Contains(prompt, "initiated by **GPT-Boy**, another agent in this workspace") {
		t.Errorf("assignment-triggered prompt lost the initiator block\n---\n%s", prompt)
	}
}

// TestTurnModeMarkerAlwaysPresent is the regression guard for the review
// finding on #6021: the brief's mode router keys off an explicit marker in the
// per-turn prompt, so that marker must be emitted unconditionally from the same
// branch that selects the code path.
//
// The dangerous case is a comment-triggered run whose comment body is empty (or
// an older server that doesn't send one). Before this guard the prompt emitted
// no `[NEW COMMENT]` block at all, the brief fell through to Ownership mode,
// and the agent would change the issue status on a turn that must not.
func TestTurnModeMarkerAlwaysPresent(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		task Task
		want string
		deny string
	}{
		{
			name: "comment-triggered with content",
			task: Task{IssueID: "issue-1", TriggerCommentID: "c-1", TriggerCommentContent: "please look"},
			want: "**Turn mode: Reply.**",
			deny: "**Turn mode: Ownership.**",
		},
		{
			name: "comment-triggered with EMPTY content",
			task: Task{IssueID: "issue-1", TriggerCommentID: "c-1"},
			want: "**Turn mode: Reply.**",
			deny: "**Turn mode: Ownership.**",
		},
		{
			name: "assignment-triggered",
			task: Task{IssueID: "issue-1"},
			want: "**Turn mode: Ownership.**",
			deny: "**Turn mode: Reply.**",
		},
		{
			name: "assignment-triggered with handoff note",
			task: Task{IssueID: "issue-1", HandoffNote: "start with the API"},
			want: "**Turn mode: Ownership.**",
			deny: "**Turn mode: Reply.**",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prompt := BuildPrompt(tc.task, "claude")
			if !strings.Contains(prompt, tc.want) {
				t.Errorf("prompt missing turn-mode marker %q\n---\n%s", tc.want, prompt)
			}
			if strings.Contains(prompt, tc.deny) {
				t.Errorf("prompt carries the wrong turn-mode marker %q\n---\n%s", tc.deny, prompt)
			}
		})
	}
}

// The mode marker only makes sense for the two issue paths — the issue-less
// kinds have no Reply/Ownership distinction and no issue status to protect.
func TestTurnModeMarkerAbsentOnIssuelessKinds(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		task Task
	}{
		{"chat", Task{ChatSessionID: "chat-1"}},
		{"quick-create", Task{QuickCreatePrompt: "make an issue"}},
		{"autopilot", Task{AutopilotRunID: "run-1"}},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prompt := BuildPrompt(tc.task, "claude")
			for _, banned := range []string{"**Turn mode: Reply.**", "**Turn mode: Ownership.**"} {
				if strings.Contains(prompt, banned) {
					t.Errorf("%s prompt must not carry %q\n---\n%s", tc.name, banned, prompt)
				}
			}
		})
	}
}

// The brief's router must describe the markers the prompt actually emits.
// A drift here is exactly the bug this pair of changes fixes, and it is
// invisible at runtime until an agent silently picks the wrong mode.
func TestBriefModeRouterMatchesPromptMarkers(t *testing.T) {
	t.Parallel()

	brief, err := execenv.InjectRuntimeConfig(t.TempDir(), "claude", execenv.TaskContextForEnv{IssueID: "issue-1"})
	if err != nil {
		t.Fatalf("InjectRuntimeConfig: %v", err)
	}
	for _, want := range []string{"`Turn mode: Reply.`", "`Turn mode: Ownership.`"} {
		if !strings.Contains(brief, want) {
			t.Errorf("brief mode router does not name %s\n---\n%s", want, brief)
		}
	}
	// The retired wording keyed off the prompt's first line, which was never
	// actually the [NEW COMMENT] block.
	if strings.Contains(brief, "It opens with a `[NEW COMMENT]` block") {
		t.Error("brief still routes on the prompt's opening line; it must route on the explicit marker")
	}
}
