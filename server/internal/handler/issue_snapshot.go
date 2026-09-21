package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// issueSnapshotVersion is the shape version of the JSON stored in
// agent_task_queue.issue_snapshot.
//
// A snapshot carrying any other version is treated as UNKNOWN rather than
// coerced, which degrades to the unconditional issue read — the behaviour that
// predates this column.
//
// Bump it when a stored row can no longer answer the question the current code
// asks of it. In practice that is exactly two cases:
//
//   - a field is ADDED to the compared set, because old rows do not carry it
//     and their silence would read as "unchanged";
//   - the way a value is derived changes (a different hash, a different
//     normalisation), because the same content would then compare as different.
//
// REMOVING a field does not qualify and must not bump: the remaining fields are
// still present in old rows and still mean the same thing, so the comparison
// stays correct and the extra key is simply ignored. Dropping status (MUL-7344
// review) is that case, which is why this is still 1 even though rows written
// by the previous shape already exist.
const issueSnapshotVersion = 1

// Compared field names, in the fixed order they are reported.
//
// The set answers exactly one question — "must the agent re-read the issue
// BODY?" — so only title and description qualify. They are the task itself, and
// nothing but a read can deliver them.
//
// Everything else is excluded because the per-turn message already carries the
// current value, or because changing it does not change the agent's work:
//
//   - status: shipped as a current value on every claim, so the agent learns it
//     without a read. Comparing it was a mistake measured, not argued: the
//     workflow has the agent set in_progress and in_review on its OWN runs, so
//     its own bookkeeping counted as "changed" and reported a body re-read that
//     nothing in the body justified. Replaying this issue's 21 follow-ups, that
//     alone cut "unchanged" from 17 to 4. "Somebody intervened" is still
//     visible — the agent compares the shipped status against what it last set.
//   - assignee: shipped as a current value; "is this mine now" needs no compare.
//   - priority: only a read reveals it, but it does not change what the agent
//     does.
//
// The agent is told exactly this set was compared, so a field absent here must
// never be implied to have been checked: status, assignee, priority, labels,
// parent, due date, stage, project and metadata are all out of scope, and an
// issue whose ONLY change is one of them is reported as unchanged.
const (
	issueFieldTitle       = "title"
	issueFieldDescription = "description"
)

// issueStateSnapshot is the comparison key for one claim's view of an issue.
//
// Title and description are stored as SHA-256 hex, not as text: the column
// exists to answer "did this move", and a second copy of every issue body in
// the task queue would be both a storage cost and a place for issue text to
// leak from.
//
// The claim's CURRENT status and assignee reach the agent as their own response
// fields, read straight off the issue row — they were never sourced from here,
// so dropping status from the comparison did not affect them.
type issueStateSnapshot struct {
	Version           int    `json:"v"`
	TitleSHA256       string `json:"title_sha256"`
	DescriptionSHA256 string `json:"description_sha256"`
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// buildIssueStateSnapshot captures the compared fields of an issue as of now.
func buildIssueStateSnapshot(issue db.Issue) issueStateSnapshot {
	return issueStateSnapshot{
		Version:           issueSnapshotVersion,
		TitleSHA256:       sha256Hex(issue.Title),
		DescriptionSHA256: sha256Hex(issue.Description.String),
	}
}

// changedFieldsSince reports which compared fields differ from prev, in the
// fixed order above. An empty result means every compared field matched — and
// says nothing about the fields outside the set.
func (s issueStateSnapshot) changedFieldsSince(prev issueStateSnapshot) []string {
	var changed []string
	if s.TitleSHA256 != prev.TitleSHA256 {
		changed = append(changed, issueFieldTitle)
	}
	if s.DescriptionSHA256 != prev.DescriptionSHA256 {
		changed = append(changed, issueFieldDescription)
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

// taskStatusCompleted is the only terminal status that PROVES a run delivered
// its prompt to the provider. The distinction matters because the row is a
// delta anchor, not just a session pointer — see resumedRunAnchor.
const taskStatusCompleted = "completed"

// resumedRunAnchor is the prior run whose provider session THIS claim hands
// back, and therefore the only run either of a claim's two deltas may be
// measured from.
//
// The distinction that makes this type necessary: "the run that started most
// recently" and "the run whose session we resume" are not the same row.
// GetLastTaskSession skips poisoned and retired sessions, and a manual rerun
// resumes an operator-chosen source, so both legitimately hand back an OLDER
// run. Measuring a delta against the newest run while resuming an older one
// reports "unchanged" to an agent whose resumed memory predates the change —
// the one failure mode this whole mechanism exists to avoid, and one with no
// symptom at runtime (MUL-7344, found in review).
//
// A nil *resumedRunAnchor means this claim resumes nothing it can date, so
// neither delta is computed and the daemon falls back to the reads it has
// always performed.
type resumedRunAnchor struct {
	// StartedAt dates the comment delta.
	StartedAt pgtype.Timestamptz
	// IssueSnapshot is the issue state that run was handed at ITS claim. Empty
	// for a run that predates the column.
	IssueSnapshot []byte
}

// newResumedRunAnchor returns the anchor for a resume source, or nil when that
// source cannot date a delta.
//
// A snapshot is written at CLAIM time, so its existence proves only that the
// server built a payload — not that the agent ever saw it. Between those two
// points the daemon writes started_at (TaskService.StartTask) and only then
// launches the provider, so a run can carry a snapshot AND a started_at and
// still have died before the prompt reached the session: the daemon exits
// during prepare, the runtime drops, and orphan recovery marks the row failed.
// Its snapshot then describes an issue that session never saw, and comparing
// against it reports "unchanged" across a real edit — plus a comment anchor
// that hides every comment older than it.
//
// Nothing on the row distinguishes "failed after the provider ran" from "failed
// before it ran": session_id is inherited by CreateRetryTask at insert, and
// started_at precedes the launch. The only terminal status that PROVES delivery
// is 'completed' — a run cannot finish a turn it never started.
//
// So failed and cancelled sources date nothing. Their SESSION is still resumed
// (GetLastTaskSession accepts them on purpose, and that behaviour is unchanged);
// the run simply performs the context reads it always did. That is the cheap
// side of the trade: those paths are the ones already recovering from a
// problem, and the optimization's main case — an ordinary follow-up after a
// healthy run — is untouched.
func newResumedRunAnchor(status string, startedAt pgtype.Timestamptz, snapshot []byte) *resumedRunAnchor {
	if status != taskStatusCompleted || !startedAt.Valid {
		return nil
	}
	return &resumedRunAnchor{StartedAt: startedAt, IssueSnapshot: snapshot}
}

// commentCountScope carries the issue/workspace/trigger identity the comment
// delta needs, captured while the trigger comment is loaded and consumed once
// the resume anchor is known.
type commentCountScope struct {
	AnchorID    pgtype.UUID
	IssueID     pgtype.UUID
	WorkspaceID pgtype.UUID
}
