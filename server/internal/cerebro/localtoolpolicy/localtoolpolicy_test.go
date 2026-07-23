package localtoolpolicy

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
)

func TestDecideAlwaysEnforces(t *testing.T) {
	for _, tc := range []struct {
		setting toolpolicy.Setting
		kind    DecisionKind
		allowed bool
	}{
		{toolpolicy.SettingAllow, KindAllow, true},
		{toolpolicy.SettingAsk, KindAsk, false},
		{toolpolicy.SettingDeny, KindDeny, false},
		{"", KindDeny, false},
		{toolpolicy.SettingInherit, KindDeny, false},
	} {
		got := Decide(toolpolicy.Effective{Setting: tc.setting, Reason: "test"})
		if got.Kind != tc.kind || got.Allowed != tc.allowed || !got.Enforced {
			t.Fatalf("Decide(%q) = %+v", tc.setting, got)
		}
	}
}

func TestAskNeedsApproval(t *testing.T) {
	if !Decide(toolpolicy.Effective{Setting: toolpolicy.SettingAsk}).NeedsApproval() {
		t.Fatal("Ask must route to approval")
	}
}
