package platformaction

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebro/permissions"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
)

func TestDecisionFor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		setting toolpolicy.Setting
		want    permissions.DecisionKind
	}{
		{name: "allow", setting: toolpolicy.SettingAllow, want: permissions.DecisionAllow},
		{name: "ask", setting: toolpolicy.SettingAsk, want: permissions.DecisionNeedsApproval},
		{name: "deny", setting: toolpolicy.SettingDeny, want: permissions.DecisionDeny},
		{name: "disable", setting: toolpolicy.SettingDisable, want: permissions.DecisionDeny},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := decisionFor(toolpolicy.Effective{Setting: tt.setting, Reason: tt.name})
			if got.Kind != tt.want || got.Reason != tt.name {
				t.Fatalf("decisionFor(%q) = %+v, want kind %q and preserved reason", tt.setting, got, tt.want)
			}
		})
	}
}
