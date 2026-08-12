package execenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildCommentReplyInstructionsCodexLinux pins that the Linux/macOS
// reply template now mandates `--content-file` (post-#4182). The previous
// `--content-stdin` + HEREDOC mandate (#1795 / #1851 / MUL-2904) was kept
// for years to defend against backtick / `$()` substitution in the body,
// but the heredoc/flag boundary turned out to be fragile in its own right:
// when a model wrapped extra flags around the heredoc on `multica issue
// create`, the flags got swallowed into stdin and silently dropped (OXY-78,
// OXY-76). The file path defeats both classes — the body never reaches the
// shell, and all flags live on one shell-token line.
//
// Not parallel: mutates the package-level runtimeGOOS.
func TestBuildCommentReplyInstructionsCodexLinux(t *testing.T) {
	saved := runtimeGOOS
	t.Cleanup(func() { runtimeGOOS = saved })
	runtimeGOOS = "linux"

	issueID := "11111111-1111-1111-1111-111111111111"
	triggerID := "22222222-2222-2222-2222-222222222222"

	got := BuildCommentReplyInstructions("codex", issueID, triggerID, false)

	for _, want := range []string{
		"multica issue comment add " + issueID + " --parent " + triggerID + " --content-file ./reply.md",
		"Write the body file first",
		"--content-file ./reply.md",
		"#4182",
		"rm ./reply.md",
		"Do NOT write literal `\\n` escapes to simulate line breaks",
		"do NOT reuse --parent values from previous turns",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("codex/linux reply instructions missing %q\n---\n%s", want, got)
		}
	}

	for _, banned := range []string{
		"--content \"...\"",
		"<<'COMMENT'",
		"cat <<",
		"--parent " + triggerID + " --content-stdin",
	} {
		if strings.Contains(got, banned) {
			t.Fatalf("codex/linux reply instructions should not contain %q\n---\n%s", banned, got)
		}
	}
}

// TestBuildCommentReplyInstructionsNonCodexLinux pins that EVERY provider on
// Linux/macOS — not just Codex — gets the `--content-file` template. Two
// shell-driven failure classes motivate the uniform file path:
//   - MUL-2904 / OKK-497: an agent inlined a backtick-wrapped table name into
//     `--content`; the shell ran it as a command substitution, silently deleted
//     it, the stored comment no longer matched the model's intent, and the
//     model retried forever.
//   - GitHub #4182 (OXY-78 / OXY-76): an agent wrapped extra flags around an
//     `--content-stdin` HEREDOC; the bash heredoc/flag boundary swallowed
//     `--assignee` / `--project` into stdin or dropped them as failed
//     standalone shell statements, while the create still exited 0 with nulls.
//
// Both classes are shell-driven, so the guardrail is uniform across providers
// and across hosts.
//
// Not parallel: mutates the package-level runtimeGOOS.
func TestBuildCommentReplyInstructionsNonCodexLinux(t *testing.T) {
	saved := runtimeGOOS
	t.Cleanup(func() { runtimeGOOS = saved })

	issueID := "11111111-1111-1111-1111-111111111111"
	triggerID := "22222222-2222-2222-2222-222222222222"

	for _, host := range []string{"linux", "darwin"} {
		for _, provider := range []string{"claude", "opencode", "openclaw", "hermes", "kimi", "reasonix", "kiro", "cursor"} {
			name := provider + "/" + host
			t.Run(name, func(t *testing.T) {
				runtimeGOOS = host
				got := BuildCommentReplyInstructions(provider, issueID, triggerID, false)

				for _, want := range []string{
					"multica issue comment add " + issueID + " --parent " + triggerID + " --content-file ./reply.md",
					// MUL-5442 cross-channel dedup: shell-hazard mechanics live in
					// the brief's Comment Formatting; the cookbook keeps the
					// file-first order, the command, and the pointer.
					"Write the body file first",
					"## Comment Formatting",
					"#4182",
					"rm ./reply.md",
					"do NOT reuse --parent values from previous turns",
					"Post your reply as a comment",
				} {
					if !strings.Contains(got, want) {
						t.Errorf("%s reply instructions missing %q\n---\n%s", name, want, got)
					}
				}

				// The two regressions: agent-authored comments must never be
				// steered at inline `--content "..."` (MUL-2904) and never at
				// `--content-stdin` HEREDOC on multi-flag commands (#4182).
				for _, banned := range []string{
					"--content \"...\"",
					"<<'COMMENT'",
					"cat <<",
					"--parent " + triggerID + " --content-stdin",
				} {
					if strings.Contains(got, banned) {
						t.Errorf("%s reply instructions still contains %q\n---\n%s", name, banned, got)
					}
				}
			})
		}
	}
}

