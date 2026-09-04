package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// CompletionResultVersion1 is the only completion-result envelope version this
// server understands. A payload declaring anything else is rejected rather than
// guessed at: an unknown version means the producer is newer than us, and
// silently reading it with v1 rules would persist a result we mis-parsed.
const CompletionResultVersion1 = 1

// CompletionResultV1 is the canonical, versioned shape of a task's terminal
// product. It is the ONLY thing persisted to agent_task_queue.result and
// autopilot_run.result, and the only shape any consumer reads.
//
// What belongs here: genuinely terminal products of the run. What does NOT:
// anything the task row already owns a column and a top-level API field for —
// status, branch_name, work_dir/durable_work_dir, parent lineage, or the
// agent's session id. Those were previously carried along inside the result
// blob because the whole /complete request struct was marshalled verbatim,
// which made the blob a second, unversioned source of truth for data that
// already had one (and leaked session_id to every UI caller in the process).
type CompletionResultV1 struct {
	Version int `json:"version"`
	// Summary is the agent's final human-facing answer. An empty string is a
	// LEGAL terminal value — a tool-only chat turn genuinely produces no prose,
	// and writeChatCompletionOutcome depends on being able to tell that apart
	// from a failure. The field must nevertheless be PRESENT on an explicit v1
	// payload: "absent" means the producer never populated it, which is a bug
	// we want to hear about, while "" is a deliberate report of no prose.
	// Distinguishing them is the whole reason this is a strict parse.
	Summary string `json:"summary"`
	// ArtifactIDs references large products stored outside the message body.
	// Always non-nil after parsing — missing, null and [] all normalize to an
	// empty slice so no reader needs a nil check. Nothing produces these yet
	// (CODI-14 does); the semantics are pinned now so that landing artifacts
	// later is additive rather than another contract change.
	ArtifactIDs []string `json:"artifact_ids"`
}

// ErrLegacyCompletionResult signals that a payload carries no versioned
// envelope at all. It is not a malformed-input error: a pre-v1 daemon, and
// every historical row already in the database, look exactly like this. The
// caller decides what to do — the /complete boundary normalizes from the
// legacy `output` field, and the stored-row adapter does the same.
var ErrLegacyCompletionResult = errors.New("completion result: no versioned envelope")

// maxArtifactIDLen bounds an artifact identifier. Identifiers are opaque to
// this package but they are not free-form text: they end up in URLs and
// lookups, so a value longer than this is a producer bug, not a long name.
const maxArtifactIDLen = 256

// ParseCompletionResult strictly parses one completion-result envelope.
//
// This is the WRITE-boundary contract, and it is deliberately unforgiving:
// once a payload declares `version`, every downstream reader is entitled to
// assume the parse was exact. Falling back to legacy interpretation on a
// malformed v1 would persist a half-understood result under a v1 label, and
// nothing downstream could ever tell that had happened.
//
// Returns ErrLegacyCompletionResult when there is no envelope to parse, which
// callers treat as "normalize from the legacy output field", not as a failure.
//
// Note on NUL/UTF-8: this function VALIDATES artifact ids but does not rewrite
// them. Sanitizing an identifier by deleting bytes would silently change which
// object it points at — an id containing a NUL is rejected outright. Summary is
// free-form prose and IS sanitized, but by the caller at the storage boundary
// (util.SanitizeTextForPostgres), not here: this package must not depend on
// storage concerns.
func ParseCompletionResult(raw []byte) (CompletionResultV1, error) {
	var zero CompletionResultV1
	if len(raw) == 0 {
		return zero, ErrLegacyCompletionResult
	}

	// Probe for the envelope without committing to the full shape, so a
	// payload with no `version` can be reported as legacy rather than as a
	// type error on some other field.
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return zero, fmt.Errorf("completion result: not a JSON object: %w", err)
	}
	rawVersion, ok := probe["version"]
	if !ok {
		return zero, ErrLegacyCompletionResult
	}

	var version int
	if err := json.Unmarshal(rawVersion, &version); err != nil {
		return zero, fmt.Errorf("completion result: version is not an integer: %w", err)
	}
	if version != CompletionResultVersion1 {
		return zero, fmt.Errorf("completion result: unsupported version %d", version)
	}

	// summary must be present. See the field comment: absent and "" mean
	// different things and collapsing them would make the envelope unable to
	// distinguish a producer bug from a legitimately silent turn.
	rawSummary, ok := probe["summary"]
	if !ok {
		return zero, errors.New("completion result: v1 payload is missing required field summary")
	}
	// Reject `null` explicitly. Unmarshalling JSON null into a string is a
	// silent no-op in encoding/json — it leaves the target as "" and returns no
	// error — which would quietly collapse an explicit null into the legal
	// empty-string case and defeat the present/absent distinction above.
	if string(rawSummary) == "null" {
		return zero, errors.New("completion result: summary is null; use \"\" to report no prose")
	}
	var summary string
	if err := json.Unmarshal(rawSummary, &summary); err != nil {
		return zero, fmt.Errorf("completion result: summary is not a string: %w", err)
	}

	artifactIDs, err := parseArtifactIDs(probe["artifact_ids"])
	if err != nil {
		return zero, err
	}

	return CompletionResultV1{
		Version:     CompletionResultVersion1,
		Summary:     summary,
		ArtifactIDs: artifactIDs,
	}, nil
}

