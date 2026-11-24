// Package contribution aggregates issue activity into the team contribution
// dashboard. The handler loads raw issues and dated events; every counting
// rule — what is active, what counts as delivery — lives here so the page's
// numbers share one definition.
package contribution

import (
	"sort"
	"time"
)

// Event kinds.
const (
	KindCreated       = "created"
	KindCommented     = "commented"
	KindStatusChanged = "status_changed"
)

// Actor types. System covers automation with no member or agent behind it;
// it never appears in the leaderboards and its completions are counted apart.
const (
	ActorMember = "member"
	ActorAgent  = "agent"
	ActorSystem = "system"
)

// Status groups shown on the dashboard. Custom statuses fold into the group of
// their lifecycle category.
const (
	StatusTodo       = "todo"
	StatusInProgress = "in_progress"
	StatusInReview   = "in_review"
	StatusDone       = "done"
	StatusCancelled  = "cancelled"
)

var builtinCategory = map[string]string{
	"backlog":     "unstarted",
	"todo":        "unstarted",
	"in_progress": "started",
	"in_review":   "started",
	"blocked":     "started",
	"done":        "done",
	"cancelled":   "closed",
}

func lifecycleCategory(status, category string) string {
	if category != "" {
		return category
	}
	return builtinCategory[status]
}

// StatusGroup maps a status key and its lifecycle category onto a dashboard
// status group.
func StatusGroup(status, category string) string {
	switch lifecycleCategory(status, category) {
	case "started":
		if status == "in_review" {
			return StatusInReview
		}
		return StatusInProgress
	case "done":
		return StatusDone
	case "closed":
		return StatusCancelled
	default:
		return StatusTodo
	}
}

// isOpen is the "unfinished" KPI: in progress or in review. Blocked work is
// started but stalled, so it is left out.
func isOpen(status, category string) bool {
	switch StatusGroup(status, category) {
	case StatusInReview:
		return true
	case StatusInProgress:
		return status != "blocked"
	}
	return false
}

func isClosedCategory(category string) bool { return category == "done" || category == "closed" }

// isStock is an issue that is still open now: neither done nor cancelled.
func isStock(status, category string) bool {
	return !isClosedCategory(lifecycleCategory(status, category))
}

// Issue is one issue in scope.
type Issue struct {
	ID             string
	Number         int32
	Title          string
	Status         string
	StatusCategory string
	ProjectID      string
	CreatorType    string
	CreatorID      string
	CreatedAt      time.Time
}

// Comment is one plain comment or reply.
type Comment struct {
	IssueID    string
	AuthorType string
	AuthorID   string
	At         time.Time
}

// StatusChange is one status_changed activity row.
type StatusChange struct {
	IssueID    string
	ActorType  string
	ActorID    string
	At         time.Time
	ToStatus   string
	ToCategory string
}

// Event is one dated contribution event.
type Event struct {
	At        time.Time
	Kind      string
	ActorType string
	ActorID   string
	IssueID   string
	ToStatus  string
	// Completes is set on a status change into a done-category status.
	Completes bool
	// Closes is set on a status change into a done- or closed-category
	// status: the issue left active work.
	Closes bool
}

func (e Event) actorKey() string { return e.ActorType + ":" + e.ActorID }

// Input is everything one request loaded.
type Input struct {
	Issues        []Issue
	Comments      []Comment
	StatusChanges []StatusChange
	Since         time.Time
	Now           time.Time
	Loc           *time.Location
	// MapActor may rewrite an actor (for example to fold restricted agents
	// into one bucket). Nil keeps actors as they are.
	MapActor func(actorType, actorID string) (string, string)
}

// Dataset is the scoped, time-ordered event set the aggregations read.
type Dataset struct {
	issues map[string]*Issue
	order  []*Issue
	events []Event
	since  time.Time
	now    time.Time
	loc    *time.Location
}

func normalizeActor(actorType, actorID string) (string, string) {
	if actorID == "" || (actorType != ActorMember && actorType != ActorAgent) {
		return ActorSystem, ""
	}
	return actorType, actorID
}

