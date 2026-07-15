package domainevent

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func testUUID(t *testing.T) pgtype.UUID {
	t.Helper()
	return pgtype.UUID{Bytes: uuid.New(), Valid: true}
}

// Every typed constructor must produce an envelope that agrees with the catalog
// (type, subject_type, schema_version) and passes validate — this is the guard
// the future public create path reuses, so it must hold for the internal callers
// too.
func TestConstructorsProduceValidEnvelopes(t *testing.T) {
	ws := testUUID(t)
	subj := testUUID(t)
	actor := MemberActor(testUUID(t))

	cases := []struct {
		name        string
		evt         Event
		wantType    string
		wantSubject string
	}{
		{"issue.created", IssueCreated(ws, subj, actor, IssueCreatedPayload{Status: "todo", Title: "x"}), TypeIssueCreated, SubjectIssue},
		{"issue.status_changed", IssueStatusChanged(ws, subj, actor, IssueStatusChangedPayload{From: "todo", To: "done"}), TypeIssueStatusChanged, SubjectIssue},
		{"issue.assigned", IssueAssigned(ws, subj, actor, IssueAssignedPayload{ToAssigneeType: "agent"}), TypeIssueAssigned, SubjectIssue},
		{"comment.created", CommentCreated(ws, subj, actor, CommentCreatedPayload{IssueID: uuid.NewString(), AuthorType: "member"}), TypeCommentCreated, SubjectComment},
		{"task.completed", TaskCompleted(ws, subj, SystemActor(), TaskCompletedPayload{IssueID: uuid.NewString()}), TypeTaskCompleted, SubjectTask},
		{"task.failed", TaskFailed(ws, subj, SystemActor(), TaskFailedPayload{Retryable: true}), TypeTaskFailed, SubjectTask},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.evt.validate(); err != nil {
				t.Fatalf("validate: %v", err)
			}
			if tc.evt.Type != tc.wantType {
				t.Errorf("Type = %q, want %q", tc.evt.Type, tc.wantType)
			}
			if tc.evt.SubjectType != tc.wantSubject {
				t.Errorf("SubjectType = %q, want %q", tc.evt.SubjectType, tc.wantSubject)
			}
			if tc.evt.SchemaVersion != 1 {
				t.Errorf("SchemaVersion = %d, want 1", tc.evt.SchemaVersion)
			}
			if !json.Valid(tc.evt.Payload) {
				t.Errorf("payload is not valid JSON: %s", tc.evt.Payload)
			}
			// A v1 domain write is always a root event.
			if tc.evt.CorrelationID.Valid || tc.evt.HopCount != 0 {
				t.Errorf("expected root event (no correlation, hop 0), got correlation.Valid=%v hop=%d", tc.evt.CorrelationID.Valid, tc.evt.HopCount)
			}
		})
	}
}

func TestValidateRejectsBadEnvelopes(t *testing.T) {
	ws := testUUID(t)
	subj := testUUID(t)
	base := IssueStatusChanged(ws, subj, MemberActor(testUUID(t)), IssueStatusChangedPayload{From: "a", To: "b"})

	mutate := func(f func(*Event)) Event {
		e := base
		f(&e)
		return e
	}

	cases := map[string]Event{
		"unknown type":       mutate(func(e *Event) { e.Type = "issue.exploded" }),
		"wrong schema ver":   mutate(func(e *Event) { e.SchemaVersion = 2 }),
		"wrong subject type": mutate(func(e *Event) { e.SubjectType = SubjectTask }),
		"bad actor type":     mutate(func(e *Event) { e.ActorType = "wizard" }),
		"missing workspace":  mutate(func(e *Event) { e.WorkspaceID = pgtype.UUID{} }),
		"missing subject":    mutate(func(e *Event) { e.SubjectID = pgtype.UUID{} }),
		"empty payload":      mutate(func(e *Event) { e.Payload = nil }),
	}
	for name, e := range cases {
		t.Run(name, func(t *testing.T) {
			if err := e.validate(); err == nil {
				t.Fatalf("expected validate to reject %s", name)
			}
		})
	}
}

// ActorFrom must never let an unknown or empty actor type mint a member/agent
// identity — it degrades to system so a mislabelled caller can't fabricate
// authorship (guards the accountable-actor split from MUL-4332 §8).
func TestActorFromDegradesUnknownToSystem(t *testing.T) {
	id := testUUID(t)
	if a := ActorFrom("member", id); a.Type != ActorMember || a.ID != id {
		t.Errorf("member: got %+v", a)
	}
	if a := ActorFrom("", id); a.Type != ActorSystem || a.ID.Valid {
		t.Errorf("empty type should degrade to system with no id, got %+v", a)
	}
	if a := ActorFrom("wizard", id); a.Type != ActorSystem || a.ID.Valid {
		t.Errorf("unknown type should degrade to system with no id, got %+v", a)
	}
	if a := ActorFrom("system", id); a.Type != ActorSystem || a.ID.Valid {
		t.Errorf("system actor never carries an id, got %+v", a)
	}
}

// The payload JSON shape is a wire contract the PR3 matcher's fixed-vocabulary
// predicates bind against, so pin the exact keys.
func TestPayloadJSONShape(t *testing.T) {
	raw, err := json.Marshal(IssueStatusChangedPayload{From: "todo", To: "in_progress"})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m["from"] != "todo" || m["to"] != "in_progress" {
		t.Errorf("unexpected status payload: %s", raw)
	}

	// omitempty must drop absent optional UUID fields rather than emit null.
	raw, _ = json.Marshal(IssueCreatedPayload{Status: "todo", Title: "x"})
	if got := string(raw); got != `{"status":"todo","title":"x"}` {
		t.Errorf("expected optional keys omitted, got %s", got)
	}
}
