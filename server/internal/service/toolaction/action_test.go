package toolaction

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const testDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestDecodeMetadataOnlyRejectsRawValues(t *testing.T) {
	for _, field := range []string{
		"arguments", "results", "headers", "url", "command_line",
		"environment", "provider_body", "secret",
	} {
		t.Run(field, func(t *testing.T) {
			raw := `{"transport_kind":"managed_mcp","server_key":"linear","tool_name":"list_issues","schema_digest":"` + testDigest + `","coverage_kind":"managed_mcp","event_type":"started","` + field + `":"raw-value"}`
			_, err := DecodeMetadataOnly(strings.NewReader(raw))
			if !errors.Is(err, ErrRawValue) {
				t.Fatalf("DecodeMetadataOnly error = %v, want %v", err, ErrRawValue)
			}
		})
	}
}

func TestValidateRejectsSecretCanary(t *testing.T) {
	event := Event{
		TransportKind: "managed_mcp",
		ServerKey:     "linear",
		ToolName:      "list_SECRET_CANARY_issues",
		SchemaDigest:  testDigest,
		CoverageKind:  "managed_mcp",
		EventType:     "started",
	}
	if err := Validate(event); !errors.Is(err, ErrSensitiveMetadata) {
		t.Fatalf("Validate error = %v, want %v", err, ErrSensitiveMetadata)
	}
}

func TestEventCannotSerializeRawValueFields(t *testing.T) {
	raw, err := json.Marshal(Event{
		TransportKind: "managed_mcp",
		ServerKey:     "linear",
		ToolName:      "list_issues",
		SchemaDigest:  testDigest,
		CoverageKind:  "managed_mcp",
		EventType:     "started",
	})
	if err != nil {
		t.Fatalf("Marshal error = %v", err)
	}
	encoded := strings.ToLower(string(raw))
	for _, field := range []string{"arguments", "results", "headers", "url", "command_line", "environment", "provider_body", "secret"} {
		if strings.Contains(encoded, `"`+field+`"`) {
			t.Fatalf("serialized event exposed %q: %s", field, raw)
		}
	}
}

func TestValidateRejectsURLAndCommandLineValues(t *testing.T) {
	base := Event{
		TransportKind: "managed_mcp",
		ServerKey:     "linear",
		ToolName:      "list_issues",
		SchemaDigest:  testDigest,
		CoverageKind:  "managed_mcp",
		EventType:     "started",
	}
	for _, value := range []string{"https://example.invalid/tool", "rm -rf workspace"} {
		event := base
		event.ToolName = value
		if err := Validate(event); !errors.Is(err, ErrRawValue) {
			t.Fatalf("Validate(%q) error = %v, want %v", value, err, ErrRawValue)
		}
	}
}

func TestRecordRejectsSensitiveMetadataBeforePersistence(t *testing.T) {
	queries := &recordingEventQueries{}
	service := NewSQLService(nil)
	event := validRecordEvent()
	event.ToolName = "SECRET_CANARY"
	if _, err := service.RecordIn(context.Background(), queries, event); !errors.Is(err, ErrSensitiveMetadata) {
		t.Fatalf("RecordIn error = %v, want %v", err, ErrSensitiveMetadata)
	}
	if queries.calls != 0 {
		t.Fatalf("sensitive event reached persistence %d time(s)", queries.calls)
	}
}

func TestRecordPropagatesAuditWriteFailure(t *testing.T) {
	queries := &recordingEventQueries{err: errors.New("audit unavailable")}
	if _, err := NewSQLService(nil).RecordIn(context.Background(), queries, validRecordEvent()); err == nil {
		t.Fatal("RecordIn succeeded after its audit write failed")
	}
}

type recordingEventQueries struct {
	calls int
	err   error
}

func (q *recordingEventQueries) CreateOrGetAgentToolActionEvent(context.Context, db.CreateOrGetAgentToolActionEventParams) (db.CreateOrGetAgentToolActionEventRow, error) {
	q.calls++
	return db.CreateOrGetAgentToolActionEventRow{ID: actionTestUUID(9), CreatedAt: pgtype.Timestamptz{Time: time.Unix(1, 0), Valid: true}}, q.err
}

func validRecordEvent() Event {
	return Event{
		WorkspaceID:   uuidString(1),
		AgentID:       uuidString(2),
		TaskID:        uuidString(3),
		InvocationID:  uuidString(4),
		TransportKind: "managed_mcp",
		ServerKey:     "linear",
		ToolName:      "list_issues",
		SchemaDigest:  testDigest,
		CoverageKind:  "managed_mcp",
		EventType:     "started",
		OutcomeCode:   "started",
	}
}

func actionTestUUID(seed byte) pgtype.UUID {
	var bytes [16]byte
	bytes[15] = seed
	return pgtype.UUID{Bytes: bytes, Valid: true}
}

func uuidString(seed byte) string {
	return "00000000-0000-0000-0000-00000000000" + string(rune('0'+seed))
}
