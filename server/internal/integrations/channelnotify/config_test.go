package channelnotify

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

type testSender struct{}

func (testSender) SendInbox(context.Context, Target, Notification) error { return nil }

func TestParseEnabledChannelsEmptyDisablesForwarding(t *testing.T) {
	if got := ParseEnabledChannels(" "); len(got) != 0 {
		t.Fatalf("ParseEnabledChannels(empty) = %v, want empty", got)
	}
}

func TestParseEnabledChannelsNormalizesDeduplicatesAndSorts(t *testing.T) {
	got := ParseEnabledChannels(" feishu,slack,feishu, SLACK ")
	want := []channel.Type{"feishu", "slack"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("ParseEnabledChannels() = %v, want %v", got, want)
	}
}

func TestRegistryStoresAndListsSendersDeterministically(t *testing.T) {
	r := NewRegistry()
	r.Register("slack", testSender{})
	r.Register("feishu", testSender{})

	if _, ok := r.Lookup("feishu"); !ok {
		t.Fatal("Registry.Lookup(feishu) did not find registered sender")
	}
	got := r.Types()
	if len(got) != 2 || got[0] != "feishu" || got[1] != "slack" {
		t.Fatalf("Registry.Types() = %v, want [feishu slack]", got)
	}
}
