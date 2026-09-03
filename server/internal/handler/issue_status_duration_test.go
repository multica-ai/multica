package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// base is an arbitrary fixed instant; every case builds offsets from it so the
// expected numbers are readable as "N units after creation".
var base = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func at(d time.Duration) time.Time { return base.Add(d) }

// got maps entries to a compact status->seconds view for assertions that do not
// care about order.
func got(entries []StatusDurationEntry) map[string]int64 {
	out := map[string]int64{}
	for _, e := range entries {
		out[e.Status] = e.Seconds
	}
	return out
}

func order(entries []StatusDurationEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Status
	}
	return out
}

// TestAggregate_LinearScreenshotShape reproduces the canonical case: an issue
// created in backlog, moved through an implementation status, now sitting in
// review. It pins all three attribution rules at once — segment 0 comes from
// the first change's `from`, interior segments from each change's `to`, and the
// open tail from the issue's current status.
func TestAggregate_LinearScreenshotShape(t *testing.T) {
	changes := []statusChange{
		{From: "backlog", To: "implement", At: at(9 * time.Second)},
		{From: "implement", To: "code_review", At: at(9*time.Second + 56*time.Minute)},
	}
	now := at(9*time.Second + 56*time.Minute + 11*24*time.Hour)

	entries, partial := aggregateStatusDurations(base, "code_review", changes, now)
	if partial {
		t.Fatal("partial = true, want false when history exists")
	}

	want := map[string]int64{
		"backlog":     9,
		"implement":   56 * 60,
		"code_review": 11 * 24 * 3600,
	}
	for status, seconds := range want {
		if g := got(entries)[status]; g != seconds {
			t.Errorf("%s = %ds, want %ds", status, g, seconds)
		}
	}

	// Ordering is by first entry, so the list reads chronologically.
	wantOrder := []string{"backlog", "implement", "code_review"}
	for i, status := range order(entries) {
		if status != wantOrder[i] {
			t.Fatalf("order = %v, want %v", order(entries), wantOrder)
		}
	}

	for _, e := range entries {
		if (e.Status == "code_review") != e.Current {
			t.Errorf("%s current = %v, want %v", e.Status, e.Current, e.Status == "code_review")
		}
	}
}

// TestAggregate_RepeatVisitsAccumulate is the reason the result is a map keyed
// by status rather than a list of segments: bouncing between review and
// implement must sum into one row per status, not four rows.
func TestAggregate_RepeatVisitsAccumulate(t *testing.T) {
	changes := []statusChange{
		{From: "todo", To: "review", At: at(1 * time.Hour)},  // todo: 1h
		{From: "review", To: "todo", At: at(3 * time.Hour)},  // review: 2h
		{From: "todo", To: "review", At: at(4 * time.Hour)},  // todo: +1h
		{From: "review", To: "todo", At: at(10 * time.Hour)}, // review: +6h
	}
	now := at(12 * time.Hour) // todo: +2h

	entries, _ := aggregateStatusDurations(base, "todo", changes, now)

	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2 (one row per distinct status)", len(entries))
	}
	if g := got(entries)["todo"]; g != int64(4*time.Hour/time.Second) {
		t.Errorf("todo = %ds, want %ds", g, int64(4*time.Hour/time.Second))
	}
	if g := got(entries)["review"]; g != int64(8*time.Hour/time.Second) {
		t.Errorf("review = %ds, want %ds", g, int64(8*time.Hour/time.Second))
	}
}

