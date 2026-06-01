package statusmodels

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// fakeTxStarter satisfies TxStarter and lets a test fail Begin deterministically.
type fakeTxStarter struct {
	beginErr error
	calls    int
}

func (f *fakeTxStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	f.calls++
	return nil, f.beginErr
}

func TestSetIssueCustomStatusAtomic_BeginErrorBubblesUp(t *testing.T) {
	want := errors.New("pool exhausted")
	starter := &fakeTxStarter{beginErr: want}
	h := &Handler{Tx: starter}
	_, err := h.setIssueCustomStatusAtomic(context.Background(), setIssueCustomStatusArgs{})
	if !errors.Is(err, want) {
		t.Fatalf("expected Begin error to bubble up, got %v", err)
	}
	if starter.calls != 1 {
		t.Fatalf("expected exactly one Begin call, got %d", starter.calls)
	}
}

func TestSetIssueCustomStatus_FallbackPathWhenTxNil(t *testing.T) {
	// When the handler has no TxStarter wired (test shims), the atomic path
	// must skip the transactional branch and reach the fallback. We assert
	// this by giving the handler no Cerebro queries either — fallback will
	// try to call h.Cerebro.UpsertCerebroIssueStatus on nil and panic, which
	// is the proof that the fallback branch was taken (the atomic branch
	// would have failed earlier at Begin). Recover catches the panic.
	h := &Handler{}
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected fallback path to be reached (nil-deref on Cerebro)")
		}
	}()
	_, _ = h.setIssueCustomStatusAtomic(context.Background(), setIssueCustomStatusArgs{})
}

func threeValidStatuses() []statusEntry {
	return []statusEntry{
		{Key: "plan", Label: "Plan", Color: "#8b5cf6", BaseStatus: "todo", Description: "Issue is being planned"},
		{Key: "i_gang", Label: "I gang", Color: "#f59e0b", BaseStatus: "in_progress", Description: "Work has started"},
		{Key: "faerdig", Label: "Færdig", Color: "#10b981", BaseStatus: "done", Description: "Done and merged"},
	}
}

func TestValidateWriteRequest_RequiresName(t *testing.T) {
	err := validateWriteRequest(writeStatusModelRequest{Statuses: threeValidStatuses()})
	if err == nil {
		t.Fatal("empty name must be rejected")
	}
}

func TestValidateWriteRequest_RequiresMinStatuses(t *testing.T) {
	req := writeStatusModelRequest{
		Name: "Plan-først",
		Statuses: []statusEntry{
			{Key: "plan", Label: "Plan", BaseStatus: "todo"},
			{Key: "done", Label: "Done", BaseStatus: "done"},
		},
	}
	if err := validateWriteRequest(req); err == nil {
		t.Fatalf("a model with %d statuses must be rejected (min %d)", len(req.Statuses), minStatuses)
	}
}

func TestValidateWriteRequest_AcceptsThreeValidStatuses(t *testing.T) {
	req := writeStatusModelRequest{Name: "Plan-først", Statuses: threeValidStatuses()}
	if err := validateWriteRequest(req); err != nil {
		t.Fatalf("valid 3-status model rejected: %v", err)
	}
}

func TestValidateWriteRequest_RejectsUnknownBaseStatus(t *testing.T) {
	statuses := threeValidStatuses()
	statuses[1].BaseStatus = "shipped" // not one of the 7 base statuses
	req := writeStatusModelRequest{Name: "x", Statuses: statuses}
	if err := validateWriteRequest(req); err == nil {
		t.Fatal("status bound to an unknown base_status must be rejected")
	}
}

func TestValidateWriteRequest_RejectsDuplicateKeys(t *testing.T) {
	statuses := threeValidStatuses()
	statuses[2].Key = statuses[0].Key
	req := writeStatusModelRequest{Name: "x", Statuses: statuses}
	if err := validateWriteRequest(req); err == nil {
		t.Fatal("duplicate status keys must be rejected")
	}
}

func TestValidateWriteRequest_RejectsEmptyKeyOrLabel(t *testing.T) {
	statuses := threeValidStatuses()
	statuses[0].Label = "  "
	if err := validateWriteRequest(writeStatusModelRequest{Name: "x", Statuses: statuses}); err == nil {
		t.Fatal("blank label must be rejected")
	}
	statuses = threeValidStatuses()
	statuses[0].Key = ""
	if err := validateWriteRequest(writeStatusModelRequest{Name: "x", Statuses: statuses}); err == nil {
		t.Fatal("blank key must be rejected")
	}
}

func TestNormalizeStatuses_AssignsPositionByOrder(t *testing.T) {
	raw, err := normalizeStatuses(threeValidStatuses())
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}
	var got []statusEntry
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	for i, s := range got {
		if s.Position != i {
			t.Errorf("status %q: position = %d, want %d", s.Key, s.Position, i)
		}
	}
}

