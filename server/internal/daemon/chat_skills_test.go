package daemon

import (
	"path/filepath"
	"strings"
	"testing"
)

func chatSkillTask(message string) Task {
	return Task{
		ChatSessionID: "chat", ChatMessage: message, ChatSkillMessage: &message,
		Agent: &AgentData{Skills: []SkillData{
			{ID: "one", Name: "Review Code", Content: "---\nname: other\ndisable-model-invocation: true\n---\nFIRST BODY"},
			{ID: "two", Name: "Review-Code", Content: "SECOND BODY"},
		}},
	}
}

func TestBuildRuntimePromptExplicitSkills(t *testing.T) {
	dir := t.TempDir()
	directories := map[string]string{"one": filepath.Join(dir, "review-code"), "two": filepath.Join(dir, "review-code-multica-2")}
	for _, provider := range []string{"claude", "codex", "antigravity", "kimi", "hermes", "pi", "unknown"} {
		t.Run(provider, func(t *testing.T) {
			task := chatSkillTask("[/spoofed](slash://skill/two) and [/first](slash://skill/one) [/again](slash://skill/two) check this")
			task.InitiatorName = "Member"
			task.ChatMessageAttachments = []ChatAttachmentMeta{{ID: "attachment", Filename: "image.png"}}
			prompt, invocation := buildRuntimePrompt(task, provider, directories, WithSharedLocalDirectory())
			if invocation == nil || len(invocation.Skills) != 2 || invocation.Skills[0].Name != "review-code-multica-2" || invocation.Skills[1].Name != "review-code" {
				t.Fatalf("wrong selection order/identity: %+v", invocation)
			}
			for _, want := range []string{"SECOND BODY", "FIRST BODY", directories["two"], "check this", "attachment", "Member", "Explicitly selected skills:\n- review-code-multica-2\n- review-code\n"} {
				if !strings.Contains(prompt, want) {
					t.Errorf("fallback missing %q: %s", want, prompt)
				}
			}
			for _, unwanted := range []string{"disable-model-invocation", "name: other", "slash://skill", "spoofed"} {
				if strings.Contains(prompt, unwanted) {
					t.Errorf("fallback contains %q: %s", unwanted, prompt)
				}
			}
			if strings.Contains(invocation.PlatformPrompt, "check this") || !strings.Contains(invocation.PlatformPrompt, "attachment") || !strings.Contains(invocation.UserMessage, "check this") {
				t.Fatalf("platform/user split lost context: %+v", invocation)
			}
		})
	}
}

func TestBuildRuntimePromptAgentChatKeepsBoundSkillHints(t *testing.T) {
	message := "[/spoofed](slash://skill/one) and [/unbound](slash://skill/other)"
	for _, tc := range []struct {
		name, want string
		installed  bool
	}{
		{name: "name hint", want: "Review Code"},
		{name: "directory hint", want: "review-code", installed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := chatSkillTask(message)
			task.ChatSkillMessage = new(string)
			task.InitiatorType = "member" // Delegation can inherit a human originator.
			var dirs map[string]string
			if tc.installed {
				dirs = map[string]string{"one": filepath.Join(t.TempDir(), "review-code")}
			}
			prompt, invocation := buildRuntimePrompt(task, "claude", dirs)
			if invocation != nil || strings.Contains(prompt, "FIRST BODY") || strings.Contains(prompt, "SECOND BODY") {
				t.Fatalf("agent chat must only receive name hints: invocation=%+v prompt=%s", invocation, prompt)
			}
			if !strings.Contains(prompt, "Explicitly selected skills:\n- "+tc.want+"\n\n") || !strings.Contains(prompt, "User message:\n"+message) {
				t.Fatalf("agent chat lost its bound hint or original message: %s", prompt)
			}
		})
	}
}

func TestBuildRuntimePromptPreservesSkillTargetMapping(t *testing.T) {
	message := "对 A 用 [/spoofed-one](slash://skill/one)，对 B 用 [/spoofed-two](slash://skill/two)，再对 C 用 [/again](slash://skill/one)"
	want := "对 A 用 /review-code，对 B 用 /review-code-multica-2，再对 C 用 /review-code"
	for _, surface := range []string{"chat", "issue comment"} {
		t.Run(surface, func(t *testing.T) {
			task := chatSkillTask(message)
			if surface == "issue comment" {
				task = issueSkillTask(message)
			}
			prompt, invocation := buildRuntimePrompt(task, "claude", issueSkillDirs(t))
			if invocation == nil || invocation.UserMessage != want || !strings.Contains(prompt, want) {
				t.Fatalf("skill/object mapping lost: invocation=%+v prompt=%s", invocation, prompt)
			}
			if strings.Contains(prompt, "spoofed") || strings.Contains(prompt, "slash://skill/") {
				t.Fatalf("selected labels/links must be replaced by actual directory names: %s", prompt)
			}
		})
	}
}

