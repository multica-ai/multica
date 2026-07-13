package featureflags

import (
	"context"
	"testing"
)

func TestReleaseFlagsDefaultToOff(t *testing.T) {
	ctx := context.Background()
	if AgentBuilderEnabled(ctx, nil) {
		t.Fatal("agent builder release flag must default to off")
	}
	if ResourceLabelsEnabled(ctx, nil) {
		t.Fatal("resource labels release flag must default to off")
	}
	if AgentSkillTogglesEnabled(ctx, nil) {
		t.Fatal("agent skill toggles release flag must default to off")
	}
}
