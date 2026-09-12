package handler

import (
	"fmt"
	"math"
	"math/rand/v2"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// repeatedBatchStage is the pre-optimization staged selection loop. Keep the
// per-candidate scan here as an oracle and benchmark, not a production fallback.
func repeatedBatchStage(children, completed []db.Issue, terminal func(db.Issue) bool) (db.Issue, bool) {
	var rep db.Issue
	found := false
	for _, c := range completed {
		if !c.Stage.Valid || !stageBarrierClosed(children, c, terminal) {
			continue
		}
		if !found || c.Stage.Int32 > rep.Stage.Int32 {
			rep, found = c, true
		}
	}
	return rep, found
}

func TestHighestClosedBatchStage(t *testing.T) {
	for _, tc := range []struct {
		name       string
		children   []db.Issue
		candidates []int
		want       int // Index in children, or -1 for no closed stage.
	}{
		{"empty", nil, nil, -1},
		{"no_candidates", []db.Issue{child(1, "done")}, nil, -1},
		{"same_stage_first_seen", []db.Issue{child(1, "done"), child(1, "cancelled")}, []int{1, 0, 1}, 1},
		{"same_stage_open", []db.Issue{child(1, "done"), child(1, "in_progress")}, []int{0}, -1},
		{"earlier_stage_open", []db.Issue{child(7, "done"), child(2, "backlog")}, []int{0}, -1},
		{"later_stage_open", []db.Issue{child(7, "todo"), child(2, "cancelled")}, []int{1}, 1},
		{"several_stages_close", []db.Issue{child(20, "done"), child(2, "done"), child(7, "cancelled"), child(20, "done")}, []int{1, 3, 2, 0}, 3},
		{"only_lower_stage_in_batch", []db.Issue{child(2, "done"), child(7, "done")}, []int{0}, 0},
		{"gap_before_open_stage", []db.Issue{child(20, "done"), child(2, "done"), child(7, "backlog")}, []int{0, 1}, 1},
		{"ignore_unstaged_sibling", []db.Issue{child(0, "missing"), child(7, "cancelled")}, []int{1}, 1},
		{"unstaged_closes_nothing", []db.Issue{child(7, "done"), child(0, "done")}, []int{1}, -1},
		{"mixed_candidates", []db.Issue{child(0, "done"), child(7, "done"), child(2, "done")}, []int{0, 2, 1}, 1},
		{"max_stage_closed", []db.Issue{child(math.MaxInt32, "done")}, []int{0}, 0},
		{"max_stage_open", []db.Issue{child(math.MaxInt32, "backlog"), child(7, "done")}, []int{1}, 1},
		{"zero_is_not_a_sentinel", []db.Issue{{Stage: pgtype.Int4{Int32: 0, Valid: true}, Status: "done"}, child(1, "todo")}, []int{0}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for i := range tc.children {
				tc.children[i].Number = int32(i + 1)
			}
			var completed []db.Issue
			for _, i := range tc.candidates {
				completed = append(completed, tc.children[i])
			}
			terminal := func(c db.Issue) bool {
				if !c.Stage.Valid {
					t.Fatal("staged selection must not inspect an unresolved unstaged sibling")
				}
				return literalTerminalChild(c)
			}
			got, found := highestClosedBatchStage(tc.children, completed, terminal)
			if found != (tc.want >= 0) || (found && !reflect.DeepEqual(got, tc.children[tc.want])) {
				t.Fatalf("representative number=%d, found=%t; want child index %d", got.Number, found, tc.want)
			}
			old, oldFound := repeatedBatchStage(tc.children, completed, terminal)
			if found != oldFound || (found && !reflect.DeepEqual(got, old)) {
				t.Fatal("selection differs from the repeated-scan algorithm")
			}
		})
	}
}

