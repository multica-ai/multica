package localtoolpolicy

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
)

func TestParseMode(t *testing.T) {
	cases := map[string]Mode{
		"":          ModeOff,
		"off":       ModeOff,
		"garbage":   ModeOff,
		"OFF":       ModeOff,
		"observe":   ModeObserve,
		" Observe ": ModeObserve,
		"dry-run":   ModeObserve,
		"log":       ModeObserve,
		"enforce":   ModeEnforce,
		"ENFORCE":   ModeEnforce,
		"on":        ModeEnforce,
		"block":     ModeEnforce,
	}
	for in, want := range cases {
		if got := ParseMode(in); got != want {
			t.Errorf("ParseMode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestModeFromEnvDefaultsOff(t *testing.T) {
	t.Setenv(EnvVar, "")
	if got := ModeFromEnv(); got != ModeOff {
		t.Fatalf("ModeFromEnv() with empty env = %q, want off", got)
	}
	t.Setenv(EnvVar, "enforce")
	if got := ModeFromEnv(); got != ModeEnforce {
		t.Fatalf("ModeFromEnv() = %q, want enforce", got)
	}
}

func TestModeFromFlags(t *testing.T) {
	cases := []struct {
		enabled, enforce bool
		want             Mode
	}{
		{false, false, ModeOff},
		{false, true, ModeOff},  // enforce is meaningless while the master is off
		{true, false, ModeObserve},
		{true, true, ModeEnforce},
	}
	for _, c := range cases {
		if got := ModeFromFlags(c.enabled, c.enforce); got != c.want {
			t.Errorf("ModeFromFlags(enabled=%v, enforce=%v) = %q, want %q", c.enabled, c.enforce, got, c.want)
		}
	}
}

func eff(s toolpolicy.Setting) toolpolicy.Effective {
	return toolpolicy.Effective{Setting: s, Reason: "test reason"}
}

// TestDecideTruthTable exercises every (mode, setting) cell of the documented
// truth table — the heart of the staged-rollout safety contract.
func TestDecideTruthTable(t *testing.T) {
	type want struct {
		kind       DecisionKind
		allowed    bool
		enforced   bool
		wouldBlock bool
	}
	cases := []struct {
		name    string
		mode    Mode
		setting toolpolicy.Setting
		want    want
	}{
		// off: everything allowed, nothing enforced, nothing flagged.
		{"off/allow", ModeOff, toolpolicy.SettingAllow, want{KindAllow, true, false, false}},
		{"off/ask", ModeOff, toolpolicy.SettingAsk, want{KindAllow, true, false, false}},
		{"off/deny", ModeOff, toolpolicy.SettingDeny, want{KindAllow, true, false, false}},

		// observe: always allowed, never enforced, but flags would-block for non-allow.
		{"observe/allow", ModeObserve, toolpolicy.SettingAllow, want{KindAllow, true, false, false}},
		{"observe/ask", ModeObserve, toolpolicy.SettingAsk, want{KindAllow, true, false, true}},
		{"observe/deny", ModeObserve, toolpolicy.SettingDeny, want{KindAllow, true, false, true}},

		// enforce: acts on the verdict.
		{"enforce/allow", ModeEnforce, toolpolicy.SettingAllow, want{KindAllow, true, true, false}},
		{"enforce/ask", ModeEnforce, toolpolicy.SettingAsk, want{KindAsk, false, true, true}},
		{"enforce/deny", ModeEnforce, toolpolicy.SettingDeny, want{KindDeny, false, true, true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := Decide(tc.mode, eff(tc.setting))
			if d.Kind != tc.want.kind {
				t.Errorf("Kind = %q, want %q", d.Kind, tc.want.kind)
			}
			if d.Allowed != tc.want.allowed {
				t.Errorf("Allowed = %v, want %v", d.Allowed, tc.want.allowed)
			}
			if d.Enforced != tc.want.enforced {
				t.Errorf("Enforced = %v, want %v", d.Enforced, tc.want.enforced)
			}
			if d.WouldBlock != tc.want.wouldBlock {
				t.Errorf("WouldBlock = %v, want %v", d.WouldBlock, tc.want.wouldBlock)
			}
			if d.Observed != tc.setting {
				t.Errorf("Observed = %q, want %q", d.Observed, tc.setting)
			}
			if d.Reason != "test reason" {
				t.Errorf("Reason = %q, want propagated", d.Reason)
			}
		})
	}
}

// TestDecideEnforceFailClosed: an unexpected setting (Inherit / unknown) must
// fail closed under enforce, never silently allow.
func TestDecideEnforceFailClosed(t *testing.T) {
	for _, s := range []toolpolicy.Setting{toolpolicy.SettingInherit, toolpolicy.Setting("bogus"), ""} {
		d := Decide(ModeEnforce, eff(s))
		if d.Allowed || d.Kind != KindDeny {
			t.Errorf("enforce/%q = {kind:%q allowed:%v}, want deny+blocked", s, d.Kind, d.Allowed)
		}
	}
}

func TestNeedsApproval(t *testing.T) {
	if !Decide(ModeEnforce, eff(toolpolicy.SettingAsk)).NeedsApproval() {
		t.Error("enforce/ask should NeedApproval")
	}
	if Decide(ModeObserve, eff(toolpolicy.SettingAsk)).NeedsApproval() {
		t.Error("observe/ask must NOT NeedApproval (dry run never blocks)")
	}
	if Decide(ModeEnforce, eff(toolpolicy.SettingDeny)).NeedsApproval() {
		t.Error("enforce/deny is a hard deny, not an approval")
	}
}
