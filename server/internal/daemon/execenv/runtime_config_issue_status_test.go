package execenv

import (
	"strings"
	"testing"
)

// legacyStatusLine is the pre-MUL-6460 status bullet. Workspaces without
// custom statuses — including every deployment behind an old server — must
// keep rendering it byte-identical: it is part of the prompt-cache prefix and
// the no-custom-statuses path is the compatibility contract of MUL-6460.
const legacyStatusLine = "- `multica issue status <id> <status> [--no-start]` — flip status (todo / in_progress / in_review / done / blocked / backlog / cancelled).\n"

// catalogBridgeBullet is the workflow-section bridge from category rules to a
// concrete key choice; it must appear exactly when a catalog is present.
const catalogBridgeBullet = "- The workflow rules above name fixed built-in statuses, not categories. Custom statuses inherit only lifecycle semantics: done is successful completion; closed is cancellation. A custom started status does not replace in_review or blocked. Use the built-in key when its special workflow behavior is required.\n"

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
		"- `multica issue status <id> <status> [--no-start]` — flip status. Categories describe lifecycle only; custom statuses do not inherit built-in parking, review, failure, or recovery behavior. Built-in status keys are fixed:\n",
		"  - `unstarted`: `backlog`, `todo` (built-in), `later` (Later — Deferred on purpose), `rework` (Rework)\n",
		"  - `done`: `done` (built-in)\n",
		"  - `started`: `in_progress`, `in_review`, `blocked` (built-in), `human_review` (Human Review — Awaiting human acceptance)\n",
		"  - `closed`: `cancelled` (built-in)\n",
		catalogBridgeBullet,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("catalog brief missing %q\n---\n%s", want, out)
		}
	}
	if strings.Contains(out, "more custom statuses not listed") {
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
		`  - ` + "`started`: `in_progress`, `in_review`, `blocked`" + ` (built-in), ` + "`qa`" + ` (QA \*bold\* # Heading — line1 line2 \[x\])` + "\n",
		"  - `unstarted`: `backlog`, `todo` (built-in)\n",
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