func TestHighestClosedBatchStageRandomizedEquivalence(t *testing.T) {
	rng := rand.New(rand.NewPCG(23, 8192))
	stages := []int32{0, 1, 2, 7, 20, math.MaxInt32}
	statuses := []string{"backlog", "todo", "in_progress", "done", "cancelled"}
	for iteration := range 2000 {
		children := make([]db.Issue, rng.IntN(40))
		for i := range children {
			children[i] = child(stages[rng.IntN(len(stages))], statuses[rng.IntN(len(statuses))])
			children[i].Number = int32(i + 1)
		}
		if len(children) > 0 {
			children[0].Stage = pgtype.Int4{Int32: 1, Valid: true} // The production caller handles unstaged sets separately.
		}
		var completed []db.Issue
		if len(children) > 0 {
			for range rng.IntN(2*len(children) + 1) {
				c := children[rng.IntN(len(children))]
				c.Status = "done"
				// Candidate rows precede the final sibling read: a concurrent
				// edit may have changed a child's stage or reopened its status.
				if rng.IntN(4) == 0 {
					c.Stage = child(stages[rng.IntN(len(stages))], "done").Stage
				}
				completed = append(completed, c)
			}
		}
		rng.Shuffle(len(children), func(i, j int) { children[i], children[j] = children[j], children[i] })
		beforeChildren, beforeCompleted := slices.Clone(children), slices.Clone(completed)
		got, found := highestClosedBatchStage(children, completed, literalTerminalChild)
		want, wantFound := repeatedBatchStage(children, completed, literalTerminalChild)
		if found != wantFound || (found && !reflect.DeepEqual(got, want)) {
			t.Fatalf("iteration %d: got number=%d stage=%v found=%t; want number=%d stage=%v found=%t", iteration, got.Number, got.Stage, found, want.Number, want.Stage, wantFound)
		}
		if !reflect.DeepEqual(children, beforeChildren) || !reflect.DeepEqual(completed, beforeCompleted) {
			t.Fatalf("iteration %d: selection mutated its input", iteration)
		}
	}
}

func TestHighestClosedBatchStageLinearWork(t *testing.T) {
	for _, n := range []int{100, 1000} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			children := make([]db.Issue, n)
			for i := range children {
				children[i] = child(1, "done")
				children[i].Number = int32(i + 1)
			}
			// Count actual in-memory sibling probes AFTER status resolution,
			// not catalog calls (which #8192 already reduced to one per pass).
			probes := 0
			terminal := func(c db.Issue) bool { probes++; return literalTerminalChild(c) }
			got, found := highestClosedBatchStage(children, children, terminal)
			if !found || got.Number != 1 || probes != n {
				t.Fatalf("number=%d found=%t probes=%d; want first child and %d probes", got.Number, found, probes, n)
			}
			probes = 0
			repeatedBatchStage(children, children, terminal)
			if probes != n*n {
				t.Fatalf("baseline probes=%d, want %d", probes, n*n)
			}
		})
	}
}

func TestHighestClosedBatchStageEarlyExit(t *testing.T) {
	children := make([]db.Issue, 1000)
	for i := range children {
		children[i] = child(1, "done")
	}
	children[0].Status = "in_progress"
	for _, completed := range [][]db.Issue{nil, {child(0, "done")}, children[1:2], children[1:3]} {
		probes := 0
		_, found := highestClosedBatchStage(children, completed, func(c db.Issue) bool {
			probes++
			return literalTerminalChild(c)
		})
		want := 0
		if len(completed) > 0 && completed[0].Stage.Valid {
			want = 1
		}
		if found || probes != want {
			t.Fatalf("candidates=%v: found=%t probes=%d, want no closed stage and %d probes", completed, found, probes, want)
		}
	}
}

