// JEH-1173: integration test. Exercises the cerebro mask layer against
// a real Persona SDK client backed by an httptest server that speaks the
// /v1/check protocol. This catches contract drift between the SDK and
// our wire-format assumptions — the unit tests above stub the SDK
// interface, so they don't.
//
// The test also covers the audit-write path end-to-end when DATABASE_URL
// is set, mirroring how the existing share-token integration test gates
// on DB availability.

package mask

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	persona "github.com/hvejsel/firtal-persona/sdk/go"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/handler"
)

// personaScript is one canned /v1/check response keyed on what the
// cerebro caller is asking about. The test server matches by kind + id.
type personaScript struct {
	kind         string
	id           string
	allowed      bool
	maskedFields []string
	reason       string
}

func newPersonaTestServer(t *testing.T, scripts []personaScript) (*httptest.Server, *[]map[string]any) {
	t.Helper()
	captured := make([]map[string]any, 0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/check" {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "Bearer ") {
			http.Error(w, "missing bearer", http.StatusUnauthorized)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		captured = append(captured, body)
		res, _ := body["resource"].(map[string]any)
		kind, _ := res["kind"].(string)
		id, _ := res["id"].(string)
		for _, s := range scripts {
			if s.kind == kind && s.id == id {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"allowed":       s.allowed,
					"decision_id":   "test-" + id,
					"reason":        s.reason,
					"masked_fields": s.maskedFields,
				})
				return
			}
		}
		// Default: deny so the test surfaces unexpected checks.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"allowed": false,
			"reason":  fmt.Sprintf("no scripted response for %s/%s", kind, id),
		})
	}))
	t.Cleanup(srv.Close)
	return srv, &captured
}

// TestIntegration_CheckerAllowWithMask drives the SDK against a real
// HTTP server and asserts the Checker forwards classification +
// field_classifications, parses masked_fields back, and returns Allowed.
func TestIntegration_CheckerAllowWithMask(t *testing.T) {
	srv, captured := newPersonaTestServer(t, []personaScript{
		{kind: KindIssue, id: "issue-1", allowed: true, maskedFields: []string{"description"}, reason: "pii field above grant"},
	})
	client, err := persona.New(persona.Config{Endpoint: srv.URL, Token: "test-token"})
	if err != nil {
		t.Fatalf("persona.New: %v", err)
	}
	c := NewChecker(client, slog.Default())
	dec := c.Decide(context.Background(),
		"actor-mia", "read", KindIssue, "issue-1", "pii",
		map[string]string{"description": "pii", "title": "pii"})
	if !dec.Allowed {
		t.Fatalf("expected Allowed=true, got %+v", dec)
	}
	if len(dec.MaskedFields) != 1 || dec.MaskedFields[0] != "description" {
		t.Errorf("expected MaskedFields=[description], got %v", dec.MaskedFields)
	}
	if dec.DecisionID != "test-issue-1" {
		t.Errorf("expected decision_id roundtrip, got %q", dec.DecisionID)
	}
	// Verify the SDK actually sent classification + field_classifications.
	if len(*captured) != 1 {
		t.Fatalf("expected 1 /v1/check call, got %d", len(*captured))
	}
	body := (*captured)[0]
	res := body["resource"].(map[string]any)
	attrs := res["attributes"].(map[string]any)
	if attrs["classification"] != "pii" {
		t.Errorf("expected classification=pii on wire, got %v", attrs["classification"])
	}
	fc, ok := attrs["field_classifications"].(map[string]any)
	if !ok || fc["description"] != "pii" {
		t.Errorf("expected field_classifications.description=pii on wire, got %v", attrs["field_classifications"])
	}
}

