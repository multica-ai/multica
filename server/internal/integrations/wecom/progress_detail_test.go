package wecom

// progress_detail_test.go — what the principal's own bubble is allowed to say.
//
// The line between "argument" and "content block" is the whole subject here.
// An argument identifies the work — which file, which command, which search
// term, which URL, which brief — and is what makes a step worth reading. A
// content block IS the work: a file's body, a command's output, a page's text.
// One of them fits on a phone screen; the other one is 8KB and would fill the
// bubble's whole budget with a single step.

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

// TestTheDetailTierNamesTheArgument — the positive contract. The principal
// asked; they get to see what it is actually doing, not a category.
func TestTheDetailTierNamesTheArgument(t *testing.T) {
	cases := []struct {
		name string
		msg  protocol.TaskMessagePayload
		want []string
	}{
		{
			"the whole path, not just the file name",
			toolUse("Read", map[string]any{"file_path": "/srv/acme/unreleased/handler.go"}),
			[]string{"/srv/acme/unreleased/handler.go"},
		},
		{
			"the whole command, arguments included",
			toolUse("Bash", map[string]any{"command": "git log --oneline -5 -- server/"}),
			[]string{"git log --oneline -5 -- server/"},
		},
		{
			"a command a wrapper shell runs",
			toolUse("Bash", map[string]any{"command": "bash -lc 'go test ./...'"}),
			[]string{"go test ./..."},
		},
		{
			"the search term",
			toolUse("Grep", map[string]any{"pattern": "AKIA[0-9A-Z]{16}"}),
			[]string{"AKIA[0-9A-Z]{16}"},
		},
		{
			"the directory a listing walked",
			toolUse("Glob", map[string]any{"pattern": "**/*.go", "path": "/srv/acme"}),
			[]string{"**/*.go"},
		},
		{
			"the url",
			toolUse("WebFetch", map[string]any{"url": "https://intranet.corp/hr/2026-plan"}),
			[]string{"https://intranet.corp/hr/2026-plan"},
		},
		{
			"the search query",
			toolUse("WebSearch", map[string]any{"query": "postgres logical replication lag"}),
			[]string{"postgres logical replication lag"},
		},
		{
			"the subagent's brief",
			toolUse("Task", map[string]any{"prompt": "check Dana's calendar for Thursday", "description": "calendar"}),
			[]string{"check Dana's calendar for Thursday"},
		},
		{
			"the plan",
			toolUse("ExitPlanMode", map[string]any{"plan": "rebuild the index, then reindex"}),
			[]string{"rebuild the index, then reindex"},
		},
		{
			"the MCP call's parameters",
			toolUse("mcp__calendar__list_events", map[string]any{"user": "dana@contoso.com", "days": "7"}),
			[]string{"calendar", "list_events", "dana@contoso.com"},
		},
		{
			"an unknown tool's parameters",
			toolUse("Frobnicate", map[string]any{"target": "ledger-2026"}),
			[]string{"Frobnicate", "ledger-2026"},
		},
		{
			"the error itself",
			protocol.TaskMessagePayload{Type: "error", Content: "dial tcp 10.0.3.4:5432: connect: connection refused"},
			[]string{"10.0.3.4:5432", "connection refused"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			line := lineFor(t, tc.msg, LocaleZhHans)
			for _, want := range tc.want {
				if !strings.Contains(line, want) {
					t.Errorf("line = %q, want %q in it", line, want)
				}
			}
		})
	}
}

