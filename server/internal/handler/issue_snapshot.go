package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// issueSnapshotVersion is the shape version of the JSON stored in
// agent_task_queue.issue_snapshot. Bump it whenever the compared field SET
// changes — never when only a value's formatting changes, because a stored
// snapshot is compared against a freshly built one and a formatting drift
// inside the same version would read as a spurious "changed".
//
// A snapshot carrying any other version is treated as UNKNOWN rather than
// coerced: the two runs did not compare the same things, so neither
// "unchanged" nor a changed-field list would be true. Unknown degrades to the
// unconditional issue read, which is the behaviour that predates this column.
const issueSnapshotVersion = 1

// Compared field names, in the fixed order they are reported. The agent is
// told exactly this set was compared, so a field absent here must never be
// implied to have been checked: labels, parent, due date, stage, project and
// metadata are all deliberately out of scope, and an issue whose ONLY change is
// one of them is reported as unchanged.
const (
	issueFieldTitle       = "title"
	issueFieldDescription = "description"
	issueFieldStatus      = "status"
	issueFieldAssignee    = "assignee"
	issueFieldPriority    = "priority"
)

// issueStateSnapshot is the comparison key for one claim's view of an issue.
//
// Title and description are stored as SHA-256 hex, not as text: the column
// exists to answer "did this move", and a second copy of every issue body in
// the task queue would be both a storage cost and a place for issue text to
// leak from. Status, assignee and priority are short enums/ids, so they are
// stored raw — that also lets the claim report the CURRENT status and assignee
// to the agent without a second read.
type issueStateSnapshot struct {
	Version           int    `json:"v"`
	Status            string `json:"status"`
	AssigneeType      string `json:"assignee_type,omitempty"`
	AssigneeID        string `json:"assignee_id,omitempty"`
	Priority          string `json:"priority"`
	TitleSHA256       string `json:"title_sha256"`
	DescriptionSHA256 string `json:"description_sha256"`
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// buildIssueStateSnapshot captures the compared fields of an issue as of now.
func buildIssueStateSnapshot(issue db.Issue) issueStateSnapshot {
	snap := issueStateSnapshot{
		Version:           issueSnapshotVersion,
		Status:            issue.Status,
		Priority:          issue.Priority,
		TitleSHA256:       sha256Hex(issue.Title),
		DescriptionSHA256: sha256Hex(issue.Description.String),
	}
	// An unassigned issue and an issue assigned to nobody-in-particular must
	// hash the same way, so read through the pgtype validity rather than
	// letting an invalid column contribute a zero UUID string.
	if issue.AssigneeType.Valid {
		snap.AssigneeType = issue.AssigneeType.String
	}
	if issue.AssigneeID.Valid {
		snap.AssigneeID = uuidToString(issue.AssigneeID)
	}
	return snap
}

// changedFieldsSince reports which compared fields differ from prev, in the
// fixed order above. An empty result means every compared field matched.
//
// Assignee is ONE field: type and id only mean something together, and a
// member→agent reassignment that kept a coincidentally equal id half is still
// one reassignment to report.
func (s issueStateSnapshot) changedFieldsSince(prev issueStateSnapshot) []string {
	var changed []string
	if s.TitleSHA256 != prev.TitleSHA256 {
		changed = append(changed, issueFieldTitle)
	}
	if s.DescriptionSHA256 != prev.DescriptionSHA256 {
		changed = append(changed, issueFieldDescription)
	}
	if s.Status != prev.Status {
		changed = append(changed, issueFieldStatus)
	}
	if s.AssigneeType != prev.AssigneeType || s.AssigneeID != prev.AssigneeID {
		changed = append(changed, issueFieldAssignee)
	}
	if s.Priority != prev.Priority {
		changed = append(changed, issueFieldPriority)
	}
	return changed
}

// decodeIssueStateSnapshot parses a stored snapshot. It returns ok=false for
// every state that means "this claim cannot say what changed": no prior run, a
// row written before the column existed, a snapshot whose write lost its CAS,
// malformed JSON, or a different shape version. Callers must map ok=false to
// "not compared" and never to "unchanged" — the whole point of the flag is that
// those two are not the same answer, and only one of them may replace a read.
func decodeIssueStateSnapshot(raw []byte) (issueStateSnapshot, bool) {
	if len(raw) == 0 {
		return issueStateSnapshot{}, false
	}
	var snap issueStateSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return issueStateSnapshot{}, false
	}
	if snap.Version != issueSnapshotVersion {
		return issueStateSnapshot{}, false
	}
	return snap, true
}
