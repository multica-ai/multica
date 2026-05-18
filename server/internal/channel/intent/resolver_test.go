package intent_test

import (
	"context"
	"testing"

	in "github.com/multica-ai/multica/server/internal/channel/intent"
)

func TestRuleResolver_CommandSourceHint(t *testing.T) {
	t.Parallel()

	resolver := in.NewRuleResolver(in.NewRuleMatcher())
	got, err := resolver.Resolve(context.Background(), in.IntentRequest{
		Text:       "帮我记一个 登录优化",
		SourceHint: in.SourceCommand,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Matched {
		t.Fatal("expected rule match")
	}
	if got.Intent.Source != in.SourceCommand {
		t.Fatalf("source = %q, want command", got.Intent.Source)
	}
}
