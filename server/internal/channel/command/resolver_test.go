package command_test

import (
	"context"
	"testing"

	chaction "github.com/multica-ai/multica/server/internal/channel/action"
	chcommand "github.com/multica-ai/multica/server/internal/channel/command"
)

func TestRuleResolver_CommandSourceHint(t *testing.T) {
	t.Parallel()

	resolver := chcommand.NewRuleResolver(chcommand.NewRuleMatcher())
	got, err := resolver.Resolve(context.Background(), chcommand.Request{
		Text:       "帮我记一个 登录优化",
		SourceHint: chaction.SourceCommand,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Matched {
		t.Fatal("expected rule match")
	}
	if got.Intent.Source != chaction.SourceCommand {
		t.Fatalf("source = %q, want command", got.Intent.Source)
	}
}