// TestBuildCommentReplyInstructionsWindowsUsesContentFile pins that on
// Windows every provider — Codex AND non-Codex — gets the
// `--content-file` template. The bug is shell-layer, not provider-layer:
// any agent on Windows piping HEREDOC through PowerShell loses non-ASCII
// bytes (PS 5.1's `$OutputEncoding` defaults to ASCIIEncoding). Issues
// #2198 (Chinese, Codex), #2236 (Chinese, Codex), #2376 (Cyrillic,
// non-Codex agent name) all match this signature.
//
// Not parallel: mutates the package-level runtimeGOOS.
func TestBuildCommentReplyInstructionsWindowsUsesContentFile(t *testing.T) {
	saved := runtimeGOOS
	t.Cleanup(func() { runtimeGOOS = saved })
	runtimeGOOS = "windows"

	issueID := "11111111-1111-1111-1111-111111111111"
	triggerID := "22222222-2222-2222-2222-222222222222"

	for _, provider := range []string{"codex", "claude", "opencode", "openclaw", "hermes", "kimi", "reasonix", "kiro", "cursor"} {
		t.Run(provider+"/windows", func(t *testing.T) {
			got := BuildCommentReplyInstructions(provider, issueID, triggerID, false)
			for _, want := range []string{
				"multica issue comment add " + issueID + " --parent " + triggerID + " --content-file",
				// MUL-5442 cross-channel dedup: the $OutputEncoding trap's
				// full mechanics live once, in the brief's Windows Comment
				// Formatting variant; the per-turn cookbook keeps the ban,
				// the one-line consequence, and the pointer.
				"Write the body file first",
				"never pipe via `--content-stdin`",
				"PowerShell drops non-ASCII",
				"## Comment Formatting",
			} {
				if !strings.Contains(got, want) {
					t.Errorf("%s reply instructions missing %q\n---\n%s", provider, want, got)
				}
			}
			for _, banned := range []string{
				"<<'COMMENT'",
				"--parent " + triggerID + " --content-stdin",
				"cat <<",
			} {
				if strings.Contains(got, banned) {
					t.Errorf("%s/windows reply instructions should not contain %q\n---\n%s", provider, banned, got)
				}
			}
		})
	}
}

func TestBuildCommentReplyInstructionsEmptyWhenNoTrigger(t *testing.T) {
	t.Parallel()

	for _, provider := range []string{"codex", "claude", "opencode"} {
		if got := BuildCommentReplyInstructions(provider, "issue-id", "", false); got != "" {
			t.Fatalf("expected empty string when triggerCommentID is empty for %s, got %q", provider, got)
		}
	}
}