// TestIntegration_CheckerDenyMapsToAllowedFalse drives a deny response
// and confirms the Checker keeps it as Allowed=false (no MaskedFields
// inferred from a deny).
func TestIntegration_CheckerDenyMapsToAllowedFalse(t *testing.T) {
	srv, _ := newPersonaTestServer(t, []personaScript{
		{kind: KindComment, id: "comment-X", allowed: false, reason: "no grant"},
	})
	client, _ := persona.New(persona.Config{Endpoint: srv.URL, Token: "tk"})
	c := NewChecker(client, slog.Default())
	dec := c.Decide(context.Background(),
		"actor-mia", "read", KindComment, "comment-X", "pii", nil)
	if dec.Allowed {
		t.Fatalf("expected Allowed=false")
	}
	if len(dec.MaskedFields) != 0 {
		t.Errorf("deny must not carry MaskedFields, got %v", dec.MaskedFields)
	}
}

// TestIntegration_HandlerBridgeRoundTrip wires the bridge that the
// upstream handler talks to. Verifies Decision → PersonaMaskDecision
// translation is lossless across the seam.
func TestIntegration_HandlerBridgeRoundTrip(t *testing.T) {
	srv, _ := newPersonaTestServer(t, []personaScript{
		{kind: KindAttachment, id: "att-7", allowed: true, maskedFields: []string{"url", "filename"}},
	})
	client, _ := persona.New(persona.Config{Endpoint: srv.URL, Token: "tk"})
	bridge := NewHandlerBridge(NewChecker(client, slog.Default()))
	dec := bridge.Decide(context.Background(),
		"actor-1", "read", KindAttachment, "att-7", "pii",
		map[string]string{"url": "pii", "filename": "pii"})
	if !dec.Allowed {
		t.Fatalf("expected Allowed=true, got %+v", dec)
	}
	if len(dec.MaskedFields) != 2 {
		t.Errorf("expected 2 masked fields, got %v", dec.MaskedFields)
	}
}

// TestIntegration_AuditWriter_E2E exercises the audit writer against a
// real Postgres when DATABASE_URL is set (or the share-token fixture
// default). Skips otherwise.
func TestIntegration_AuditWriter_E2E(t *testing.T) {
	ctx := context.Background()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica_firtal_cerebro_630?sslmode=disable"
	}
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("no DB available: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Skipf("DB unreachable: %v", err)
	}
	q := cerebrodb.New(pool)
	writer := NewAuditWriter(q, slog.Default())

	wsUUID := pgtype.UUID{Bytes: [16]byte{0xde, 0xad, 0xbe, 0xef}, Valid: true}
	evt := handler.PersonaMaskAuditEvent{
		WorkspaceID:    uuidToText(wsUUID),
		ActorType:      "member",
		ActorID:        "actor-integration",
		Action:         "read",
		ResourceKind:   KindIssue,
		ResourceID:     "00000000-0000-0000-0000-000000001173",
		Classification: "pii",
		Decision:       "mask",
		MaskedFields:   []string{"description"},
		DecisionID:     "dec-1173",
		Reason:         "pii field above grant",
	}
	writer.Write(ctx, evt)

	// Read back via the list query.
	rows, err := q.ListPersonaMaskAuditByResource(ctx, cerebrodb.ListPersonaMaskAuditByResourceParams{
		ResourceKind: KindIssue,
		ResourceID:   evt.ResourceID,
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("list audit rows: %v", err)
	}
	if len(rows) == 0 {
		t.Fatalf("expected audit row, got 0")
	}
	got := rows[0]
	if got.ActorID != evt.ActorID || got.Decision != "mask" || got.Classification != "pii" {
		t.Errorf("audit row mismatch: %+v", got)
	}
	if !strings.Contains(string(got.MaskedFields), "description") {
		t.Errorf("masked_fields JSONB should contain 'description', got %s", string(got.MaskedFields))
	}

	// Clean up — the integration row uses a synthetic resource_id so
	// repeat runs don't accumulate but we still tidy on success.
	_, _ = pool.Exec(ctx,
		`DELETE FROM cerebro_persona_mask_audit WHERE resource_id = $1`,
		evt.ResourceID)
}

// uuidToText is a local copy that avoids importing handler/uuidToString
// (which is package-private). The integration test only needs to render
// a pgtype.UUID as canonical text for the audit row insert.
func uuidToText(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b := u.Bytes
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