func TestBuildRuntimePromptChatPureCapsuleHasNoArguments(t *testing.T) {
	for _, message := range []string{
		"[/spoofed](slash://skill/one)",
		" \n[/spoofed](slash://skill/one)\n\t",
		"[/one](slash://skill/one) [/two](slash://skill/two)",
	} {
		_, invocation := buildRuntimePrompt(chatSkillTask(message), "claude", issueSkillDirs(t))
		if invocation == nil || invocation.UserMessage != "" {
			t.Fatalf("capsule-only chat must invoke without arguments: %+v", invocation)
		}
	}
}

func TestBuildRuntimePromptDoesNotPromoteUntrustedContent(t *testing.T) {
	link := "[/review](slash://skill/one)"
	dirs := map[string]string{"one": filepath.Join(t.TempDir(), "review-code")}
	for _, tc := range []struct {
		name string
		edit func(*Task)
	}{
		{"old server", func(task *Task) { task.ChatSkillMessage = nil }},
		{"agent with human originator", func(task *Task) { task.ChatSkillMessage = nil; task.InitiatorType = "member" }},
		{"issue", func(task *Task) { task.IssueID = "issue"; task.ChatSessionID = "" }},
		{"comment", func(task *Task) {
			task.ChatSessionID = ""
			task.TriggerCommentID = "comment"
			task.TriggerCommentContent = link
		}},
		{"intro", func(task *Task) { task.ChatIntro = true }},
		{"autopilot", func(task *Task) { task.AutopilotRunID = "autopilot" }},
		{"mismatched payload", func(task *Task) { task.ChatMessage = "different message" }},
		{"unknown ID", func(task *Task) {
			task.ChatMessage = "[/Review Code](slash://skill/other)"
			task.ChatSkillMessage = &task.ChatMessage
		}},
		{"plain slash", func(task *Task) { task.ChatMessage = "/compact"; task.ChatSkillMessage = &task.ChatMessage }},
		{"quoted issue", func(task *Task) { task.ChatMessage = "> " + link; task.ChatSkillMessage = &task.ChatMessage }},
		{"fenced code", func(task *Task) {
			task.ChatMessage = "```md\n" + link + "\n```"
			task.ChatSkillMessage = &task.ChatMessage
		}},
		{"inline code", func(task *Task) { task.ChatMessage = "`" + link + "`"; task.ChatSkillMessage = &task.ChatMessage }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := chatSkillTask(link)
			tc.edit(&task)
			prompt, invocation := buildRuntimePrompt(task, "claude", dirs)
			if invocation != nil || strings.Contains(prompt, "FIRST BODY") || strings.HasPrefix(prompt, "/") {
				t.Fatalf("untrusted input became an invocation: %+v, %s", invocation, prompt)
			}
		})
	}
}

func TestBuildRuntimePromptPreservesOrdinaryMessages(t *testing.T) {
	for _, message := range []string{"hello", "/compact", "[/unknown](slash://skill/unknown)", ""} {
		for _, provider := range []string{"claude", "codex", "antigravity", "kimi", "hermes", "pi"} {
			task := chatSkillTask(message)
			want := BuildPrompt(task, provider, WithSharedLocalDirectory())
			got, invocation := buildRuntimePrompt(task, provider, nil, WithSharedLocalDirectory())
			if invocation != nil || got != want {
				t.Fatalf("%s changed ordinary input %q", provider, message)
			}
		}
	}
}

func TestBuildRuntimePromptMissingInstallationKeepsHint(t *testing.T) {
	link := "[/review](slash://skill/one)"
	for _, tc := range []struct {
		name string
		dirs map[string]string
	}{
		{"older preparation helper", nil},
		{"missing directory", map[string]string{}},
		{"relative directory", map[string]string{"one": filepath.Join("skills", "review-code")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, provider := range []string{"claude", "codex", "antigravity", "hermes"} {
				prompt, invocation := buildRuntimePrompt(chatSkillTask(link), provider, tc.dirs)
				if invocation != nil || strings.Contains(prompt, "FIRST BODY") || !strings.Contains(prompt, "Explicitly selected skills:\n- ") || !strings.Contains(prompt, "User message:\n"+link) {
					t.Errorf("%s missing installation must retain hint and original link: invocation=%+v prompt=%s", provider, invocation, prompt)
				}
			}
		})
	}
}