// The brief must never carry this turn's trigger comment id; it points the
// agent at the per-turn user message instead (MUL-5377).
func TestInjectRuntimeConfigKeepsTriggerCommentOutOfBrief(t *testing.T) {
	saved := runtimeGOOS
	t.Cleanup(func() { runtimeGOOS = saved })
	runtimeGOOS = "linux"

	dir := t.TempDir()
	issueID := "11111111-1111-1111-1111-111111111111"
	triggerID := "22222222-2222-2222-2222-222222222222"

	if _, err := InjectRuntimeConfig(dir, "claude", TaskContextForEnv{
		IssueID:          issueID,
		TriggerCommentID: triggerID,
	}); err != nil {
		t.Fatalf("InjectRuntimeConfig failed: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	s := string(content)

	if strings.Contains(s, triggerID) {
		t.Errorf("CLAUDE.md must not carry the trigger comment id (MUL-5377)\n---\n%s", s)
	}
	for _, want := range []string{
		// MUL-5442 stage 1: the mode-router paragraph compressed to a
		// "Turn mode." lead. Pin every routing RULE, not just the markers —
		// a further compression that drops the one-block rule or the
		// no-mode-line fallback must fail here (stage-1 review).
		"**Turn mode.**",
		"Steps 1–6 are shared",
		"apply exactly one mode block",
		"differ on issue status",
		// The full fallback MAPPING, not its halves: "No mode line" and
		// "Reply mode" pinned separately could both pass while the text
		// says "No mode line → Ownership mode" (final-review catch).
		"No mode line → Reply mode",
		"do not change the issue status",
		"`Turn mode: Reply.`",
		"`Turn mode: Ownership.`",
		"Use the `--parent` value the per-turn user message gives you for this turn",
		"do NOT reuse a `--parent` from an earlier turn in this session",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("CLAUDE.md missing %q\n---\n%s", want, s)
		}
	}
}

// Windows reply instructions are file-first, never stdin. The instructions
// now ship in the per-turn prompt, so pin the helper directly (MUL-5377).
func TestWindowsCommentReplyInstructionsHaveNoStdin(t *testing.T) {
	saved := runtimeGOOS
	t.Cleanup(func() { runtimeGOOS = saved })
	runtimeGOOS = "windows"

	issueID := "11111111-1111-1111-1111-111111111111"
	triggerID := "22222222-2222-2222-2222-222222222222"

	for _, provider := range []string{"claude", "codex", "opencode"} {
		t.Run(provider, func(t *testing.T) {
			s := BuildCommentReplyInstructions(provider, issueID, triggerID, false)
			for _, want := range []string{
				"multica issue comment add " + issueID + " --parent " + triggerID + " --content-file",
				"--content-file",
			} {
				if !strings.Contains(s, want) {
					t.Errorf("%s reply instructions missing %q\n---\n%s", provider, want, s)
				}
			}
			for _, banned := range []string{
				"--parent " + triggerID + " --content-stdin",
				"always use `--content-stdin` with a HEREDOC, even for short single-line replies",
			} {
				if strings.Contains(s, banned) {
					t.Errorf("%s reply instructions must not prescribe stdin on Windows: %q\n---\n%s", provider, banned, s)
				}
			}
		})
	}
}

// TestInjectRuntimeConfigWindowsAssignmentBriefStaysFileOnly pins the PR #3654
// review fix: on Windows, the ASSIGNMENT-triggered brief must never *recommend*
// `--content-stdin`. Unlike the comment-trigger path, the assignment workflow
// has no BuildCommentReplyInstructions override, so an agent that follows the
// "post your final results" step literally would pipe its final comment through
// PowerShell and drop non-ASCII bytes (#2198 / #2236 / #2376). The OS-aware
// ## Comment Formatting section (file-only on Windows) is the single source of
// truth; the Available Commands entry and step 6 must defer to it, not re-offer
// stdin. The flag synopsis may still *list* `--content-stdin` as available.
//
// Not parallel: mutates the package-level runtimeGOOS.
func TestInjectRuntimeConfigWindowsAssignmentBriefStaysFileOnly(t *testing.T) {
	saved := runtimeGOOS
	t.Cleanup(func() { runtimeGOOS = saved })
	runtimeGOOS = "windows"

	// Assignment-triggered: IssueID set, no TriggerCommentID.
	ctx := TaskContextForEnv{IssueID: "issue-1"}

	for _, provider := range []string{"claude", "codex", "opencode"} {
		t.Run(provider, func(t *testing.T) {
			dir := t.TempDir()
			if _, err := InjectRuntimeConfig(dir, provider, ctx); err != nil {
				t.Fatalf("InjectRuntimeConfig failed: %v", err)
			}
			fileName := "CLAUDE.md"
			if provider != "claude" {
				fileName = "AGENTS.md"
			}
			data, err := os.ReadFile(filepath.Join(dir, fileName))
			if err != nil {
				t.Fatalf("read %s: %v", fileName, err)
			}
			s := string(data)

			// The Windows Comment Formatting section is file-only.
			for _, want := range []string{
				"## Comment Formatting",
				"On Windows, **always write the comment body to a UTF-8 file",
				"do NOT pipe via `--content-stdin`",
			} {
				if !strings.Contains(s, want) {
					t.Errorf("%s missing Windows file-only guidance %q\n---\n%s", fileName, want, s)
				}
			}

			// No prose may RECOMMEND stdin on Windows. The flag synopsis may
			// still list `--content-stdin`; only the prescriptive "file or
			// stdin" phrasings are banned.
			for _, banned := range []string{
				"or `--content-stdin`",
				"using `--content-file` or `--content-stdin`",
				"use `--content-file <path>` or `--content-stdin`",
			} {
				if strings.Contains(s, banned) {
					t.Errorf("%s recommends stdin on Windows: %q\n---\n%s", fileName, banned, s)
				}
			}
		})
	}
}

// TestBuildCommentReplyInstructionsSquadLeaderCarveOut pins that the squad
// leader variant scopes the reply imperative with the `no_action` exception
// (MUL-5442 #6493 review): the leader's only silent path must not be
// contradicted by a later unconditional "Post your reply" line, and the
// ordinary variant must never carry the leader carve-out.
func TestBuildCommentReplyInstructionsSquadLeaderCarveOut(t *testing.T) {
	t.Parallel()

	leader := BuildCommentReplyInstructions("claude", "issue-1", "comment-1", true)
	if !strings.Contains(leader, "Unless your outcome is `no_action`, post your reply as a comment") {
		t.Errorf("leader reply instructions missing the no_action carve-out\n---\n%s", leader)
	}
	if strings.Contains(leader, "Post your reply as a comment") {
		t.Errorf("leader reply instructions still carry the unconditional imperative\n---\n%s", leader)
	}

	ordinary := BuildCommentReplyInstructions("claude", "issue-1", "comment-1", false)
	if !strings.Contains(ordinary, "Post your reply as a comment") {
		t.Errorf("ordinary reply instructions missing the imperative\n---\n%s", ordinary)
	}
	if strings.Contains(ordinary, "Unless your outcome is") {
		t.Errorf("ordinary reply instructions leaked the leader carve-out\n---\n%s", ordinary)
	}
}
