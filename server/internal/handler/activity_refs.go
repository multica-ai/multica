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

// referencedIssueID returns the other issue an activity names, if any.
func referencedIssueID(action string, details []byte) (pgtype.UUID, bool) {
	ref, ok := activityIssueRefs[action]
	if !ok || len(details) == 0 {
		return pgtype.UUID{}, false
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(details, &fields); err != nil {
		return pgtype.UUID{}, false
	}
	var id string
	if err := json.Unmarshal(fields[ref.id], &id); err != nil || id == "" {
		return pgtype.UUID{}, false
	}
	parsed, err := util.ParseUUID(id)
	if err != nil {
		return pgtype.UUID{}, false
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
// from every activity whose referenced issue sits in a project hidden from
// the reader. actions[i] and details[i] describe the same activity. A
// referenced issue that no longer exists keeps its reference: activities
// carry a deleted issue's identifier on purpose so the row still reads.
func (h *Handler) redactHiddenActivityRefs(r *http.Request, hidden projectaccess.Hidden, actions []string, details [][]byte) error {
	if hidden.Empty() {
		return nil
	}
	refs := make([]pgtype.UUID, len(actions))
	var ids []pgtype.UUID
	for i, action := range actions {
		if id, ok := referencedIssueID(action, details[i]); ok {
			refs[i] = id
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	hiddenIssues, err := h.hiddenIssueIDs(r, hidden, ids)
	if err != nil {
		return err
	}
	for i, id := range refs {
		if !id.Valid {
			continue
		}
		if _, isHidden := hiddenIssues[id]; isHidden {
			details[i] = StripActivityIssueRef(actions[i], details[i])
		}
	}
	return nil
}