func TestDecodeStatuses_MalformedReturnsEmpty(t *testing.T) {
	if got := decodeStatuses([]byte("{not json")); len(got) != 0 {
		t.Fatalf("malformed JSON should decode to empty slice, got %d entries", len(got))
	}
	if got := decodeStatuses(nil); got == nil {
		t.Fatal("nil input should return a non-nil empty slice (parse-don't-crash)")
	}
}

// v2b tests (FIR-1550): description-required + triggers structure.

func TestValidateWriteRequest_RequiresDescription(t *testing.T) {
	statuses := threeValidStatuses()
	statuses[1].Description = " " // whitespace only
	req := writeStatusModelRequest{Name: "x", Statuses: statuses}
	if err := validateWriteRequest(req); err == nil {
		t.Fatal("blank description must be rejected — agents read this")
	}
}

func TestValidateWriteRequest_RejectsBadAssignToType(t *testing.T) {
	statuses := threeValidStatuses()
	statuses[0].Triggers = &statusTriggers{
		AssignToID:   "11111111-1111-1111-1111-111111111111",
		AssignToType: "robot",
	}
	req := writeStatusModelRequest{Name: "x", Statuses: statuses}
	if err := validateWriteRequest(req); err == nil {
		t.Fatal("assign_to_type must be member or agent")
	}
}

func TestValidateWriteRequest_RejectsHalfAssignPair(t *testing.T) {
	statuses := threeValidStatuses()
	statuses[0].Triggers = &statusTriggers{AssignToID: "11111111-1111-1111-1111-111111111111"}
	req := writeStatusModelRequest{Name: "x", Statuses: statuses}
	if err := validateWriteRequest(req); err == nil {
		t.Fatal("assign_to_id without assign_to_type must be rejected")
	}
}

func TestValidateWriteRequest_AcceptsFullTriggers(t *testing.T) {
	statuses := threeValidStatuses()
	statuses[0].Triggers = &statusTriggers{
		AssignToID:   "11111111-1111-1111-1111-111111111111",
		AssignToType: "agent",
		FireAgentID:  "22222222-2222-2222-2222-222222222222",
	}
	req := writeStatusModelRequest{Name: "x", Statuses: statuses}
	if err := validateWriteRequest(req); err != nil {
		t.Fatalf("full triggers must be accepted: %v", err)
	}
}

func TestFindStatusEntry(t *testing.T) {
	entries := threeValidStatuses()
	if got := findStatusEntry(entries, "i_gang"); got == nil || got.Label != "I gang" {
		t.Fatalf("findStatusEntry(i_gang) failed, got %+v", got)
	}
	if got := findStatusEntry(entries, "missing"); got != nil {
		t.Fatalf("findStatusEntry(missing) should return nil, got %+v", got)
	}
}

func TestValidateMapping(t *testing.T) {
	entries := threeValidStatuses() // plan->todo, i_gang->in_progress, faerdig->done

	// Empty mapping is always valid.
	if msg := validateMapping(nil, entries); msg != "" {
		t.Fatalf("nil mapping must be valid, got %q", msg)
	}

	// Valid: key exists and its base matches the map key.
	ok := map[string]string{"todo": "plan", "in_progress": "i_gang"}
	if msg := validateMapping(ok, entries); msg != "" {
		t.Fatalf("valid mapping rejected: %q", msg)
	}

	// Unknown base status.
	if msg := validateMapping(map[string]string{"nope": "plan"}, entries); msg == "" {
		t.Fatal("unknown base status must be rejected")
	}

	// Key exists but is bound to a different base (plan is todo, not done).
	if msg := validateMapping(map[string]string{"done": "plan"}, entries); msg == "" {
		t.Fatal("key bound to a different base must be rejected")
	}

	// Key does not exist in the model.
	if msg := validateMapping(map[string]string{"todo": "ghost"}, entries); msg == "" {
		t.Fatal("non-existent key must be rejected")
	}
}

func TestValidateCustomStatusKey_NoDBBranches(t *testing.T) {
	h := &Handler{} // no Cerebro queries — these branches must not touch the DB

	// Empty key: nothing to validate, returns nil before any DB access.
	if err := h.ValidateCustomStatusKey(context.Background(), pgtype.UUID{}, "todo", ""); err != nil {
		t.Fatalf("empty key must validate to nil, got %v", err)
	}
	if err := h.ValidateCustomStatusKey(context.Background(), pgtype.UUID{}, "todo", "   "); err != nil {
		t.Fatalf("blank key must validate to nil, got %v", err)
	}

	// Non-empty key with an unknown base status is a mismatch, returned before
	// any DB access.
	if err := h.ValidateCustomStatusKey(context.Background(), pgtype.UUID{}, "not_a_base", "plan"); !errors.Is(err, ErrCustomStatusKeyMismatch) {
		t.Fatalf("unknown base with explicit key must be ErrCustomStatusKeyMismatch, got %v", err)
	}
}
