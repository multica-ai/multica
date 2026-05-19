package feishu_test

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/channel/adapter/feishu"
)

// TestRealClient_ImplementsClient verifies that RealClient satisfies the
// Client interface at compile time. This is a belt-and-braces check on
// top of the var _ Client = (*RealClient)(nil) assertion in real_client.go.
func TestRealClient_ImplementsClient(t *testing.T) {
	t.Parallel()

	// Construct with dummy credentials — we're only checking the type,
	// not making network calls.
	rc := feishu.NewRealClient("cli_test", "secret", "", "")

	// Verify it implements Client by calling a method that doesn't need
	// a running connection.
	_ = rc.BotUserID // function value, not a call — just checking it exists
}

// TestRealClient_SubscribeReturnsChannel verifies that Subscribe returns
// a non-nil channel even before Start is called.
func TestRealClient_SubscribeReturnsChannel(t *testing.T) {
	t.Parallel()

	rc := feishu.NewRealClient("cli_test", "secret", "", "")
	ch := rc.Subscribe()
	if ch == nil {
		t.Error("Subscribe() returned nil; expected a non-nil channel")
	}
}
