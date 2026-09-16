package daemon

import (
	"context"
	"errors"
	"testing"
)

// fakeRedeemer is a test double for the server redeem endpoint. It records the
// task id + handle it was called with and returns a scripted result.
type fakeRedeemer struct {
	secret string
	err    error

	calls     int
	gotTaskID string
	gotHandle string
}

func (f *fakeRedeemer) RedeemRequestSecret(ctx context.Context, taskID, handle string) (string, error) {
	f.calls++
	f.gotTaskID = taskID
	f.gotHandle = handle
	if f.err != nil {
		return "", f.err
	}
	return f.secret, nil
}

const (
	testFetchToken = "mat_deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	testTaskID     = "11111111-1111-1111-1111-111111111111"
)

func TestResolveRequestSecret_NoRefIsNoOp(t *testing.T) {
	// No ref → no-op, redeemer never called, nothing stashed, owner-scoped
	// tasks unaffected.
	f := &fakeRedeemer{secret: "unused"}
	b := newRequestSecretBroker(0)
	if err := resolveRequestSecretIntoBroker(context.Background(), f, b, testTaskID, testFetchToken, ""); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if f.calls != 0 {
		t.Fatalf("redeemer must not be called for empty ref; calls=%d", f.calls)
	}
	if b.len() != 0 {
		t.Fatalf("nothing should be stashed for empty ref; len=%d", b.len())
	}
}

func TestResolveRequestSecret_RedeemsIntoBroker(t *testing.T) {
	f := &fakeRedeemer{secret: "jwt-value-123"}
	b := newRequestSecretBroker(0)
	if err := resolveRequestSecretIntoBroker(context.Background(), f, b, testTaskID, testFetchToken, "visitor-abc"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The secret lives ONLY in the broker, fetchable with the bound token.
	if b.len() != 1 {
		t.Fatalf("expected exactly one broker entry, got %d", b.len())
	}
	got, ok := b.Take(testTaskID, testFetchToken)
	if !ok || got != "jwt-value-123" {
		t.Fatalf("expected to take the stashed secret, got %q ok=%v", got, ok)
	}
	// One-shot: gone after Take.
	if _, ok := b.Take(testTaskID, testFetchToken); ok {
		t.Fatal("broker entry must be one-shot")
	}
	// The task id and handle must be forwarded verbatim, redeemed exactly once.
	if f.calls != 1 {
		t.Fatalf("expected exactly one redeem call, got %d", f.calls)
	}
	if f.gotTaskID != testTaskID {
		t.Fatalf("expected task id forwarded, got %q", f.gotTaskID)
	}
	if f.gotHandle != "visitor-abc" {
		t.Fatalf("expected handle forwarded, got %q", f.gotHandle)
	}
}

func TestResolveRequestSecret_FailClosedNilRedeemer(t *testing.T) {
	b := newRequestSecretBroker(0)
	if err := resolveRequestSecretIntoBroker(context.Background(), nil, b, testTaskID, testFetchToken, "visitor-abc"); err == nil {
		t.Fatal("expected fail-closed error when redeemer is nil")
	}
	if b.len() != 0 {
		t.Fatalf("nothing should be stashed on failure; len=%d", b.len())
	}
}

func TestResolveRequestSecret_FailClosedNilSink(t *testing.T) {
	f := &fakeRedeemer{secret: "jwt-value-123"}
	if err := resolveRequestSecretIntoBroker(context.Background(), f, nil, testTaskID, testFetchToken, "visitor-abc"); err == nil {
		t.Fatal("expected fail-closed error when broker is nil")
	}
}

func TestResolveRequestSecret_FailClosedMissingTaskID(t *testing.T) {
	f := &fakeRedeemer{secret: "jwt-value-123"}
	b := newRequestSecretBroker(0)
	if err := resolveRequestSecretIntoBroker(context.Background(), f, b, "", testFetchToken, "visitor-abc"); err == nil {
		t.Fatal("expected fail-closed error when task id missing")
	}
	if f.calls != 0 {
		t.Fatalf("redeemer must not be called without a task id; calls=%d", f.calls)
	}
}

func TestResolveRequestSecret_FailClosedMissingFetchToken(t *testing.T) {
	f := &fakeRedeemer{secret: "jwt-value-123"}
	b := newRequestSecretBroker(0)
	if err := resolveRequestSecretIntoBroker(context.Background(), f, b, testTaskID, "", "visitor-abc"); err == nil {
		t.Fatal("expected fail-closed error when fetch token missing")
	}
	if f.calls != 0 {
		t.Fatalf("redeemer must not be called without a fetch token; calls=%d", f.calls)
	}
}

func TestResolveRequestSecret_FailClosedServerRejects(t *testing.T) {
	// Server rejection (not found / expired / replay / wrong task or principal)
	// surfaces as an error → task refuses to start, nothing stashed.
	f := &fakeRedeemer{err: errors.New("rejected")}
	b := newRequestSecretBroker(0)
	if err := resolveRequestSecretIntoBroker(context.Background(), f, b, testTaskID, testFetchToken, "visitor-abc"); err == nil {
		t.Fatal("expected fail-closed error when server rejects the handle")
	}
	if b.len() != 0 {
		t.Fatalf("nothing should be stashed on rejection; len=%d", b.len())
	}
}

func TestResolveRequestSecret_FailClosedEmptySecret(t *testing.T) {
	f := &fakeRedeemer{secret: "   \n"}
	b := newRequestSecretBroker(0)
	if err := resolveRequestSecretIntoBroker(context.Background(), f, b, testTaskID, testFetchToken, "visitor-abc"); err == nil {
		t.Fatal("expected fail-closed error for empty secret")
	}
	if b.len() != 0 {
		t.Fatalf("nothing should be stashed for empty secret; len=%d", b.len())
	}
}

func TestValidateRequestSecretRef_RejectsTraversal(t *testing.T) {
	for _, bad := range []string{"../secret", "a/b", "..", ".", "x/../y", "with\x00nul", "a\\b"} {
		if err := validateRequestSecretRef(bad); err == nil {
			t.Fatalf("expected rejection for %q", bad)
		}
	}
	if err := validateRequestSecretRef("visitor-abc-123"); err != nil {
		t.Fatalf("expected accept for clean ref, got %v", err)
	}
}

func TestResolveRequestSecret_FailClosedTraversal(t *testing.T) {
	// A malformed/hostile handle is rejected before any redeem call.
	f := &fakeRedeemer{secret: "jwt-value-123"}
	b := newRequestSecretBroker(0)
	if err := resolveRequestSecretIntoBroker(context.Background(), f, b, testTaskID, testFetchToken, "../../etc/passwd"); err == nil {
		t.Fatal("expected fail-closed error for traversal ref")
	}
	if f.calls != 0 {
		t.Fatalf("redeemer must not be called for a malformed handle; calls=%d", f.calls)
	}
}