// NewDataset scopes the input and builds the event list.
func NewDataset(in Input) *Dataset {
	loc := in.Loc
	if loc == nil {
		loc = time.UTC
	}
	d := &Dataset{issues: map[string]*Issue{}, since: in.Since, now: in.Now, loc: loc}
	for idx := range in.Issues {
		iss := &in.Issues[idx]
		d.issues[iss.ID] = iss
		d.order = append(d.order, iss)
	}
	mapActor := func(t, id string) (string, string) {
		t, id = normalizeActor(t, id)
		if in.MapActor != nil && t != ActorSystem {
			t, id = in.MapActor(t, id)
		}
		return t, id
	}
	inWindow := func(at time.Time) bool { return !at.Before(in.Since) }

	for _, iss := range d.order {
		if !inWindow(iss.CreatedAt) {
			continue
		}
		t, id := mapActor(iss.CreatorType, iss.CreatorID)
		d.events = append(d.events, Event{At: iss.CreatedAt, Kind: KindCreated, ActorType: t, ActorID: id, IssueID: iss.ID})
	}
	for _, c := range in.Comments {
		if _, ok := d.issues[c.IssueID]; !ok || !inWindow(c.At) {
			continue
		}
		t, id := mapActor(c.AuthorType, c.AuthorID)
		d.events = append(d.events, Event{At: c.At, Kind: KindCommented, ActorType: t, ActorID: id, IssueID: c.IssueID})
	}
	for _, s := range in.StatusChanges {
		if _, ok := d.issues[s.IssueID]; !ok || !inWindow(s.At) {
			continue
		}
		t, id := mapActor(s.ActorType, s.ActorID)
		d.events = append(d.events, Event{
			At: s.At, Kind: KindStatusChanged, ActorType: t, ActorID: id, IssueID: s.IssueID,
			ToStatus: s.ToStatus, Completes: lifecycleCategory(s.ToStatus, s.ToCategory) == "done",
			Closes: isClosedCategory(lifecycleCategory(s.ToStatus, s.ToCategory)),
		})
	}
	sort.SliceStable(d.events, func(a, b int) bool { return d.events[a].At.Before(d.events[b].At) })
	return d
}

func (d *Dataset) day(t time.Time) string { return t.In(d.loc).Format("2006-01-02") }

func (d *Dataset) week(t time.Time) string {
	local := t.In(d.loc)
	offset := (int(local.Weekday()) + 6) % 7 // Monday = 0
	monday := time.Date(local.Year(), local.Month(), local.Day()-offset, 0, 0, 0, 0, d.loc)
	return monday.Format("2006-01-02")
}

func (d *Dataset) dayKeys() []string {
	var out []string
	start := d.since.In(d.loc)
	cur := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, d.loc)
	end := d.day(d.now)
	for {
		k := cur.Format("2006-01-02")
		out = append(out, k)
		if k >= end {
			return out
		}
		cur = cur.AddDate(0, 0, 1)
	}
}

func (d *Dataset) weekKeys() []string {
	var out []string
	first, err := time.ParseInLocation("2006-01-02", d.week(d.since), d.loc)
	if err != nil {
		return nil
	}
	end := d.week(d.now)
	for cur := first; ; cur = cur.AddDate(0, 0, 7) {
		k := cur.Format("2006-01-02")
		out = append(out, k)
		if k >= end {
			return out
		}
	}
}

type set map[string]struct{}

func (s set) add(k string) { s[k] = struct{}{} }

func bucketSet(m map[string]set, k string) set {
	s, ok := m[k]
	if !ok {
		s = set{}
		m[k] = s
	}
	return s
}

// KPIs are the headline counts.
type KPIs struct {
	// Total is the stock: issues in scope that are neither done nor cancelled
	// now, whatever the window.
	Total int `json:"total"`
	// Active is issues with activity in the window that were not left done or
	// cancelled by it; Touched counts them all.
	Active          int `json:"active"`
	Touched         int `json:"touched"`
	Created         int `json:"created"`
	CreatedByMember int `json:"created_by_member"`
	CreatedByAgent  int `json:"created_by_agent"`
	Completed       int `json:"completed"`
	Open            int `json:"open"`
	OpenInProgress  int `json:"open_in_progress"`
	OpenInReview    int `json:"open_in_review"`
}

// Bucket is one day or week of team activity, in distinct issues.
type Bucket struct {
	Date         string `json:"date"`
	Active       int    `json:"active"`
	Collaborated int    `json:"collaborated"`
	Delivered    int    `json:"delivered"`
}

// ActorStats is one member's or agent's totals, in distinct issues.
type ActorStats struct {
	ActorType     string `json:"actor_type"`
	ActorID       string `json:"actor_id"`
	Created       int    `json:"created"`
	Commented     int    `json:"commented"`
	StatusChanged int    `json:"status_changed"`
	Completed     int    `json:"completed"`
	Participated  int    `json:"participated"`
}

