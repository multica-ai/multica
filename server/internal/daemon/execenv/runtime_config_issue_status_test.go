package execenv

import (
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/issuestatus"
)

// legacyStatusLine is the pre-MUL-6460 status bullet. Workspaces without
// custom statuses — including every deployment behind an old server — must
// keep rendering it byte-identical: it is part of the prompt-cache prefix and
// the no-custom-statuses path is the compatibility contract of MUL-6460.
const legacyStatusLine = "- `multica issue status <id> <status> [--no-start]` — flip status (todo / in_progress / in_review / done / blocked / backlog / cancelled).\n"

// catalogBridgeBullet distinguishes workflow keys from lifecycle categories;
// it must appear exactly when a catalog is present.
const catalogBridgeBullet = "- The workflow rules above refer to exact built-in status keys, not categories. Custom statuses share lifecycle semantics only, not built-in automation behavior.\n"

func TestBriefStatusCatalogAbsentKeepsLegacyLine(t *testing.T) {
	t.Parallel()
	base := TaskContextForEnv{IssueID: "issue-1", AgentID: "a-1", AgentName: "Eve"}
	out := buildMetaSkillContent("claude", base)
	if !strings.Contains(out, legacyStatusLine) {
		t.Fatalf("brief without a catalog must keep the legacy status line\n---\n%s", out)
	}
	if strings.Contains(out, catalogBridgeBullet) {
		t.Errorf("brief without a catalog must not carry the catalog bridge bullet")
	}

	withEmpty := base
	withEmpty.IssueStatuses = []IssueStatusForEnv{}
	if got := buildMetaSkillContent("claude", withEmpty); got != out {
		t.Errorf("empty catalog must render byte-identical to absent catalog:\n%s", firstBriefDiff(out, got))
	}
}

func TestBriefStatusCatalogRendered(t *testing.T) {
	t.Parallel()
	ctx := TaskContextForEnv{
		IssueID: "issue-1", AgentID: "a-1", AgentName: "Eve",
		IssueStatuses: []IssueStatusForEnv{
			{Key: "later", Name: "Later", Category: "backlog", Description: "Deferred on purpose"},
			{Key: "rework", Name: "Rework", Category: "todo"},
			{Key: "human_review", Name: "Human Review", Category: "in_review", Description: "Awaiting human acceptance"},
		},
	}
	out := buildMetaSkillContent("claude", ctx)
	if strings.Contains(out, legacyStatusLine) {
		t.Errorf("catalog brief must replace the legacy seven-value enumeration")
	}
	for _, want := range []string{
		"- `multica issue status <id> <status> [--no-start]` — flip status. Available statuses by lifecycle category:\n",
		"  - unstarted category: `backlog`, `todo` (built-in), `later` (Later — Deferred on purpose), `rework` (Rework)\n",
		"  - done category: `done` (built-in)\n",
		"  - started category: `in_progress`, `in_review`, `blocked` (built-in), `human_review` (Human Review — Awaiting human acceptance)\n",
		"  - closed category: `cancelled` (built-in)\n",
		catalogBridgeBullet,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("catalog brief missing %q\n---\n%s", want, out)
		}
	}
	if strings.Contains(out, "custom statuses not listed") || strings.Contains(out, "Custom statuses omitted") {
		t.Errorf("no truncation disclosure may appear when nothing was omitted")
	}
}

// TestBriefStatusCatalogSanitizesAndDiscloses pins the two safety properties:
// user-authored name/description cannot inject markdown structure into the
// trusted brief, and a key that fails the code-token guard drops its entry
// entirely instead of rendering mangled.
func TestBriefStatusCatalogSanitizesAndDiscloses(t *testing.T) {
	t.Parallel()
	ctx := TaskContextForEnv{
		IssueID: "issue-1", AgentID: "a-1", AgentName: "Eve",
		IssueStatuses: []IssueStatusForEnv{
			{Key: "qa", Name: "QA *bold*\n# Heading", Category: "in_review", Description: "line1\nline2 [x]"},
			{Key: "bad key!", Name: "Evil", Category: "todo", Description: "must be dropped"},
		},
		IssueStatusesOmitted: 4,
	}
	out := buildMetaSkillContent("claude", ctx)
	for _, want := range []string{
		`  - started category: ` + "`in_progress`, `in_review`, `blocked`" + ` (built-in), ` + "`qa`" + ` (QA \*bold\* # Heading — line1 line2 \[x\])` + "\n",
		"  - unstarted category: `backlog`, `todo` (built-in)\n",
		"  - …and 4 more custom statuses not listed; an invalid status errors with the full valid list.\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("catalog brief missing %q\n---\n%s", want, out)
		}
	}
	if strings.Contains(out, "Evil") || strings.Contains(out, "bad key") {
		t.Errorf("entry with an invalid key must be dropped entirely\n---\n%s", out)
	}
}

