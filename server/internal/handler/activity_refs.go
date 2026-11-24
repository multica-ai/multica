package handler

import (
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/projectaccess"
	"github.com/multica-ai/multica/server/internal/util"
)

// activityIssueRef names the details keys of an activity that point at a
// second issue: its id, for linking, and its identifier, for reading.
type activityIssueRef struct {
	id         string
	identifier string
}

// activityIssueRefs lists the activities that name another issue. That issue
// can sit in a private project the reader of this issue's timeline (or its
// project's feed) cannot see, so its reference is redacted per reader.
var activityIssueRefs = map[string]activityIssueRef{
	"duplicate_marked":   {id: "original_id", identifier: "original_identifier"},
	"duplicate_unmarked": {id: "original_id", identifier: "original_identifier"},
	"duplicate_added":    {id: "duplicate_id", identifier: "duplicate_identifier"},
	"duplicate_removed":  {id: "duplicate_id", identifier: "duplicate_identifier"},
}

// ActivityIssueRef returns the other issue an activity names. named reports
// whether the details carry a reference at all; id is invalid when they do
// but its issue cannot be told from them, which leaves its visibility
// unknown.
func ActivityIssueRef(action string, details []byte) (id pgtype.UUID, named bool) {
	ref, ok := activityIssueRefs[action]
	if !ok || len(details) == 0 {
		return pgtype.UUID{}, false
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(details, &fields); err != nil {
		return pgtype.UUID{}, true
	}
	rawID, hasID := fields[ref.id]
	if _, hasIdentifier := fields[ref.identifier]; !hasID && !hasIdentifier {
		return pgtype.UUID{}, false
	}
	var s string
	if err := json.Unmarshal(rawID, &s); err != nil {
		return pgtype.UUID{}, true
	}
	parsed, err := util.ParseUUID(s)
	if err != nil {
		return pgtype.UUID{}, true
	}
	return parsed, true
}

// StripActivityIssueRef removes the other issue's id and identifier from an
// activity's details, keeping every other field. Clients render the row with
// a placeholder in its place. Activities that name no other issue come back
// unchanged.
func StripActivityIssueRef(action string, details []byte) []byte {
	ref, ok := activityIssueRefs[action]
	if !ok || len(details) == 0 {
		return details
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(details, &fields); err != nil {
		// Unreadable details cannot be redacted key by key; drop them all.
		return []byte("{}")
	}
	if _, hasID := fields[ref.id]; !hasID {
		if _, hasIdentifier := fields[ref.identifier]; !hasIdentifier {
			return details
		}
	}
	delete(fields, ref.id)
	delete(fields, ref.identifier)
	out, err := json.Marshal(fields)
	if err != nil {
		return []byte("{}")
	}
	return out
}

// redactHiddenActivityRefs strips, in place, the reference to another issue
// from every activity unless the reader can be shown to see that issue. A
// referenced issue that no longer exists is stripped for every reader: who
// could see it went with its project, and seeing every project today says
// nothing about a project deleted since. actions[i] and details[i] describe
// the same activity.
func (h *Handler) redactHiddenActivityRefs(r *http.Request, hidden projectaccess.Hidden, actions []string, details [][]byte) error {
	refs := make([]pgtype.UUID, len(actions))
	named := make([]bool, len(actions))
	var ids []pgtype.UUID
	for i, action := range actions {
		refs[i], named[i] = ActivityIssueRef(action, details[i])
		if refs[i].Valid {
			ids = append(ids, refs[i])
		}
	}
	visible := map[pgtype.UUID]bool{}
	if len(ids) > 0 {
		rows, err := h.Queries.ListIssueProjectIDs(r.Context(), ids)
		if err != nil {
			return err
		}
		for _, row := range rows {
			visible[row.ID] = hidden.CanSeeProject(row.ProjectID)
		}
	}
	for i := range actions {
		if named[i] && !visible[refs[i]] {
			details[i] = StripActivityIssueRef(actions[i], details[i])
		}
	}
	return nil
}
