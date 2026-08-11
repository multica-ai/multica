package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// nowRFC3339 returns the current UTC time in the wire DateTime form.
func nowRFC3339() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05Z07:00")
}

// parseUUIDParam parses a canonical lowercase UUID. Invalid input yields err.
func parseUUIDParam(s string) (pgtype.UUID, error) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return pgtype.UUID{}, err
	}
	return u, nil
}

// workspaceIDFromRequest extracts the workspace id stamped by the workspace
// membership middleware. Returns ok=false when absent (caller rejects 403).
func workspaceIDFromRequest(r *http.Request) (string, bool) {
	id := r.Header.Get("X-Workspace-ID")
	if id == "" {
		return "", false
	}
	return id, true
}

func getEvidenceScoreParams(execID pgtype.UUID, version string) db.GetEvidenceScoreParams {
	return db.GetEvidenceScoreParams{ExecutionID: execID, AlgorithmVersion: version}
}

func uuidOrNil(u pgtype.UUID) *string {
	if !u.Valid {
		return nil
	}
	s := u.String()
	return &s
}

func textOrNil(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	s := t.String
	return &s
}

func tsOrNil(t pgtype.Timestamptz) *string {
	if !t.Valid {
		return nil
	}
	s := t.Time.UTC().Format("2006-01-02T15:04:05Z07:00")
	return &s
}

// payloadRefFromJSON decodes a stored PayloadRef JSONB into the wire type.
func payloadRefFromJSON(b []byte) *protocol.PayloadRef {
	if len(b) == 0 {
		return nil
	}
	var ref protocol.PayloadRef
	if err := json.Unmarshal(b, &ref); err != nil {
		return nil
	}
	return &ref
}

// payloadRefsFromJSON decodes a stored PayloadRef array JSONB.
func payloadRefsFromJSON(b []byte) []protocol.PayloadRef {
	if len(b) == 0 {
		return []protocol.PayloadRef{}
	}
	var refs []protocol.PayloadRef
	if err := json.Unmarshal(b, &refs); err != nil {
		return []protocol.PayloadRef{}
	}
	if refs == nil {
		return []protocol.PayloadRef{}
	}
	return refs
}

func evidenceRecordToWire(r db.ExecutionEvidenceRecord) protocol.EvidenceRecord {
	return protocol.EvidenceRecord{
		SchemaVersion:        protocol.MemoryHubSchemaVersion,
		ExecutionID:          r.ExecutionID.String(),
		RuntimeEvidenceState: protocol.RuntimeEvidenceState(r.RuntimeEvidenceState),
		OutputRef:            payloadRefFromJSON(r.OutputRef),
		MessageRefs:          payloadRefsFromJSON(r.MessageRefs),
		UsageRefs:            payloadRefsFromJSON(r.UsageRefs),
		ArtifactRefs:         payloadRefsFromJSON(r.ArtifactRefs),
		TestRefs:             payloadRefsFromJSON(r.TestRefs),
		ReviewPolicy:         protocol.ReviewPolicyMode(r.ReviewPolicy),
		ReviewState:          protocol.ReviewState(r.ReviewState),
		ReviewVersion:        int(r.ReviewVersion),
		ReviewerAgentID:      uuidOrNil(r.ReviewerAgentID),
		ReviewTaskID:         uuidOrNil(r.ReviewTaskID),
		ReviewOutputRef:      payloadRefFromJSON(r.ReviewOutputRef),
		ReviewAttempt:        int(r.ReviewAttempt),
		MaxReviewAttempts:    int(r.MaxReviewAttempts),
		ReviewNextWakeup:     tsOrNil(r.ReviewNextWakeup),
		ReviewFailureCode:    textOrNil(r.ReviewFailureCode),
		CreatedAt:            r.CreatedAt.Time.UTC().Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:            r.UpdatedAt.Time.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
}

func evidenceEventToWire(e db.ExecutionEvidenceEvent) protocol.EvidenceEvent {
	return protocol.EvidenceEvent{
		SchemaVersion: protocol.MemoryHubSchemaVersion,
		ID:            e.ID.String(),
		ExecutionID:   e.ExecutionID.String(),
		RunID:         e.RunID,
		WorkspaceID:   e.WorkspaceID.String(),
		ProjectID:     uuidOrNil(e.ProjectID),
		AgentID:       e.AgentID.String(),
		RuntimeID:     e.RuntimeID.String(),
		Model:         e.Model,
		Sequence:      e.Sequence,
		OccurredAt:    e.OccurredAt.Time.UTC().Format("2006-01-02T15:04:05Z07:00"),
		Kind:          protocol.EvidenceKind(e.Kind),
		SHA256:        e.PayloadSha256,
	}
}

func evidenceScoreToWire(s db.ExecutionEvidenceScore) protocol.EvidenceScore {
	return protocol.EvidenceScore{
		SchemaVersion:    protocol.MemoryHubSchemaVersion,
		ExecutionID:      s.ExecutionID.String(),
		AlgorithmVersion: s.AlgorithmVersion,
		InputDigest:      s.InputDigest,
		Availability:     int(s.Availability),
		Isolation:        int(s.Isolation),
		Security:         int(s.Security),
		Recovery:         int(s.Recovery),
		Performance:      int(s.Performance),
		Observability:    int(s.Observability),
		Overall:          int(s.Overall),
		Eligible:         s.Eligible,
		ComputedAt:       s.ComputedAt.Time.UTC().Format("2006-01-02T15:04:05Z07:00"),
		EvidenceRefs:     uuidsToStrings(s.EvidenceRefs),
	}
}

// MemoryHubCapabilitiesToWire converts the handler-layer capabilities object
// to the protocol shape (identical JSON; kept separate to avoid an import
// cycle with pkg/protocol inside the authz evaluator).
func MemoryHubCapabilitiesToWire(c MemoryHubCapabilities) protocol.MemoryHubCapabilities {
	return protocol.MemoryHubCapabilities{
		SchemaVersion:     c.SchemaVersion,
		CanManage:         c.CanManage,
		CanDeleteRemote:   c.CanDeleteRemote,
		CanWithdrawMemory: c.CanWithdrawMemory,
		CanReadDocket:     c.CanReadDocket,
		CanWriteConfig:    c.CanWriteConfig,
	}
}

// normalizeSpaces trims and collapses internal whitespace (used for search
// normalization; mirrors the V5-4 "Unicode-trimmed" candidate q rule).
func normalizeSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