// ActorBucket is one actor's week, in distinct issues.
type ActorBucket struct {
	Week         string `json:"week"`
	ActorType    string `json:"actor_type"`
	ActorID      string `json:"actor_id"`
	Participated int    `json:"participated"`
	Collaborated int    `json:"collaborated"`
	Delivered    int    `json:"delivered"`
}

// Summary is the whole contribution page minus the paged lists.
type Summary struct {
	KPIs            KPIs          `json:"kpis"`
	Daily           []Bucket      `json:"daily"`
	Weekly          []Bucket      `json:"weekly"`
	Actors          []ActorStats  `json:"actors"`
	ActorWeekly     []ActorBucket `json:"actor_weekly"`
	SystemCompleted int           `json:"system_completed"`
}

// activeTracker counts, per bucket, the issues with activity in it minus those
// whose last status change in it closed them. Judging each bucket by its own
// events keeps past buckets stable when an issue is closed later.
type activeTracker struct {
	touched map[string]set
	closed  map[string]map[string]bool
}

func newActiveTracker() *activeTracker {
	return &activeTracker{touched: map[string]set{}, closed: map[string]map[string]bool{}}
}

func (t *activeTracker) add(key string, e Event) {
	bucketSet(t.touched, key).add(e.IssueID)
	if e.Kind != KindStatusChanged {
		return
	}
	m, ok := t.closed[key]
	if !ok {
		m = map[string]bool{}
		t.closed[key] = m
	}
	m[e.IssueID] = e.Closes
}

func (t *activeTracker) count(key string) int {
	n := len(t.touched[key])
	for _, closed := range t.closed[key] {
		if closed {
			n--
		}
	}
	return n
}

type actorAcc struct {
	actorType, actorID                                 string
	created, commented, statusChanged, completed, part set
}