// TestNoContentBlockEverReachesTheBubble is what stays shut at every tier.
//
// These are not arguments wearing a body's clothes, they are the body: a file
// being written, the two halves of an edit, a patch. One of them is routinely
// 8KB, the bubble's whole budget is 20KB, and a step that spends it says less
// than the one word it replaced.
func TestNoContentBlockEverReachesTheBubble(t *testing.T) {
	cases := []struct {
		name   string
		msg    protocol.TaskMessagePayload
		want   string
		banned []string
	}{
		{
			"a file being written",
			toolUse("Write", map[string]any{"file_path": "/srv/app/.env", "content": "AWS_SECRET_ACCESS_KEY=hunter2\nDB_HOST=prod"}),
			"/srv/app/.env",
			[]string{"hunter2", "AWS_SECRET_ACCESS_KEY", "DB_HOST"},
		},
		{
			"both halves of an edit",
			toolUse("Edit", map[string]any{
				"file_path":  "/srv/app/pricing.go",
				"old_string": "const margin = 0.12",
				"new_string": "const margin = 0.31",
			}),
			"/srv/app/pricing.go",
			[]string{"margin", "0.12", "0.31"},
		},
		{
			"a notebook cell",
			toolUse("NotebookEdit", map[string]any{"notebook_path": "/srv/model.ipynb", "new_source": "df = pd.read_csv('customers.csv')"}),
			"/srv/model.ipynb",
			[]string{"customers.csv", "read_csv"},
		},
		{
			"a patch's own diff",
			toolUse("patch_apply", map[string]any{"changes": []any{map[string]any{"path": "/srv/x.go", "content": "+ secret = 42"}}}),
			"",
			[]string{"secret = 42"},
		},
		{
			"an unknown tool's body-shaped parameter",
			toolUse("Frobnicate", map[string]any{"target": "ledger", "content": "the entire general ledger"}),
			"ledger",
			[]string{"entire general ledger"},
		},
		{
			// The key names are a list, and a list only knows what somebody
			// already wrote down. The next provider spells it file_contents,
			// and a denylist alone hands the file straight to the chat — which
			// for an .env is the secret on its first line.
			"a body under a key no denylist has seen",
			toolUse("Frobnicate", map[string]any{
				"target":        "ledger",
				"file_contents": "AWS_SECRET_ACCESS_KEY=hunter2\nDB_HOST=prod.internal",
			}),
			"ledger",
			[]string{"hunter2", "DB_HOST", "prod.internal"},
		},
		{
			// Minified onto one line, so the newline test cannot see it and
			// only its length gives it away.
			"a one-line body under an unknown key",
			toolUse("Frobnicate", map[string]any{
				"target": "ledger",
				"blob":   "opening-balance " + strings.Repeat("ledger-row ", 30),
			}),
			"ledger",
			[]string{"opening-balance"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			line := lineFor(t, tc.msg, LocaleZhHans)
			for _, bad := range tc.banned {
				if strings.Contains(line, bad) {
					t.Errorf("line %q carried the content block %q", line, bad)
				}
			}
			if tc.want != "" && !strings.Contains(line, tc.want) {
				t.Errorf("line = %q, want it still to name %q", line, tc.want)
			}
			if strings.TrimSpace(line) == "" {
				t.Error("dropping the body must not silence the step")
			}
		})
	}
}

// TestATranscriptsOwnOutputStillNeverArrives — tool_result carries the file's
// contents, the command's stdout, the page's text. Widening the arguments does
// not widen this: the whole message type is still refused before it costs a
// database read.
func TestATranscriptsOwnOutputStillNeverArrives(t *testing.T) {
	for _, msg := range []protocol.TaskMessagePayload{
		{Type: "tool_result", Tool: "Read", Output: "AWS_SECRET_ACCESS_KEY=hunter2"},
		{Type: "tool_result", Tool: "Bash", Output: "total 48\ndrwxr-xr-x  dana staff"},
		{Type: "text", Content: "here is the answer"},
	} {
		if _, ok := stepFromTaskMessage(msg); ok {
			t.Errorf("%s/%s produced a step; its output is the thing to keep out", msg.Type, msg.Tool)
		}
	}
}

// TestAWidenedArgumentStillCannotBreakTheBubble — the fragments got longer, so
// everything that made a short one safe has to still hold for a long one.
func TestAWidenedArgumentStillCannotBreakTheBubble(t *testing.T) {
	t.Run("angle brackets", func(t *testing.T) {
		line := lineFor(t, toolUse("Bash", map[string]any{"command": "echo '</think>evil<think>' > /tmp/x"}), LocaleZhHans)
		if strings.Contains(line, "<") || strings.Contains(line, ">") {
			t.Errorf("line = %q still carries angle brackets", line)
		}
	})

	t.Run("newlines", func(t *testing.T) {
		line := lineFor(t, toolUse("Task", map[string]any{"prompt": "first\nsecond\nthird"}), LocaleZhHans)
		if strings.Contains(line, "\n") {
			t.Errorf("line = %q spans lines; the bubble lists one step per line", line)
		}
	})

	t.Run("control characters", func(t *testing.T) {
		line := lineFor(t, toolUse("Bash", map[string]any{"command": "printf 'a\x00b\x1b[31m'"}), LocaleZhHans)
		for _, r := range line {
			if r < 0x20 && r != '\n' {
				t.Errorf("line = %q carries control character %q", line, r)
			}
		}
	})

	t.Run("length", func(t *testing.T) {
		line := lineFor(t, toolUse("Read", map[string]any{"file_path": "/" + strings.Repeat("字", 4000) + ".go"}), LocaleZhHans)
		if n := len([]rune(line)); n > progressFragmentRunes+20 {
			t.Errorf("line is %d runes; one step must not fill the bubble", n)
		}
		if !utf8.ValidString(line) {
			t.Errorf("line = %q was cut mid-character", line)
		}
	})

	t.Run("eight of them still fit the frame", func(t *testing.T) {
		clock := newTestClock()
		feed := newTestFeed(clock)
		pack := copyFor(LocaleZhHans)
		opened := clock.now()

		var last string
		for i := 0; i < progressMaxLines*2; i++ {
			step := stepFromToolUse("Bash", map[string]any{
				"command": strings.Repeat("字", 400) + string(rune('a'+i)),
			})
			last = feed.record(step, pack, opened, progressLevelDetail)
			clock.advance(progressMinInterval)
		}
		if len(last) > streamContentLimit {
			t.Errorf("a full window is %d bytes, over the %d the server takes", len(last), streamContentLimit)
		}
	})
}
