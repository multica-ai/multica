package handler

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const projectListMaxLimit = 200

// projectNoLead is the lead filter value for projects without a lead.
const projectNoLead = "none"

// Sort keys for GET /api/projects, each with the direction it runs in when
// the request names none. Dates sort undated projects last either way.
var projectSortDefaultDesc = map[string]bool{
	"name":             false,
	"priority":         true,
	"status":           false,
	"progress":         true,
	"start_date":       false,
	"due_date":         false,
	"created_at":       true,
	"last_activity_at": true,
}

var projectPriorityRank = map[string]int{"urgent": 4, "high": 3, "medium": 2, "low": 1, "none": 0}
var projectStatusRank = map[string]int{"planned": 0, "in_progress": 1, "paused": 2, "completed": 3, "cancelled": 4}

// projectListQuery is GET /api/projects' query string: q, status, priority
// and lead (each repeatable or comma-separated; values within one are ORed),
// include_archived=true or archived=only, sort, direction, limit and offset.
// Without limit every match is returned.
type projectListQuery struct {
	includeArchived bool
	archivedOnly    bool
	search          string
	statuses        []string
	priorities      []string
	// leads holds "member:<uuid>", "agent:<uuid>" or projectNoLead.
	leads  []string
	sort   string
	desc   bool
	limit  int
	offset int
}

func projectListValues(q url.Values, key string) []string {
	var out []string
	for _, raw := range q[key] {
		out = append(out, splitCommaParam(raw)...)
	}
	return out
}

func parseProjectListQuery(q url.Values) (projectListQuery, error) {
	out := projectListQuery{
		search:     strings.ToLower(strings.TrimSpace(q.Get("q"))),
		statuses:   projectListValues(q, "status"),
		priorities: projectListValues(q, "priority"),
		leads:      projectListValues(q, "lead"),
		sort:       "created_at",
		desc:       true,
	}
	switch a := q.Get("archived"); a {
	case "":
		out.includeArchived = q.Get("include_archived") == "true"
	case "only":
		out.includeArchived, out.archivedOnly = true, true
	default:
		return out, fmt.Errorf("invalid archived %q: use only", a)
	}
	for _, s := range out.statuses {
		if !slices.Contains(validProjectStatuses, s) {
			return out, fmt.Errorf("invalid status %q: use one of %s", s, strings.Join(validProjectStatuses, ", "))
		}
	}
	for _, p := range out.priorities {
		if !slices.Contains(validProjectPriorities, p) {
			return out, fmt.Errorf("invalid priority %q: use one of %s", p, strings.Join(validProjectPriorities, ", "))
		}
	}
	for i, lead := range out.leads {
		if lead == projectNoLead {
			continue
		}
		actorType, id, found := strings.Cut(lead, ":")
		if !found || (actorType != "member" && actorType != "agent") {
			return out, fmt.Errorf("invalid lead %q: use member:<id>, agent:<id> or none", lead)
		}
		parsed, err := util.ParseUUID(id)
		if err != nil {
			return out, fmt.Errorf("invalid lead %q: %q is not a uuid", lead, id)
		}
		out.leads[i] = actorType + ":" + uuidToString(parsed)
	}

	if s := q.Get("sort"); s != "" {
		desc, ok := projectSortDefaultDesc[s]
		if !ok {
			keys := make([]string, 0, len(projectSortDefaultDesc))
			for k := range projectSortDefaultDesc {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			return out, fmt.Errorf("invalid sort %q: use one of %s", s, strings.Join(keys, ", "))
		}
		out.sort, out.desc = s, desc
	}
	switch d := q.Get("direction"); d {
	case "":
	case "asc", "desc":
		out.desc = d == "desc"
	default:
		return out, fmt.Errorf("invalid direction %q: use asc or desc", d)
	}

	if l := q.Get("limit"); l != "" {
		v, err := strconv.Atoi(l)
		if err != nil || v < 1 || v > projectListMaxLimit {
			return out, fmt.Errorf("invalid limit %q: use 1 to %d", l, projectListMaxLimit)
		}
		out.limit = v
	}
	if o := q.Get("offset"); o != "" {
		v, err := strconv.Atoi(o)
		if err != nil || v < 0 {
			return out, errors.New("invalid offset: use a number of projects to skip, 0 or more")
		}
		out.offset = v
	}
	return out, nil
}

func (q projectListQuery) matchesProject(p db.Project) bool {
	if q.archivedOnly && !p.ArchivedAt.Valid {
		return false
	}
	if q.search != "" && !strings.Contains(strings.ToLower(p.Title), q.search) {
		return false
	}
	if len(q.statuses) > 0 && !slices.Contains(q.statuses, p.Status) {
		return false
	}
	if len(q.priorities) > 0 && !slices.Contains(q.priorities, p.Priority) {
		return false
	}
	if len(q.leads) > 0 {
		lead := projectNoLead
		if p.LeadType.Valid && p.LeadID.Valid {
			lead = p.LeadType.String + ":" + uuidToString(p.LeadID)
		}
		if !slices.Contains(q.leads, lead) {
			return false
		}
	}
	return true
}

// page returns the requested slice of already-sorted projects.
func (q projectListQuery) page(resp []ProjectResponse) []ProjectResponse {
	if q.offset >= len(resp) {
		return []ProjectResponse{}
	}
	resp = resp[q.offset:]
	if q.limit > 0 && q.limit < len(resp) {
		resp = resp[:q.limit]
	}
	return resp
}

// projectProgress sorts by the same completed / scope ratio the list shows;
// a project with nothing in scope sorts below every project that has work.
func projectProgress(p ProjectResponse) float64 {
	if p.Progress.Scope == 0 {
		return -1
	}
	return float64(p.Progress.Completed) / float64(p.Progress.Scope)
}

func compareOptionalDays(a, b *string, desc bool) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return 1
	case b == nil:
		return -1
	}
	c := strings.Compare(*a, *b)
	if desc {
		return -c
	}
	return c
}

func compareTimestamps(a, b string) int {
	ta, errA := time.Parse(time.RFC3339Nano, a)
	tb, errB := time.Parse(time.RFC3339Nano, b)
	if errA != nil || errB != nil {
		return strings.Compare(a, b)
	}
	return ta.Compare(tb)
}

func compareNumbers[T int | float64](a, b T) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// sortProjectResponses orders projects by one key, then by title and id so
// that pages stay stable between requests.
func sortProjectResponses(resp []ProjectResponse, key string, desc bool) {
	sort.SliceStable(resp, func(i, j int) bool {
		a, b := resp[i], resp[j]
		var c int
		switch key {
		case "start_date":
			c = compareOptionalDays(a.StartDate, b.StartDate, desc)
		case "due_date":
			c = compareOptionalDays(a.DueDate, b.DueDate, desc)
		default:
			switch key {
			case "name":
				c = strings.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title))
			case "priority":
				c = compareNumbers(projectPriorityRank[a.Priority], projectPriorityRank[b.Priority])
			case "status":
				c = compareNumbers(projectStatusRank[a.Status], projectStatusRank[b.Status])
			case "progress":
				c = compareNumbers(projectProgress(a), projectProgress(b))
			case "last_activity_at":
				c = compareTimestamps(a.LastActivityAt, b.LastActivityAt)
			default:
				c = compareTimestamps(a.CreatedAt, b.CreatedAt)
			}
			if desc {
				c = -c
			}
		}
		if c == 0 && key != "name" {
			c = strings.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title))
		}
		if c == 0 {
			c = strings.Compare(a.ID, b.ID)
		}
		return c < 0
	})
}
