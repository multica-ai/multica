package handler

import (
	"encoding/json"
	"math"
	"net/http"
	"sort"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	// projectProgressMaxWeeks bounds the history (and the activity rows read
	// to build it) for long-running projects.
	projectProgressMaxWeeks = 52
	// projectProgressVelocityDays is the trailing window the forecast's
	// weekly completion rate is measured over.
	projectProgressVelocityDays = 28
	// projectProgressMaxForecastDays drops forecasts too far out to mean
	// anything, such as one issue finished in a month against hundreds open.
	projectProgressMaxForecastDays = 3 * 365
)

type ProjectProgressAssignee struct {
	// AssigneeType and AssigneeID are null for unassigned issues.
	AssigneeType *string `json:"assignee_type"`
	AssigneeID   *string `json:"assignee_id"`
	Scope        int64   `json:"scope"`
	Completed    int64   `json:"completed"`
	Started      int64   `json:"started"`
}

// ProjectProgressLabel counts the issues carrying one label. An issue with
// several labels counts under each, so rows can add up to more than the scope.
type ProjectProgressLabel struct {
	// LabelID is null for the issues with no label.
	LabelID   *string `json:"label_id"`
	Scope     int64   `json:"scope"`
	Completed int64   `json:"completed"`
	Started   int64   `json:"started"`
}

// ProjectProgressPoint is the project's progress at the end of Date (UTC).
type ProjectProgressPoint struct {
	Date      string `json:"date"`
	Scope     int64  `json:"scope"`
	Started   int64  `json:"started"`
	Completed int64  `json:"completed"`
}

type ProjectProgressForecast struct {
	// CompletionDate is the calendar day ("YYYY-MM-DD") the remaining scope
	// is done at the trailing four-week completion rate.
	CompletionDate string  `json:"completion_date"`
	WeeklyVelocity float64 `json:"weekly_velocity"`
}

type ProjectProgressResponse struct {
	Progress   ProjectProgress           `json:"progress"`
	ByAssignee []ProjectProgressAssignee `json:"by_assignee"`
	ByLabel    []ProjectProgressLabel    `json:"by_label"`
	// History has one point a week from the project's start (at most a year
	// back) and a final point for today. Scope counts the issues in the
	// project now, from when each was created: moves between projects are
	// not recorded, so an issue moved in later counts from its creation.
	History []ProjectProgressPoint `json:"history"`
	// Forecast is null until there is a week of history and a positive
	// completion rate, and once nothing in scope is left.
	Forecast *ProjectProgressForecast `json:"forecast"`
}

type progressStatusChange struct {
	at   time.Time
	from string
}

type progressIssue struct {
	status       string
	assigneeType string
	assigneeID   string
	labelIDs     []string
	createdAt    time.Time
	// changes are the issue's status changes after the history window
	// opens, oldest first.
	changes []progressStatusChange
}

// statusAt is the issue's status at t: the "from" of its first change after
// t, or its current status when nothing changed since.
func (i progressIssue) statusAt(t time.Time) string {
	idx := sort.Search(len(i.changes), func(k int) bool { return i.changes[k].at.After(t) })
	if idx < len(i.changes) && i.changes[idx].from != "" {
		return i.changes[idx].from
	}
	return i.status
}