// Summary aggregates the dataset. Every count is of distinct issues within its
// bucket, so a bucket never double-counts one issue.
func (d *Dataset) Summary() Summary {
	var s Summary
	for _, iss := range d.order {
		if isStock(iss.Status, iss.StatusCategory) {
			s.KPIs.Total++
		}
	}

	const window = ""
	active := newActiveTracker()
	touched := set{}
	completed := set{}
	dayActive, weekActive := newActiveTracker(), newActiveTracker()
	dayCollab, dayDeliver := map[string]set{}, map[string]set{}
	weekCollab, weekDeliver := map[string]set{}, map[string]set{}
	actors := map[string]*actorAcc{}
	var actorOrder []string
	actorWeek := map[string]map[string][3]set{}
	systemCompleted := set{}

	for _, e := range d.events {
		dk, wk := d.day(e.At), d.week(e.At)
		touched.add(e.IssueID)
		active.add(window, e)
		dayActive.add(dk, e)
		weekActive.add(wk, e)
		switch e.Kind {
		case KindCreated:
			s.KPIs.Created++
			switch e.ActorType {
			case ActorMember:
				s.KPIs.CreatedByMember++
			case ActorAgent:
				s.KPIs.CreatedByAgent++
			}
		case KindCommented:
			bucketSet(dayCollab, dk).add(e.IssueID)
			bucketSet(weekCollab, wk).add(e.IssueID)
		}
		if e.Completes {
			completed.add(e.IssueID)
			bucketSet(dayDeliver, dk).add(e.IssueID)
			bucketSet(weekDeliver, wk).add(e.IssueID)
			if e.ActorType == ActorSystem {
				systemCompleted.add(e.IssueID)
			}
		}
		if e.ActorType == ActorSystem {
			continue
		}

		key := e.actorKey()
		acc, ok := actors[key]
		if !ok {
			acc = &actorAcc{actorType: e.ActorType, actorID: e.ActorID, created: set{}, commented: set{}, statusChanged: set{}, completed: set{}, part: set{}}
			actors[key] = acc
			actorOrder = append(actorOrder, key)
		}
		acc.part.add(e.IssueID)
		switch e.Kind {
		case KindCreated:
			acc.created.add(e.IssueID)
		case KindCommented:
			acc.commented.add(e.IssueID)
		case KindStatusChanged:
			acc.statusChanged.add(e.IssueID)
		}
		if e.Completes {
			acc.completed.add(e.IssueID)
		}
		weeks, ok := actorWeek[key]
		if !ok {
			weeks = map[string][3]set{}
			actorWeek[key] = weeks
		}
		w, ok := weeks[wk]
		if !ok {
			w = [3]set{{}, {}, {}}
			weeks[wk] = w
		}
		w[0].add(e.IssueID)
		if e.Kind == KindCommented {
			w[1].add(e.IssueID)
		}
		if e.Completes {
			w[2].add(e.IssueID)
		}
	}

	s.KPIs.Active = active.count(window)
	s.KPIs.Touched = len(touched)
	s.KPIs.Completed = len(completed)
	s.SystemCompleted = len(systemCompleted)

	for id := range touched {
		iss := d.issues[id]
		if isOpen(iss.Status, iss.StatusCategory) {
			s.KPIs.Open++
			if StatusGroup(iss.Status, iss.StatusCategory) == StatusInReview {
				s.KPIs.OpenInReview++
			} else {
				s.KPIs.OpenInProgress++
			}
		}
	}

	for _, k := range d.dayKeys() {
		s.Daily = append(s.Daily, Bucket{Date: k, Active: dayActive.count(k), Collaborated: len(dayCollab[k]), Delivered: len(dayDeliver[k])})
	}
	weeks := d.weekKeys()
	for _, k := range weeks {
		s.Weekly = append(s.Weekly, Bucket{Date: k, Active: weekActive.count(k), Collaborated: len(weekCollab[k]), Delivered: len(weekDeliver[k])})
	}

	s.Actors = make([]ActorStats, 0, len(actorOrder))
	for _, key := range actorOrder {
		acc := actors[key]
		s.Actors = append(s.Actors, ActorStats{
			ActorType: acc.actorType, ActorID: acc.actorID,
			Created: len(acc.created), Commented: len(acc.commented), StatusChanged: len(acc.statusChanged),
			Completed: len(acc.completed), Participated: len(acc.part),
		})
		for _, wk := range weeks {
			w, ok := actorWeek[key][wk]
			if !ok {
				continue
			}
			s.ActorWeekly = append(s.ActorWeekly, ActorBucket{
				Week: wk, ActorType: acc.actorType, ActorID: acc.actorID,
				Participated: len(w[0]), Collaborated: len(w[1]), Delivered: len(w[2]),
			})
		}
	}
	sort.SliceStable(s.Actors, func(a, b int) bool {
		if s.Actors[a].Participated != s.Actors[b].Participated {
			return s.Actors[a].Participated > s.Actors[b].Participated
		}
		return s.Actors[a].ActorID < s.Actors[b].ActorID
	})
	return s
}

// EventRow is one row of the activity detail list.
type EventRow struct {
	At        time.Time `json:"at"`
	Kind      string    `json:"kind"`
	ActorType string    `json:"actor_type"`
	ActorID   string    `json:"actor_id"`
	IssueID   string    `json:"issue_id"`
	Number    int32     `json:"number"`
	Title     string    `json:"title"`
	ProjectID string    `json:"project_id"`
	Status    string    `json:"status"`
	StatusGrp string    `json:"status_group"`
	ToStatus  string    `json:"to_status"`
}

// Events returns the window's events newest first, optionally narrowed to a
// set of "type:id" actor keys, paged by limit/offset, plus the total.
func (d *Dataset) Events(actors []string, limit, offset int) ([]EventRow, int) {
	want := set{}
	for _, a := range actors {
		want.add(a)
	}
	var matched []Event
	for i := len(d.events) - 1; i >= 0; i-- {
		e := d.events[i]
		if len(want) > 0 {
			if _, ok := want[e.actorKey()]; !ok {
				continue
			}
		}
		matched = append(matched, e)
	}
	total := len(matched)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	out := make([]EventRow, 0, end-offset)
	for _, e := range matched[offset:end] {
		iss := d.issues[e.IssueID]
		out = append(out, EventRow{
			At: e.At, Kind: e.Kind, ActorType: e.ActorType, ActorID: e.ActorID,
			IssueID: e.IssueID, Number: iss.Number, Title: iss.Title, ProjectID: iss.ProjectID,
			Status: iss.Status, StatusGrp: StatusGroup(iss.Status, iss.StatusCategory),
			ToStatus: e.ToStatus,
		})
	}
	return out, total
}