func TestBatchChildDonePreservesRepresentativeAndParentOrder(t *testing.T) {
	for _, staged := range []bool{false, true} {
		for _, status := range []string{"done", "cancelled", "approved"} {
			t.Run(fmt.Sprintf("staged=%t/%s", staged, status), func(t *testing.T) {
				ws := dbfx.Workspace(t, "Batch stage selection", "batch-stage-selection", testutil.Cols{"issue_prefix": "BST"})
				fx := testutil.New(testPool, ws, testUserID)
				fx.Member(t, ws, testUserID, "owner")
				fx.Insert(t, "issue_status", testutil.Cols{"workspace_id": ws, "key": "approved", "name": "Approved", "category": "done", "color": "#123456"})
				var parents, agents []string
				var children [][]string
				for p := range 2 {
					agent := fx.Agent(t, fmt.Sprintf("Parent agent %d", p), fx.Runtime(t, fmt.Sprintf("Runtime %d", p)))
					parent := fx.Issue(t, "Parent", testutil.Cols{"status": "in_progress", "assignee_type": "agent", "assignee_id": agent})
					parents, agents = append(parents, parent), append(agents, agent)
					fx.Cleanup(t, "DELETE FROM comment WHERE issue_id = $1", parent)
					fx.Cleanup(t, "DELETE FROM agent_task_queue WHERE issue_id = $1", parent)
					var ids []string
					for _, stage := range []int{2, 7, 7} {
						cols := testutil.Cols{"status": "in_progress", "parent_issue_id": parent}
						if staged {
							cols["stage"] = stage
						}
						ids = append(ids, fx.Issue(t, "Child", cols))
					}
					children = append(children, ids)
					if staged {
						fx.Issue(t, "Next stage", testutil.Cols{"status": "backlog", "parent_issue_id": parent, "stage": 20})
						fx.Issue(t, "Unstaged unknown status", testutil.Cols{"status": "missing", "parent_issue_id": parent})
					}
				}
				h := *testHandler
				h.Bus = events.New()
				var notified []string
				h.Bus.Subscribe(protocol.EventCommentCreated, func(e events.Event) {
					payload, ok := e.Payload.(map[string]any)
					if !ok {
						t.Errorf("unexpected comment payload %T", e.Payload)
						return
					}
					comment, ok := payload["comment"].(CommentResponse)
					if !ok {
						t.Errorf("unexpected comment response %T", payload["comment"])
						return
					}
					notified = append(notified, comment.IssueID)
				})
				// Interleave parents, visit higher stages before lower ones, and
				// choose the second child in the highest stage first.
				ids := []string{children[1][2], children[0][0], children[1][0], children[0][2], children[1][1], children[0][1]}
				request := testutil.WithHeaders(testutil.JSONRequest(http.MethodPatch, "/api/issues/batch", map[string]any{
					"issue_ids": ids, "updates": map[string]any{"status": status},
				}), "X-User-ID", testUserID, "X-Workspace-ID", ws)
				var response struct {
					Updated int `json:"updated"`
				}
				testutil.Call(t, h.BatchUpdateIssues, request).Want(http.StatusOK).JSON(&response)
				if response.Updated != len(ids) {
					t.Fatalf("updated=%d, want %d", response.Updated, len(ids))
				}
				if !slices.Equal(notified, []string{parents[1], parents[0]}) {
					t.Fatalf("notification order=%v, want second parent then first", notified)
				}
				for p, parent := range parents {
					if countSystemCommentsOn(t, parent) != 1 || countPendingTasksForAgent(t, parent, agents[p]) != 1 {
						t.Fatal("each parent must receive exactly one comment and one pending run")
					}
					rep := children[p][2]
					if !staged && p == 0 {
						rep = children[p][0] // Unstaged groups retain their first completed child.
					}
					content, _, _, _ := systemCommentOn(t, parent)
					if !strings.Contains(content, "together in a batch update") || !strings.Contains(content, "](mention://issue/"+rep+")") {
						t.Fatalf("lost batch wording or first representative: %s", content)
					}
					if staged && (!strings.Contains(content, "Stage 7 of this issue is complete") || !strings.Contains(content, "Stage 2: 1/1 done; Stage 7: 2/2 done; Stage 20: 0/1 done (next)") || !strings.Contains(content, "Stage 20 is next")) {
						t.Fatalf("inaccurate final-state summary: %s", content)
					}
					if !staged && !strings.Contains(content, "All sub-issues are complete") {
						t.Fatalf("lost unstaged completion: %s", content)
					}
					if got, want := triggerCommentIDForAgentTask(t, parent, agents[p]), systemCommentIDOn(t, parent); got != want {
						t.Fatalf("run trigger=%s, want final comment %s", got, want)
					}
				}
			})
		}
	}
}

func BenchmarkBatchStageSelection(b *testing.B) {
	for _, n := range []int{1, 10, 100, 1000, 10000} {
		children := make([]db.Issue, n)
		for i := range children {
			children[i] = child(1, "done")
			children[i].ID = parseUUID(fmt.Sprintf("00000000-0000-0000-0000-%012x", i+1))
		}
		completed := slices.Clone(children)
		for _, shape := range []struct {
			name        string
			count       int
			firstStatus string
			want        bool
		}{
			{"same_stage_all_complete", n, "done", true},
			{"single_candidate", 1, "done", true},
			{"first_sibling_reopened", n, "in_progress", false},
			{"two_candidates_first_sibling_reopened", min(2, n), "in_progress", false},
		} {
			children[0].Status = shape.firstStatus
			// Both algorithms use the same pre-resolved snapshot as production.
			// Fixture construction and status resolution are outside the timed loop.
			terminal, err := resolveTerminalChildren(children, func(c db.Issue) (string, error) { return c.Status, nil })
			if err != nil {
				b.Fatal(err)
			}
			for _, tc := range []struct {
				name string
				pick func([]db.Issue, []db.Issue, func(db.Issue) bool) (db.Issue, bool)
			}{{"repeated", repeatedBatchStage}, {"linear", highestClosedBatchStage}} {
				b.Run(fmt.Sprintf("%s/N=%d/%s", shape.name, n, tc.name), func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						rep, found := tc.pick(children, completed[:shape.count], terminal)
						if found != shape.want || (found && rep.ID != children[0].ID) {
							b.Fatal("incorrect barrier or representative")
						}
					}
				})
			}
		}
	}
}