func utcDay(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// projectProgressWindowStart is the first day of the history: the project's
// start, or a year before today for an older project.
func projectProgressWindowStart(start, now time.Time) time.Time {
	today := utcDay(now)
	day := utcDay(start)
	if day.After(today) {
		return today
	}
	if earliest := today.AddDate(0, 0, -7*projectProgressMaxWeeks); day.Before(earliest) {
		return earliest
	}
	return day
}

func buildProjectProgressHistory(issues []progressIssue, categoryOf func(string) string, windowStart, now time.Time) []ProjectProgressPoint {
	today := utcDay(now)
	var days []time.Time
	for d := utcDay(windowStart); d.Before(today); d = d.AddDate(0, 0, 7) {
		days = append(days, d)
	}
	days = append(days, today)

	points := make([]ProjectProgressPoint, len(days))
	for i, d := range days {
		at := d.AddDate(0, 0, 1)
		if at.After(now) {
			at = now
		}
		p := ProjectProgressPoint{Date: d.Format("2006-01-02")}
		for _, issue := range issues {
			if issue.createdAt.After(at) {
				continue
			}
			switch categoryOf(issue.statusAt(at)) {
			case issuestatus.CategoryClosed:
				continue
			case issuestatus.CategoryDone:
				p.Completed++
			case issuestatus.CategoryStarted:
				p.Started++
			}
			p.Scope++
		}
		points[i] = p
	}
	return points
}

func forecastProjectCompletion(history []ProjectProgressPoint, now time.Time) *ProjectProgressForecast {
	if len(history) < 2 {
		return nil
	}
	today := utcDay(now)
	last := history[len(history)-1]
	remaining := last.Scope - last.Completed
	if remaining <= 0 {
		return nil
	}
	base := history[0]
	cutoff := today.AddDate(0, 0, -projectProgressVelocityDays)
	for _, p := range history[:len(history)-1] {
		if d, err := time.Parse("2006-01-02", p.Date); err == nil && !d.After(cutoff) {
			base = p
		}
	}
	baseDay, err := time.Parse("2006-01-02", base.Date)
	if err != nil {
		return nil
	}
	days := today.Sub(baseDay).Hours() / 24
	if days < 7 {
		return nil
	}
	velocity := float64(last.Completed-base.Completed) / (days / 7)
	if velocity <= 0 {
		return nil
	}
	ahead := int(math.Ceil(float64(remaining) / velocity * 7))
	if ahead > projectProgressMaxForecastDays {
		return nil
	}
	return &ProjectProgressForecast{
		CompletionDate: today.AddDate(0, 0, ahead).Format("2006-01-02"),
		WeeklyVelocity: math.Round(velocity*100) / 100,
	}
}

func summarizeProjectProgress(issues []progressIssue, categoryOf func(string) string) (ProjectProgress, []ProjectProgressAssignee) {
	var total, terminal, completed, started int64
	type key struct{ typ, id string }
	byAssignee := map[key]*ProjectProgressAssignee{}
	var order []key
	for _, issue := range issues {
		category := categoryOf(issue.status)
		total++
		if category == issuestatus.CategoryClosed {
			terminal++
			continue
		}
		k := key{issue.assigneeType, issue.assigneeID}
		row, ok := byAssignee[k]
		if !ok {
			row = &ProjectProgressAssignee{}
			if k.typ != "" && k.id != "" {
				typ, id := k.typ, k.id
				row.AssigneeType, row.AssigneeID = &typ, &id
			}
			byAssignee[k] = row
			order = append(order, k)
		}
		row.Scope++
		switch category {
		case issuestatus.CategoryDone:
			terminal++
			completed++
			row.Completed++
		case issuestatus.CategoryStarted:
			started++
			row.Started++
		}
	}
	rows := make([]ProjectProgressAssignee, 0, len(order))
	for _, k := range order {
		rows = append(rows, *byAssignee[k])
	}
	// Largest share first; unassigned last; ties in a stable order.
	sort.SliceStable(rows, func(a, b int) bool {
		ua, ub := rows[a].AssigneeID == nil, rows[b].AssigneeID == nil
		if ua != ub {
			return ub
		}
		if rows[a].Scope != rows[b].Scope {
			return rows[a].Scope > rows[b].Scope
		}
		if ua {
			return false
		}
		return *rows[a].AssigneeType+*rows[a].AssigneeID < *rows[b].AssigneeType+*rows[b].AssigneeID
	})
	return newProjectProgress(total, terminal, completed, started), rows
}

// summarizeProjectProgressByLabel breaks the scope down by label, largest
// first, with the unlabelled issues last.
func summarizeProjectProgressByLabel(issues []progressIssue, categoryOf func(string) string) []ProjectProgressLabel {
	byLabel := map[string]*ProjectProgressLabel{}
	var order []string
	add := func(labelID string, category string) {
		row, ok := byLabel[labelID]
		if !ok {
			row = &ProjectProgressLabel{}
			if labelID != "" {
				id := labelID
				row.LabelID = &id
			}
			byLabel[labelID] = row
			order = append(order, labelID)
		}
		row.Scope++
		switch category {
		case issuestatus.CategoryDone:
			row.Completed++
		case issuestatus.CategoryStarted:
			row.Started++
		}
	}
	for _, issue := range issues {
		category := categoryOf(issue.status)
		if category == issuestatus.CategoryClosed {
			continue
		}
		if len(issue.labelIDs) == 0 {
			add("", category)
			continue
		}
		for _, id := range issue.labelIDs {
			add(id, category)
		}
	}
	rows := make([]ProjectProgressLabel, 0, len(order))
	for _, id := range order {
		rows = append(rows, *byLabel[id])
	}
	sort.SliceStable(rows, func(a, b int) bool {
		na, nb := rows[a].LabelID == nil, rows[b].LabelID == nil
		if na != nb {
			return nb
		}
		if rows[a].Scope != rows[b].Scope {
			return rows[a].Scope > rows[b].Scope
		}
		if na {
			return false
		}
		return *rows[a].LabelID < *rows[b].LabelID
	})
	return rows
}

// GetProjectProgress returns a project's progress with its breakdown by
// assignee and label, weekly history and forecast completion date. (MUL-8879)
func (h *Handler) GetProjectProgress(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceID := h.resolveWorkspaceID(r)
	idUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "project id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	project, err := h.Queries.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{
		ID: idUUID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	if !h.requireProjectVisible(w, r, workspaceID, project.ID, http.StatusNotFound, "project not found") {
		return
	}

	rows, err := h.Queries.ListProjectProgressIssues(ctx, db.ListProjectProgressIssuesParams{
		WorkspaceID: wsUUID,
		ProjectID:   project.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load project progress")
		return
	}

	now := time.Now().UTC()
	start := project.CreatedAt.Time
	if project.StartDate.Valid && project.StartDate.Time.Before(start) {
		start = project.StartDate.Time
	}
	windowStart := projectProgressWindowStart(start, now)

	issues := make([]progressIssue, len(rows))
	index := make(map[string]int, len(rows))
	ids := make([]pgtype.UUID, len(rows))
	for i, row := range rows {
		issues[i] = progressIssue{
			status:       row.Status,
			assigneeType: row.AssigneeType.String,
			createdAt:    row.CreatedAt.Time,
		}
		if row.AssigneeType.Valid && row.AssigneeID.Valid {
			issues[i].assigneeID = uuidToString(row.AssigneeID)
		} else {
			issues[i].assigneeType = ""
		}
		for _, labelID := range row.LabelIds {
			issues[i].labelIDs = append(issues[i].labelIDs, uuidToString(labelID))
		}
		ids[i] = row.ID
		index[uuidToString(row.ID)] = i
	}

	if len(ids) > 0 {
		changes, err := h.Queries.ListIssueStatusChangesSince(ctx, db.ListIssueStatusChangesSinceParams{
			WorkspaceID: wsUUID,
			IssueIds:    ids,
			Since:       pgtype.Timestamptz{Time: windowStart, Valid: true},
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load project progress")
			return
		}
		for _, c := range changes {
			i, ok := index[uuidToString(c.IssueID)]
			if !ok {
				continue
			}
			var details struct {
				From string `json:"from"`
			}
			_ = json.Unmarshal(c.Details, &details)
			issues[i].changes = append(issues[i].changes, progressStatusChange{at: c.CreatedAt.Time, from: details.From})
		}
	}

	resolver := issuestatus.NewResolver(wsUUID)
	categoryOf := func(status string) string { return resolver.Category(ctx, h.Queries, status) }

	progress, byAssignee := summarizeProjectProgress(issues, categoryOf)
	history := buildProjectProgressHistory(issues, categoryOf, windowStart, now)
	writeJSON(w, http.StatusOK, ProjectProgressResponse{
		Progress:   progress,
		ByAssignee: byAssignee,
		ByLabel:    summarizeProjectProgressByLabel(issues, categoryOf),
		History:    history,
		Forecast:   forecastProjectCompletion(history, now),
	})
}