// IssueRow is one issue in the issue list.
type IssueRow struct {
	IssueID      string `json:"issue_id"`
	Number       int32  `json:"number"`
	Title        string `json:"title"`
	Status       string `json:"status"`
	StatusGroup  string `json:"status_group"`
	ProjectID    string `json:"project_id"`
	Participants int    `json:"participants"`
	Comments     int    `json:"comments"`
	// Interactions is comments plus status changes in the window.
	Interactions int `json:"interactions"`
	// CompletionSeconds runs from creation to the latest completion inside the
	// window; nil when the issue is not done or was completed before it.
	CompletionSeconds *int64 `json:"completion_seconds"`
}

type issueAcc struct {
	participants  set
	comments      int
	statusChanges int
	completedAt   time.Time
	lastAt        time.Time
}

// issueAccs accumulates per-issue activity over the window for the issues
// keep accepts.
func (d *Dataset) issueAccs(keep func(*Issue) bool) map[string]*issueAcc {
	accs := map[string]*issueAcc{}
	for _, e := range d.events {
		if !keep(d.issues[e.IssueID]) {
			continue
		}
		a, ok := accs[e.IssueID]
		if !ok {
			a = &issueAcc{participants: set{}}
			accs[e.IssueID] = a
		}
		if e.ActorType != ActorSystem {
			a.participants.add(e.actorKey())
		}
		switch e.Kind {
		case KindCommented:
			a.comments++
		case KindStatusChanged:
			a.statusChanges++
		}
		if e.Completes {
			a.completedAt = e.At
		}
		a.lastAt = e.At
	}
	return accs
}

func (d *Dataset) issueRow(id string, a *issueAcc) IssueRow {
	iss := d.issues[id]
	row := IssueRow{
		IssueID: id, Number: iss.Number, Title: iss.Title, Status: iss.Status,
		StatusGroup: StatusGroup(iss.Status, iss.StatusCategory), ProjectID: iss.ProjectID,
		Participants: len(a.participants), Comments: a.comments,
		Interactions: a.comments + a.statusChanges,
	}
	if row.StatusGroup == StatusDone && !a.completedAt.IsZero() {
		secs := int64(a.completedAt.Sub(iss.CreatedAt) / time.Second)
		row.CompletionSeconds = &secs
	}
	return row
}

func (d *Dataset) issueRows(ids []string, accs map[string]*issueAcc, limit, offset int) ([]IssueRow, int) {
	total := len(ids)
	if offset > total {
		offset = total
	}
	end := min(offset+limit, total)
	rows := make([]IssueRow, 0, end-offset)
	for _, id := range ids[offset:end] {
		rows = append(rows, d.issueRow(id, accs[id]))
	}
	return rows, total
}

// Issue list sort orders.
const (
	SortInteractions = "interactions"
	SortParticipants = "participants"
	SortCompletion   = "completion"
	SortRecent       = "recent"
)

// IsIssueSort reports whether s names an issue list order.
func IsIssueSort(s string) bool {
	switch s {
	case SortInteractions, SortParticipants, SortCompletion, SortRecent:
		return true
	}
	return false
}

// Issues lists every issue with activity in the window, ordered by sort
// (largest first; issues without a completion time last for "completion").
func (d *Dataset) Issues(sortBy string, limit, offset int) ([]IssueRow, int) {
	accs := d.issueAccs(func(*Issue) bool { return true })
	rows := make([]IssueRow, 0, len(accs))
	for id, a := range accs {
		rows = append(rows, d.issueRow(id, a))
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		switch sortBy {
		case SortParticipants:
			if a.Participants != b.Participants {
				return a.Participants > b.Participants
			}
		case SortCompletion:
			if (a.CompletionSeconds == nil) != (b.CompletionSeconds == nil) {
				return a.CompletionSeconds != nil
			}
			if a.CompletionSeconds != nil && *a.CompletionSeconds != *b.CompletionSeconds {
				return *a.CompletionSeconds > *b.CompletionSeconds
			}
		case SortRecent:
			if la, lb := accs[a.IssueID].lastAt, accs[b.IssueID].lastAt; !la.Equal(lb) {
				return la.After(lb)
			}
		default:
			if a.Interactions != b.Interactions {
				return a.Interactions > b.Interactions
			}
		}
		if la, lb := accs[a.IssueID].lastAt, accs[b.IssueID].lastAt; !la.Equal(lb) {
			return la.After(lb)
		}
		return a.Number > b.Number
	})
	total := len(rows)
	if offset > total {
		offset = total
	}
	return rows[offset:min(offset+limit, total)], total
}