func TestBriefStatusCatalogMixedVersions(t *testing.T) {
	t.Parallel()
	stored := []IssueStatusForEnv{
		{Key: "later", Name: "Later", Category: "unstarted", Description: "Deferred on purpose"},
		{Key: "awaiting_response", Name: "Awaiting response", Category: "started", Description: "Waiting for a reply"},
		{Key: "accepted", Name: "Accepted", Category: "done", Description: "Review complete"},
		{Key: "withdrawn", Name: "Withdrawn", Category: "closed", Description: "No longer needed"},
	}
	wire := append([]IssueStatusForEnv(nil), stored...)
	for i := range wire {
		wire[i].Category = issuestatus.WireCategory(wire[i].Key, wire[i].Category)
	}
	for _, tc := range []struct {
		name      string
		entries   []IssueStatusForEnv
		render    func(*strings.Builder, TaskContextForEnv)
		legacyRaw bool
	}{
		{"stored categories with old daemon reproduce omission", stored, writeLegacyIssueStatusCommand, true},
		{"stored categories with new daemon", stored, writeIssueStatusCommand, false},
		{"wire categories with old daemon", wire, writeLegacyIssueStatusCommand, false},
		{"wire categories with new daemon", wire, writeIssueStatusCommand, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b strings.Builder
			tc.render(&b, TaskContextForEnv{IssueStatuses: tc.entries})
			for _, entry := range stored {
				want := "`" + entry.Key + "` (" + entry.Name + " — " + entry.Description + ")"
				count := 1
				if tc.legacyRaw && entry.Category != "done" {
					count = 0
				}
				if got := strings.Count(b.String(), want); got != count {
					t.Errorf("%q occurs %d times, want %d in:\n%s", want, got, count, b.String())
				}
			}
		})
	}
	var storedBrief, wireBrief strings.Builder
	writeIssueStatusCommand(&storedBrief, TaskContextForEnv{IssueStatuses: stored})
	writeIssueStatusCommand(&wireBrief, TaskContextForEnv{IssueStatuses: wire})
	if storedBrief.String() != wireBrief.String() {
		t.Errorf("wire mapping must preserve the new daemon's grouping and output")
	}
	// Previously emitted wire categories must all remain readable, including
	// the three spellings not produced for customs by today's WireCategory.
	for _, category := range legacyStatusCategoryOrder {
		var b strings.Builder
		writeIssueStatusCommand(&b, TaskContextForEnv{IssueStatuses: []IssueStatusForEnv{
			{Key: "custom", Name: "Custom", Category: category, Description: "Legacy payload"},
		}})
		if strings.Count(b.String(), "`custom` (Custom — Legacy payload)") != 1 {
			t.Errorf("legacy category %s not rendered exactly once: %s", category, b.String())
		}
	}
}

func TestBriefStatusCatalogUnknownCategoriesDisclosed(t *testing.T) {
	t.Parallel()
	for _, onlyUnknown := range []bool{false, true} {
		entries := []IssueStatusForEnv{
			{Key: "future_one", Name: "DO_NOT_RENDER_NAME", Category: "future\n# INJECT", Description: "DO_NOT_RENDER_DESCRIPTION"},
			{Key: "future_two", Name: "Hidden", Category: ""},
			{Key: "bad key!", Name: "INVALID_KEY_NAME", Category: "future"},
		}
		if !onlyUnknown {
			entries = append(entries, IssueStatusForEnv{Key: "qa", Name: "QA *bold*\n# Heading", Category: "started", Description: "line1\nline2 [x]"})
		}
		out := buildMetaSkillContent("claude", TaskContextForEnv{IssueStatuses: entries, IssueStatusesOmitted: 4})
		for _, want := range []string{
			"  - Custom statuses omitted due to unrecognized categories: 2.\n",
			"  - …and 4 more custom statuses not listed; an invalid status errors with the full valid list.\n",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("missing %q in:\n%s", want, out)
			}
		}
		for _, unwanted := range []string{"future_one", "future_two", "INJECT", "DO_NOT_RENDER", "INVALID_KEY_NAME"} {
			if strings.Contains(out, unwanted) {
				t.Errorf("unknown/invalid entry leaked %q", unwanted)
			}
		}
		if !onlyUnknown && !strings.Contains(out, "`qa` (QA \\*bold\\* # Heading — line1 line2 \\[x\\])") {
			t.Errorf("known status must retain sanitization: %s", out)
		}
	}
}
