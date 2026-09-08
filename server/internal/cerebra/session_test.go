package cerebra

import (
	"context"
	"testing"
	"time"
)

func TestSessionStore_TTL_And_Escalation(t *testing.T) {
	ctx := context.Background()
	store := NewSessionStore(50 * time.Millisecond)

	issueID := "issue-test-1"

	// 1. Set pin
	store.Set(ctx, issueID, "", "rt-1", "claude-haiku-3-5", TierSimple)
	pin := store.Get(ctx, issueID, "")
	if pin == nil || pin.Model != "claude-haiku-3-5" {
		t.Fatalf("expected pin for haiku, got %v", pin)
	}

	// 2. Higher-tier escalation updates pin
	store.Set(ctx, issueID, "", "rt-1", "claude-opus-4-5", TierHeavy)
	pin = store.Get(ctx, issueID, "")
	if pin == nil || pin.Model != "claude-opus-4-5" || pin.Tier != TierHeavy {
		t.Fatalf("expected escalated pin for opus, got %v", pin)
	}

	// 3. Lower-tier request does not demote active escalated pin
	store.Set(ctx, issueID, "", "rt-1", "claude-haiku-3-5", TierSimple)
	pin = store.Get(ctx, issueID, "")
	if pin == nil || pin.Model != "claude-opus-4-5" || pin.Tier != TierHeavy {
		t.Fatalf("expected escalated pin to be retained, got %v", pin)
	}

	// 4. TTL expiry
	time.Sleep(60 * time.Millisecond)
	expiredPin := store.Get(ctx, issueID, "")
	if expiredPin != nil {
		t.Fatalf("expected nil after TTL expiry, got %v", expiredPin)
	}
}
