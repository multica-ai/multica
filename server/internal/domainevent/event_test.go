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
		{"task.failed", TaskFailed(ws, subj, SystemActor(), TaskFailedPayload{RetryEligible: true}), TypeTaskFailed, SubjectTask},
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
		"member without id":  mutate(func(e *Event) { e.ActorType = ActorMember; e.ActorID = pgtype.UUID{} }),
		"agent without id":   mutate(func(e *Event) { e.ActorType = ActorAgent; e.ActorID = pgtype.UUID{} }),
		"system with id":     mutate(func(e *Event) { e.ActorType = ActorSystem; e.ActorID = testUUID(t) }),
		"unknown type w/ id": mutate(func(e *Event) { e.ActorType = "wizard"; e.ActorID = testUUID(t) }),
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

// ActorFrom is fail-closed (MUL-4332 review point 6): a system actor is
// normalised to carry no id, but an unknown/empty type is passed through
// unchanged so validate rejects it — it must NOT be silently laundered into a
// system actor, which would permanently mis-record authorship.
func TestActorFromFailClosed(t *testing.T) {
	id := testUUID(t)
	ws, subj := testUUID(t), testUUID(t)
	build := func(a Actor) error {
		return IssueStatusChanged(ws, subj, a, IssueStatusChangedPayload{From: "a", To: "b"}).validate()
	}

	if a := ActorFrom("member", id); a.Type != ActorMember || a.ID != id {
		t.Errorf("member: got %+v", a)
	}
	if a := ActorFrom("system", id); a.Type != ActorSystem || a.ID.Valid {
		t.Errorf("system actor never carries an id, got %+v", a)
	}
	// Unknown / empty types survive into the envelope and are rejected there.
	if a := ActorFrom("wizard", id); a.Type != "wizard" {
		t.Errorf("unknown type must pass through, got %+v", a)
	}
	if err := build(ActorFrom("wizard", id)); err == nil {
		t.Error("unknown actor type must fail validate, not degrade to system")
	}
	if err := build(ActorFrom("", id)); err == nil {
		t.Error("empty actor type must fail validate")
	}
	// The valid shapes still pass.
	if err := build(MemberActor(id)); err != nil {
		t.Errorf("member actor should validate: %v", err)
	}
	if err := build(SystemActor()); err != nil {
		t.Errorf("system actor should validate: %v", err)
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