// TestAggregate_NoHistory covers issues that predate status-change logging and
// issues that have simply never moved. Both must report the whole lifetime on
// the current status, flagged partial so the UI can hedge its wording.
func TestAggregate_NoHistory(t *testing.T) {
	now := at(5 * time.Hour)

	entries, partial := aggregateStatusDurations(base, "backlog", nil, now)

	if !partial {
		t.Error("partial = false, want true when there is no recorded history")
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	if entries[0].Status != "backlog" || entries[0].Seconds != int64(5*time.Hour/time.Second) {
		t.Errorf("entry = %+v, want backlog for the full lifetime", entries[0])
	}
	if !entries[0].Current {
		t.Error("the only entry must be marked current")
	}
}

// TestAggregate_TailUsesIssueRowNotLastActivity pins the deliberate disagreement
// rule. The activity listener is a best-effort async subscriber, so the issue
// row can hold a status that never produced an activity row. The status the
// user is looking at must be the one the tail is attributed to.
func TestAggregate_TailUsesIssueRowNotLastActivity(t *testing.T) {
	changes := []statusChange{
		{From: "todo", To: "review", At: at(1 * time.Hour)},
	}
	now := at(3 * time.Hour)

	// Issue row says "done" even though no activity recorded the review->done move.
	entries, _ := aggregateStatusDurations(base, "done", changes, now)

	if g := got(entries)["done"]; g != int64(2*time.Hour/time.Second) {
		t.Errorf("done = %ds, want the full open tail (%ds)", g, int64(2*time.Hour/time.Second))
	}
	if _, ok := got(entries)["review"]; !ok {
		t.Error("review must still appear with a zero-length segment, not vanish")
	}
	for _, e := range entries {
		if e.Status == "review" && e.Current {
			t.Error("review must not be marked current when the issue row says done")
		}
	}
}

// TestAggregate_BackdatedRowsNeverGoNegative guards the clamp. A clock skew
// between writers can order a row before its predecessor; that must cost one
// zero-length segment, never a negative contribution that eats real time from
// another status.
func TestAggregate_BackdatedRowsNeverGoNegative(t *testing.T) {
	changes := []statusChange{
		{From: "todo", To: "review", At: at(2 * time.Hour)},
		// Backdated: lands before the previous change.
		{From: "review", To: "done", At: at(1 * time.Hour)},
	}
	now := at(4 * time.Hour)

	entries, _ := aggregateStatusDurations(base, "done", changes, now)

	for _, e := range entries {
		if e.Seconds < 0 {
			t.Errorf("%s = %ds, want no negative durations", e.Status, e.Seconds)
		}
	}
	if g := got(entries)["todo"]; g != int64(2*time.Hour/time.Second) {
		t.Errorf("todo = %ds, want 2h preserved despite the backdated successor", g)
	}
	if g := got(entries)["done"]; g != int64(2*time.Hour/time.Second) {
		t.Errorf("done = %ds, want 2h", g)
	}
}

// TestAggregate_EmptyStatusSegmentsAreDropped covers activity rows whose details
// lack an endpoint. There is no label to render for "", so such a segment must
// not create a bucket.
func TestAggregate_EmptyStatusSegmentsAreDropped(t *testing.T) {
	changes := []statusChange{
		{From: "", To: "review", At: at(1 * time.Hour)},
	}
	now := at(2 * time.Hour)

	entries, _ := aggregateStatusDurations(base, "review", changes, now)

	for _, e := range entries {
		if e.Status == "" {
			t.Fatal("an empty status key must never become a row")
		}
	}
	if len(entries) != 1 || entries[0].Status != "review" {
		t.Fatalf("entries = %+v, want only the review row", entries)
	}
}

// TestAggregate_SubSecondSegmentsRoundToZeroButStillListed keeps a status the
// issue genuinely passed through visible even when it held it only briefly.
// Dropping it would hide a real transition; the row reads as "0s".
func TestAggregate_SubSecondSegmentsRoundToZeroButStillListed(t *testing.T) {
	changes := []statusChange{
		{From: "todo", To: "triage", At: at(500 * time.Millisecond)},
		{From: "triage", To: "done", At: at(900 * time.Millisecond)},
	}
	now := at(time.Hour)

	entries, _ := aggregateStatusDurations(base, "done", changes, now)

	if len(entries) != 3 {
		t.Fatalf("len(entries) = %d, want 3 including the sub-second visit", len(entries))
	}
	if g := got(entries)["triage"]; g != 0 {
		t.Errorf("triage = %ds, want 0 (rounded down but present)", g)
	}
}

// ── endpoint tests ────────────────────────────────────────────────────────────
//
// The pure tests above cover the arithmetic. These cover the wiring the pure
// tests cannot see: the SQL projection (jsonb -> from/to), row ordering, the
// created_at anchor coming off the issue row, and workspace scoping.

// fetchStatusDurations calls the endpoint and returns the decoded body.
func fetchStatusDurations(t *testing.T, issueID string) (StatusDurationsResponse, int) {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/"+issueID+"/status-durations", nil)
	req = withURLParam(req, "id", issueID)
	testHandler.GetIssueStatusDurations(w, req)
	var resp StatusDurationsResponse
	if w.Code == http.StatusOK {
		json.NewDecoder(w.Body).Decode(&resp)
	}
	return resp, w.Code
}

// seedStatusChange inserts one status_changed activity at an explicit instant.
func seedStatusChange(t *testing.T, issueID, from, to string, at time.Time) {
	t.Helper()
	_, err := testPool.Exec(context.Background(), `
		INSERT INTO activity_log (workspace_id, issue_id, actor_type, actor_id, action, details, created_at)
		VALUES ($1, $2, 'member', $3, 'status_changed',
		        jsonb_build_object('from', $4::text, 'to', $5::text), $6)
	`, testWorkspaceID, issueID, testUserID, from, to, at)
	if err != nil {
		t.Fatalf("seed status change %s->%s: %v", from, to, err)
	}
}

// TestGetIssueStatusDurations_AggregatesSeededHistory drives the whole path:
// real jsonb rows in, aggregated durations out.
func TestGetIssueStatusDurations_AggregatesSeededHistory(t *testing.T) {
	issueID := createIssueForTimeline(t, "Status durations aggregate")

	// Anchor creation far enough back that every segment is comfortably
	// measurable in whole seconds.
	created := time.Now().UTC().Add(-10 * time.Hour)
	if _, err := testPool.Exec(context.Background(),
		`UPDATE issue SET created_at = $1, status = 'in_review' WHERE id = $2`, created, issueID); err != nil {
		t.Fatalf("backdate issue: %v", err)
	}

	seedStatusChange(t, issueID, "todo", "in_progress", created.Add(2*time.Hour))
	seedStatusChange(t, issueID, "in_progress", "in_review", created.Add(5*time.Hour))

	resp, status := fetchStatusDurations(t, issueID)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if resp.Partial {
		t.Error("partial = true, want false when history exists")
	}
	if len(resp.Entries) != 3 {
		t.Fatalf("entries = %+v, want 3 rows", resp.Entries)
	}

	// Ordered by first entry, so the list reads chronologically.
	wantOrder := []string{"todo", "in_progress", "in_review"}
	for i, e := range resp.Entries {
		if e.Status != wantOrder[i] {
			t.Fatalf("order = %v, want %v", order(resp.Entries), wantOrder)
		}
	}

	// Closed segments are exact. The open one is compared with slack because
	// it runs to time.Now() inside the handler.
	if s := got(resp.Entries)["todo"]; s != int64(2*time.Hour/time.Second) {
		t.Errorf("todo = %ds, want 7200", s)
	}
	if s := got(resp.Entries)["in_progress"]; s != int64(3*time.Hour/time.Second) {
		t.Errorf("in_progress = %ds, want 10800", s)
	}
	if s := got(resp.Entries)["in_review"]; s < int64(5*time.Hour/time.Second)-30 {
		t.Errorf("in_review = %ds, want ~18000 (the open segment)", s)
	}

	if resp.Entries[2].Status != "in_review" || !resp.Entries[2].Current {
		t.Error("the issue row's status must be the one flagged current")
	}
	if resp.ComputedAt == "" {
		t.Error("computed_at must be set so clients can tick the open segment")
	}
}

// TestGetIssueStatusDurations_NoHistoryIsPartial covers issues predating
// status-change logging: one synthetic bucket for the whole lifetime.
func TestGetIssueStatusDurations_NoHistoryIsPartial(t *testing.T) {
	issueID := createIssueForTimeline(t, "Status durations no history")
	created := time.Now().UTC().Add(-3 * time.Hour)
	if _, err := testPool.Exec(context.Background(),
		`UPDATE issue SET created_at = $1 WHERE id = $2`, created, issueID); err != nil {
		t.Fatalf("backdate issue: %v", err)
	}

	resp, status := fetchStatusDurations(t, issueID)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if !resp.Partial {
		t.Error("partial = false, want true when nothing was ever logged")
	}
	if len(resp.Entries) != 1 || resp.Entries[0].Status != "todo" {
		t.Fatalf("entries = %+v, want a single todo row", resp.Entries)
	}
	if s := resp.Entries[0].Seconds; s < int64(3*time.Hour/time.Second)-30 {
		t.Errorf("seconds = %d, want ~10800 (the full lifetime)", s)
	}
}

// TestGetIssueStatusDurations_IgnoresNonStatusActivity proves the SQL filter
// bites. Issue timelines are dominated by other action kinds; if the filter
// were dropped, their rows would be read as transitions with empty endpoints.
func TestGetIssueStatusDurations_IgnoresNonStatusActivity(t *testing.T) {
	issueID := createIssueForTimeline(t, "Status durations filter")
	created := time.Now().UTC().Add(-4 * time.Hour)
	if _, err := testPool.Exec(context.Background(),
		`UPDATE issue SET created_at = $1 WHERE id = $2`, created, issueID); err != nil {
		t.Fatalf("backdate issue: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO activity_log (workspace_id, issue_id, actor_type, actor_id, action, details, created_at)
		VALUES ($1, $2, 'member', $3, 'priority_changed', '{"from":"low","to":"high"}'::jsonb, $4)
	`, testWorkspaceID, issueID, testUserID, created.Add(time.Hour)); err != nil {
		t.Fatalf("seed priority activity: %v", err)
	}

	resp, status := fetchStatusDurations(t, issueID)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if !resp.Partial || len(resp.Entries) != 1 {
		t.Fatalf("entries = %+v (partial=%v), want the priority change to be invisible here",
			resp.Entries, resp.Partial)
	}
}

// TestGetIssueStatusDurations_CrossWorkspaceReturns404 keeps the endpoint from
// becoming a read hole into other workspaces' issue history.
func TestGetIssueStatusDurations_CrossWorkspaceReturns404(t *testing.T) {
	_, status := fetchStatusDurations(t, "00000000-0000-0000-0000-000000000000")
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for an issue this user cannot see", status)
	}
}
