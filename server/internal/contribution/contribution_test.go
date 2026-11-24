package contribution

import (
	"testing"
	"time"
)

var sg = mustLoc("Asia/Singapore")

func mustLoc(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// at builds a time on a day of November 2026 in Singapore. 2026-11-16 is a
// Monday.
func at(day, hour int) time.Time { return time.Date(2026, 11, day, hour, 0, 0, 0, sg) }

func baseInput() Input {
	return Input{
		Since: at(9, 0),
		Now:   at(18, 12),
		Loc:   sg,
		Issues: []Issue{
			// Created in window by a member, completed twice.
			{ID: "i1", Number: 1, Title: "a", Status: "done", StatusCategory: "done", CreatorType: ActorMember, CreatorID: "m1", CreatedAt: at(10, 9)},
			// Created before the window by an agent, in review.
			{ID: "i2", Number: 2, Title: "b", Status: "in_review", StatusCategory: "started", ProjectID: "p1", CreatorType: ActorAgent, CreatorID: "a1", CreatedAt: at(1, 9)},
			// Blocked, only commented in window.
			{ID: "i3", Number: 3, Title: "c", Status: "blocked", StatusCategory: "started", CreatorType: ActorMember, CreatorID: "m2", CreatedAt: at(1, 9)},
			// No activity in window at all.
			{ID: "i4", Number: 4, Title: "d", Status: "todo", StatusCategory: "unstarted", CreatorType: ActorMember, CreatorID: "m2", CreatedAt: at(2, 9)},
		},
		Comments: []Comment{
			{IssueID: "i1", AuthorType: ActorAgent, AuthorID: "a1", At: at(11, 9)},
			{IssueID: "i1", AuthorType: ActorAgent, AuthorID: "a1", At: at(11, 10)},
			{IssueID: "i3", AuthorType: ActorMember, AuthorID: "m1", At: at(17, 9)},
			{IssueID: "i3", AuthorType: ActorMember, AuthorID: "m1", At: at(8, 9)},       // before window
			{IssueID: "missing", AuthorType: ActorMember, AuthorID: "m1", At: at(12, 9)}, // out of scope
		},
		StatusChanges: []StatusChange{
			{IssueID: "i1", ActorType: ActorMember, ActorID: "m1", At: at(12, 9), ToStatus: "done", ToCategory: "done"},
			{IssueID: "i1", ActorType: ActorMember, ActorID: "m1", At: at(13, 9), ToStatus: "todo", ToCategory: "unstarted"},
			{IssueID: "i1", ActorType: "system", At: at(16, 9), ToStatus: "done", ToCategory: "done"},
			{IssueID: "i2", ActorType: ActorAgent, ActorID: "a1", At: at(16, 10), ToStatus: "in_review", ToCategory: "started"},
		},
	}
}

func TestSummaryKPIs(t *testing.T) {
	s := NewDataset(baseInput()).Summary()
	k := s.KPIs
	// Total is the stock: i1 is done, so i2, i3 and i4 remain. Active leaves
	// out i1, which the window ended on done.
	if k.Total != 3 || k.Touched != 3 || k.Active != 2 || k.Created != 1 || k.CreatedByMember != 1 || k.CreatedByAgent != 0 {
		t.Fatalf("kpis = %+v", k)
	}
	// i1 completed twice (member, then system after a reopen) counts once.
	if k.Completed != 1 {
		t.Fatalf("completed = %d, want 1", k.Completed)
	}
	// Open: i2 in review; i3 blocked is left out.
	if k.Open != 1 || k.OpenInReview != 1 || k.OpenInProgress != 0 {
		t.Fatalf("open = %+v", k)
	}
	if s.SystemCompleted != 1 {
		t.Fatalf("system completed = %d", s.SystemCompleted)
	}
}

func TestSummaryActorsExcludeSystemAndCountDistinctIssues(t *testing.T) {
	s := NewDataset(baseInput()).Summary()
	byKey := map[string]ActorStats{}
	for _, a := range s.Actors {
		if a.ActorType == ActorSystem {
			t.Fatalf("system actor in leaderboard: %+v", a)
		}
		byKey[a.ActorType+":"+a.ActorID] = a
	}
	m1 := byKey["member:m1"]
	if m1.Created != 1 || m1.Commented != 1 || m1.StatusChanged != 1 || m1.Completed != 1 || m1.Participated != 2 {
		t.Fatalf("m1 = %+v", m1)
	}
	a1 := byKey["agent:a1"]
	// Two comments on the same issue are one commented issue.
	if a1.Commented != 1 || a1.StatusChanged != 1 || a1.Participated != 2 || a1.Created != 0 {
		t.Fatalf("a1 = %+v", a1)
	}
	if s.Actors[0].Participated < s.Actors[len(s.Actors)-1].Participated {
		t.Fatalf("actors not ranked by participation: %+v", s.Actors)
	}
}

func TestSummaryBucketsAreZeroFilledMondayWeeks(t *testing.T) {
	s := NewDataset(baseInput()).Summary()
	if len(s.Daily) != 10 || s.Daily[0].Date != "2026-11-09" || s.Daily[9].Date != "2026-11-18" {
		t.Fatalf("daily span = %s..%s (%d)", s.Daily[0].Date, s.Daily[len(s.Daily)-1].Date, len(s.Daily))
	}
	wantWeeks := []string{"2026-11-09", "2026-11-16"}
	if len(s.Weekly) != len(wantWeeks) {
		t.Fatalf("weekly = %+v", s.Weekly)
	}
	for i, w := range wantWeeks {
		if s.Weekly[i].Date != w {
			t.Fatalf("week %d = %s, want %s", i, s.Weekly[i].Date, w)
		}
	}
	first := s.Weekly[0]
	if first.Active != 1 || first.Collaborated != 1 || first.Delivered != 1 {
		t.Fatalf("first week = %+v", first)
	}
	// i1 was closed again in the second week; i2 and i3 stay active.
	second := s.Weekly[1]
	if second.Active != 2 || second.Collaborated != 1 || second.Delivered != 1 {
		t.Fatalf("second week = %+v", second)
	}
}

func TestRestrictedAgentsFoldIntoOneActor(t *testing.T) {
	in := baseInput()
	in.MapActor = func(t, id string) (string, string) {
		if t == ActorAgent {
			return ActorAgent, "__restricted_agents__"
		}
		return t, id
	}
	s := NewDataset(in).Summary()
	for _, a := range s.Actors {
		if a.ActorType == ActorAgent && a.ActorID != "__restricted_agents__" {
			t.Fatalf("agent not folded: %+v", a)
		}
	}
}

func TestEventsNewestFirstFilteredAndPaged(t *testing.T) {
	d := NewDataset(baseInput())
	all, total := d.Events(nil, 100, 0)
	if total != 8 || len(all) != 8 {
		t.Fatalf("total = %d, rows = %d", total, len(all))
	}
	for i := 1; i < len(all); i++ {
		if all[i].At.After(all[i-1].At) {
			t.Fatalf("not newest first at %d", i)
		}
	}
	rows, total := d.Events([]string{"agent:a1"}, 2, 1)
	if total != 3 || len(rows) != 2 || rows[0].Kind != KindCommented {
		t.Fatalf("filtered = %d %+v", total, rows)
	}
	if rows, _ := d.Events(nil, 10, 50); len(rows) != 0 {
		t.Fatalf("offset past end = %+v", rows)
	}
}

func TestStatusGroupFallsBackToBuiltinKeys(t *testing.T) {
	cases := map[string]string{
		"backlog": StatusTodo, "in_progress": StatusInProgress, "blocked": StatusInProgress,
		"in_review": StatusInReview, "done": StatusDone, "cancelled": StatusCancelled, "custom": StatusTodo,
	}
	for status, want := range cases {
		if got := StatusGroup(status, ""); got != want {
			t.Fatalf("%s: %s, want %s", status, got, want)
		}
	}
	if got := StatusGroup("qa", "started"); got != StatusInProgress {
		t.Fatalf("custom started = %s", got)
	}
}

func TestActiveJudgesEachBucketByItsOwnLastStatusChange(t *testing.T) {
	in := baseInput()
	// The first week ends with i1 reopened (todo), so it was active then even
	// though it is done now; the second week ends with it done again.
	s := NewDataset(in).Summary()
	if s.Weekly[0].Active != 1 {
		t.Fatalf("first week active = %d, want 1 (reopened i1)", s.Weekly[0].Active)
	}
	byDay := map[string]Bucket{}
	for _, b := range s.Daily {
		byDay[b.Date] = b
	}
	// On the 12th i1 was completed and nothing reopened it that day.
	if byDay["2026-11-12"].Active != 0 || byDay["2026-11-12"].Delivered != 1 {
		t.Fatalf("12th = %+v", byDay["2026-11-12"])
	}
	// A comment after completion does not make a closed issue active again.
	in.Comments = append(in.Comments, Comment{IssueID: "i1", AuthorType: ActorMember, AuthorID: "m2", At: at(16, 11)})
	s = NewDataset(in).Summary()
	if s.KPIs.Active != 2 {
		t.Fatalf("active = %d, want 2", s.KPIs.Active)
	}
}

func TestIssuesCountInteractionsAndSort(t *testing.T) {
	d := NewDataset(baseInput())
	rows, total := d.Issues(SortInteractions, 10, 0)
	if total != 3 {
		t.Fatalf("total = %d, want 3 issues with activity", total)
	}
	// i1: 2 comments + 3 status changes.
	if rows[0].IssueID != "i1" || rows[0].Interactions != 5 || rows[0].Comments != 2 {
		t.Fatalf("first = %+v", rows[0])
	}
	byParticipants, _ := d.Issues(SortParticipants, 10, 0)
	if byParticipants[0].IssueID != "i1" || byParticipants[0].Participants != 2 {
		t.Fatalf("by participants = %+v", byParticipants[0])
	}
	byCompletion, _ := d.Issues(SortCompletion, 10, 0)
	if byCompletion[0].CompletionSeconds == nil || byCompletion[len(byCompletion)-1].CompletionSeconds != nil {
		t.Fatalf("completed issues must sort first: %+v", byCompletion)
	}
	page, total := d.Issues(SortRecent, 1, 1)
	if total != 3 || len(page) != 1 {
		t.Fatalf("paged = %d %+v", total, page)
	}
}