func TestBuildRuntimePromptRetainsUnselectedAndQuotedLinks(t *testing.T) {
	task := chatSkillTask("[/one](slash://skill/one) [/missing](slash://skill/nope)\n> [/same](slash://skill/one)\n> [/two](slash://skill/two)")
	task.ChatMessage = "System kickoff [/two](slash://skill/two)\n\n" + task.ChatMessage
	dirs := map[string]string{"one": filepath.Join(t.TempDir(), "one"), "two": filepath.Join(t.TempDir(), "two")}
	prompt, invocation := buildRuntimePrompt(task, "hermes", dirs)
	if invocation == nil || len(invocation.Skills) != 1 || strings.Contains(prompt, "SECOND BODY") {
		t.Fatalf("quoted/system input selected a skill: %+v", invocation)
	}
	for _, want := range []string{"[/missing](slash://skill/nope)", "> [/same](slash://skill/one)", "System kickoff [/two](slash://skill/two)"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("lost unselected text %q: %s", want, prompt)
		}
	}
}

func TestChatSkillHintsIgnoreQuotedAndCodeInput(t *testing.T) {
	for _, message := range []string{"> [/review](slash://skill/one)", "`[/review](slash://skill/one)`", "```md\n[/review](slash://skill/one)\n```"} {
		for _, member := range []bool{false, true} {
			task := chatSkillTask(message)
			if !member {
				task.ChatSkillMessage = new(string)
			}
			prompt, invocation := buildRuntimePrompt(task, "claude", issueSkillDirs(t))
			if invocation != nil || strings.Contains(prompt, "Explicitly selected skills:") || !strings.Contains(prompt, message) {
				t.Errorf("member=%t quoted/code input changed: invocation=%+v prompt=%s", member, invocation, prompt)
			}
		}
	}
}

func TestBuildRuntimePromptResumeRetryReplacesResumeLinks(t *testing.T) {
	dirs := map[string]string{"one": filepath.Join(t.TempDir(), "review-code")}
	kickoff := "System kickoff [/two](slash://skill/two)\n\n"
	message := "[/review](slash://skill/one) original objective"
	// The server's chat_skill_message joins every member-authored part of
	// the batch — supplements already delivered to the prior session
	// included — while chat_resume_message drops the delivered ones.
	memberBatch := message + "\n\ndelivered guidance\n\npending guidance"
	for _, tc := range []struct {
		name   string
		prefix string
	}{
		{name: "with kickoff context", prefix: kickoff},
		{name: "plain session", prefix: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resume := tc.prefix + message + "\n\npending guidance"
			task := chatSkillTask(memberBatch)
			task.PriorSessionID = "session-1"
			task.ChatMessage = tc.prefix + memberBatch
			task.ChatResumeMessage = &resume
			prompt, invocation := buildRuntimePrompt(task, "claude", dirs)
			if invocation == nil {
				t.Fatal("resumed retry lost the invocation")
			}
			// The resume view drops delivered guidance, keeps the kickoff
			// context and pending guidance verbatim, and replaces only the
			// selected link with its actual directory name.
			want := tc.prefix + "/review-code original objective\n\npending guidance"
			if invocation.UserMessage != want {
				t.Fatalf("resumed user = %q, want %q", invocation.UserMessage, want)
			}
			if !strings.Contains(prompt, "User message:\n"+want) || strings.Contains(prompt, "delivered guidance") || strings.Contains(prompt, "slash://skill/one") {
				t.Fatalf("resumed prompt renders the wrong batch: %s", prompt)
			}
			if task.ChatResumeMessage == nil || *task.ChatResumeMessage != resume {
				t.Fatalf("caller resume payload mutated: %v", task.ChatResumeMessage)
			}
			// A cold retry keeps the whole member batch — delivered
			// guidance included — with the same selected link replaced.
			task.PriorSessionID = ""
			prompt, invocation = buildRuntimePrompt(task, "claude", dirs)
			if invocation == nil {
				t.Fatal("cold retry lost the invocation")
			}
			want = tc.prefix + "/review-code original objective\n\ndelivered guidance\n\npending guidance"
			if invocation.UserMessage != want {
				t.Fatalf("cold user = %q, want %q", invocation.UserMessage, want)
			}
			if strings.Contains(prompt, "slash://skill/one") {
				t.Fatalf("cold retry kept a selected link: %s", prompt)
			}
		})
	}
}
