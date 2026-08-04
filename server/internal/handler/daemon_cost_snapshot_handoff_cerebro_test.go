package handler

// CEREBRO-PATCH(snapshot-resolved-scope): Handoff shape for the snapshot_prompt renderer (FIR-4500).
// A Handoff with --start-new resolves the old thread root and posts a brand-new
// top-level kickoff comment, whose fresh run is supposed to have no memory of the
// closed thread. These tests pin what the snapshot inlines for that run.

import (
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/cerebro/cost_optimization"
	db "github.com/multica-ai/multica/server/pkg/db/generated"

	"github.com/jackc/pgx/v5/pgtype"
)

var snapshotResolvedAt = pgtype.Timestamptz{Time: time.Unix(1700000000, 0), Valid: true}

// snapshotComments mirrors the order applySnapshotSaving applies the two scopers
// in, so these tests exercise the real pipeline and not just the renderer.
func snapshotComments(issue db.Issue, comments []db.Comment, trigger pgtype.UUID, agentID string) string {
	comments = cost_optimization.ScopeSnapshotToTriggerThread(issue.Kind, comments, trigger)
	comments = cost_optimization.DropResolvedThreads(comments, trigger)
	return renderIssueSnapshot(issue, comments, trigger, agentID)
}

func handoffThread(root pgtype.UUID, agent pgtype.UUID, resolved bool) []db.Comment {
	rootComment := db.Comment{ID: root, AuthorType: "member", Content: "OLD SESSION root question", Type: "comment"}
	if resolved {
		rootComment.ResolvedAt = snapshotResolvedAt
	}
	out := []db.Comment{rootComment}
	for i := 0; i < 10; i++ {
		out = append(out, db.Comment{
			ID:         parseUUID("bbbbbbbb-0000-0000-0000-00000000001" + string(rune('0'+i))),
			AuthorType: "agent",
			AuthorID:   agent,
			Content:    "OLD SESSION reply about something long since settled",
			Type:       "comment",
			ParentID:   root,
		})
	}
	return out
}

// TestRenderIssueSnapshot_HandoffFreshSession reproduces the comment set an issue
// carries right after `multica issue session handoff <issue> <root> --start-new`:
// a CLOSED thread (root resolved) plus a new top-level kickoff comment that is the
// trigger for the fresh run. The fresh run must not be handed the closed thread.
func TestRenderIssueSnapshot_HandoffFreshSession(t *testing.T) {
	t.Parallel()

	oldRoot := parseUUID("bbbbbbbb-0000-0000-0000-000000000001")
	kickoff := parseUUID("cccccccc-0000-0000-0000-000000000001")
	agent := parseUUID("22222222-2222-2222-2222-222222222222")

	comments := append(handoffThread(oldRoot, agent, true), db.Comment{
		ID:         kickoff,
		AuthorType: "agent",
		AuthorID:   agent,
		Content:    "Fresh session kickoff: continue from the handoff brief.",
		Type:       "comment",
	})

	got := snapshotComments(
		db.Issue{Number: 4500, Title: "Token spend", Status: "in_progress", Priority: "high"},
		comments, kickoff, uuidToString(agent),
	)

	if strings.Contains(got, "OLD SESSION") {
		t.Fatalf("fresh Handoff session must not receive the closed thread, got:\n%s", got)
	}
	if !strings.Contains(got, "Issue #4500: Token spend") {
		t.Fatalf("issue core must still be inlined, got:\n%s", got)
	}
}

// TestRenderIssueSnapshot_TriggerInsideResolvedThread guards the other direction:
// an agent woken by a reply inside an already-resolved thread still needs that
// thread's context, so it must not be dropped.
func TestRenderIssueSnapshot_TriggerInsideResolvedThread(t *testing.T) {
	t.Parallel()

	oldRoot := parseUUID("bbbbbbbb-0000-0000-0000-000000000001")
	agent := parseUUID("22222222-2222-2222-2222-222222222222")
	trigger := parseUUID("bbbbbbbb-0000-0000-0000-000000000015") // a reply in that thread

	got := snapshotComments(
		db.Issue{Number: 4500, Title: "Token spend", Status: "in_progress", Priority: "high"},
		handoffThread(oldRoot, agent, true), trigger, uuidToString(agent),
	)

	if !strings.Contains(got, "OLD SESSION root question") {
		t.Fatalf("trigger's own thread must survive even when resolved, got:\n%s", got)
	}
}

// TestRenderIssueSnapshot_OpenThreadSurvives keeps the normal case honest: an
// unresolved thread is still inlined in full.
func TestRenderIssueSnapshot_OpenThreadSurvives(t *testing.T) {
	t.Parallel()

	openRoot := parseUUID("bbbbbbbb-0000-0000-0000-000000000001")
	kickoff := parseUUID("cccccccc-0000-0000-0000-000000000001")
	agent := parseUUID("22222222-2222-2222-2222-222222222222")

	comments := append(handoffThread(openRoot, agent, false), db.Comment{
		ID:         kickoff,
		AuthorType: "agent",
		AuthorID:   agent,
		Content:    "New top-level comment.",
		Type:       "comment",
	})

	got := snapshotComments(
		db.Issue{Number: 4500, Title: "Token spend", Status: "in_progress", Priority: "high"},
		comments, kickoff, uuidToString(agent),
	)

	if !strings.Contains(got, "OLD SESSION root question") {
		t.Fatalf("open thread must still be inlined, got:\n%s", got)
	}
}