// parseArtifactIDs normalizes missing / null / [] to a non-nil empty slice and
// rejects anything that is not a clean list of usable identifiers.
func parseArtifactIDs(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 {
		return []string{}, nil
	}
	var ids []string
	if err := json.Unmarshal(raw, &ids); err != nil {
		return nil, fmt.Errorf("completion result: artifact_ids is not an array of strings: %w", err)
	}
	if ids == nil {
		return []string{}, nil
	}
	for i, id := range ids {
		switch {
		case id == "":
			return nil, fmt.Errorf("completion result: artifact_ids[%d] is empty", i)
		case len(id) > maxArtifactIDLen:
			return nil, fmt.Errorf("completion result: artifact_ids[%d] exceeds %d bytes", i, maxArtifactIDLen)
		case strings.ContainsRune(id, 0):
			// Rejected rather than stripped: an identifier is a name, and
			// deleting a byte from it would point at a different object.
			return nil, fmt.Errorf("completion result: artifact_ids[%d] contains a NUL byte", i)
		case !utf8.ValidString(id):
			return nil, fmt.Errorf("completion result: artifact_ids[%d] is not valid UTF-8", i)
		}
	}
	return ids, nil
}

// NewLegacyCompletionResult lifts a pre-v1 `output` string into the canonical
// shape. Legacy payloads carry exactly one meaningful value, so this is total:
// there is no malformed legacy input, only an empty one.
func NewLegacyCompletionResult(output string) CompletionResultV1 {
	return CompletionResultV1{
		Version:     CompletionResultVersion1,
		Summary:     output,
		ArtifactIDs: []string{},
	}
}

// ReadStoredResult is the READ-boundary adapter for a persisted result blob,
// and it is tolerant where ParseCompletionResult is strict.
//
// The asymmetry is deliberate. At the write boundary we control the producer
// and can reject bad input while it still has somewhere to go. A row already in
// the database has no such option: the run is over, and the choice is only
// between degrading and lying. So a blob we cannot parse yields ok=false, and
// callers render nothing rather than either (a) re-interpreting it under legacy
// rules — which would dress unparseable bytes up as a successful answer — or
// (b) echoing the raw blob back out, which is how session_id and absolute paths
// reached API clients in the first place.
//
// Historical legacy rows are NOT failures: they normalize through the same
// `output` lifting the write boundary uses, so a row written years before v1
// reads back as valid canonical v1.
func ReadStoredResult(raw []byte) (CompletionResultV1, bool) {
	if len(raw) == 0 {
		return CompletionResultV1{}, false
	}

	parsed, err := ParseCompletionResult(raw)
	if err == nil {
		return parsed, true
	}
	if !errors.Is(err, ErrLegacyCompletionResult) {
		// Explicitly-versioned but unreadable. Degrade; the caller logs.
		return CompletionResultV1{}, false
	}

	// No envelope: a legacy row. Recover the one field it carried.
	var legacy struct {
		Output string `json:"output"`
	}
	if err := json.Unmarshal(raw, &legacy); err != nil {
		return CompletionResultV1{}, false
	}
	return NewLegacyCompletionResult(legacy.Output), true
}
