package toolpolicy

import (
	"errors"
	"testing"
)

func TestCondition_ZeroAlwaysMatches(t *testing.T) {
	var c Condition
	if !c.IsZero() {
		t.Fatal("zero Condition should report IsZero")
	}
	ok, err := c.Matches(RequestContext{Host: "anything", Action: "push"}, nil)
	if err != nil || !ok {
		t.Fatalf("zero condition must match without error, got ok=%v err=%v", ok, err)
	}
}

func TestCondition_HostAllowlist(t *testing.T) {
	c := Condition{HostAllowlist: []string{"api.dixa.io", "*.coolrunner.dk"}}
	cases := []struct {
		host string
		want bool
	}{
		{"api.dixa.io", true},
		{"API.DIXA.IO", true},         // case-insensitive
		{"track.coolrunner.dk", true}, // wildcard subdomain
		{"coolrunner.dk", true},       // wildcard bare suffix
		{"evil.com", false},
		{"dixa.io", false},          // not the exact host
		{"notcoolrunner.dk", false}, // suffix must be on a dot boundary
	}
	for _, tc := range cases {
		ok, err := c.Matches(RequestContext{Host: tc.host}, nil)
		if err != nil {
			t.Fatalf("host %q: unexpected error %v", tc.host, err)
		}
		if ok != tc.want {
			t.Fatalf("host %q: got %v, want %v", tc.host, ok, tc.want)
		}
	}
}

func TestCondition_Actions(t *testing.T) {
	c := Condition{Actions: []string{"read", "checkout"}}
	if ok, _ := c.Matches(RequestContext{Action: "checkout"}, nil); !ok {
		t.Fatal("checkout should match")
	}
	if ok, _ := c.Matches(RequestContext{Action: "push"}, nil); ok {
		t.Fatal("push should not match")
	}
}

func TestCondition_ExprWithoutEvaluatorErrors(t *testing.T) {
	c := Condition{Expr: "ctx.host == 'x'"}
	ok, err := c.Matches(RequestContext{}, nil)
	if err == nil {
		t.Fatal("expr without evaluator must return an error, not a silent match")
	}
	if ok {
		t.Fatal("expr without evaluator must not report a match")
	}
}

func TestCondition_ExprUsesEvaluator(t *testing.T) {
	called := false
	eval := func(expr string, ctx RequestContext) (bool, error) {
		called = true
		if expr != "biz_hours" {
			t.Fatalf("evaluator got unexpected expr %q", expr)
		}
		return ctx.Action == "reveal", nil
	}
	c := Condition{Expr: "biz_hours"}
	if ok, _ := c.Matches(RequestContext{Action: "reveal"}, eval); !ok {
		t.Fatal("evaluator returning true should match")
	}
	if ok, _ := c.Matches(RequestContext{Action: "rotate"}, eval); ok {
		t.Fatal("evaluator returning false should not match")
	}
	if !called {
		t.Fatal("evaluator was never called")
	}
}

func TestCondition_ExprEvaluatorErrorPropagates(t *testing.T) {
	boom := errors.New("boom")
	eval := func(string, RequestContext) (bool, error) { return false, boom }
	c := Condition{Expr: "x"}
	if _, err := c.Matches(RequestContext{}, eval); !errors.Is(err, boom) {
		t.Fatal("evaluator error must propagate to caller")
	}
}

// Structured terms AND together: every present term must match.
func TestCondition_TermsAndTogether(t *testing.T) {
	c := Condition{HostAllowlist: []string{"api.dixa.io"}, Actions: []string{"read"}}
	if ok, _ := c.Matches(RequestContext{Host: "api.dixa.io", Action: "read"}, nil); !ok {
		t.Fatal("both terms satisfied should match")
	}
	if ok, _ := c.Matches(RequestContext{Host: "api.dixa.io", Action: "push"}, nil); ok {
		t.Fatal("action term unmet should not match even if host matches")
	}
}

// ConditionedSetting is the seam that drops a row from resolution when its WHEN
// layer is not satisfied. These cover the four cases its contract defines.
func TestConditionedSetting_NilConditionApplies(t *testing.T) {
	got, applies := ConditionedSetting(SettingDeny, nil, RequestContext{}, nil)
	if !applies || got != SettingDeny {
		t.Fatalf("nil condition must apply unchanged; got (%q, %v)", got, applies)
	}
}

func TestConditionedSetting_MetApplies(t *testing.T) {
	c := &Condition{Actions: []string{"push"}}
	got, applies := ConditionedSetting(SettingDeny, c, RequestContext{Action: "push"}, nil)
	if !applies || got != SettingDeny {
		t.Fatalf("met condition must apply unchanged; got (%q, %v)", got, applies)
	}
}

func TestConditionedSetting_UnmetDropsRow(t *testing.T) {
	c := &Condition{Actions: []string{"push"}}
	// A clean non-match drops the row regardless of effect — the row simply does
	// not scope this request, so the layer inherits.
	if _, applies := ConditionedSetting(SettingDeny, c, RequestContext{Action: "read"}, nil); applies {
		t.Fatal("unmet Deny condition must drop the row, not keep the Deny")
	}
	if _, applies := ConditionedSetting(SettingAllow, c, RequestContext{Action: "read"}, nil); applies {
		t.Fatal("unmet Allow condition must drop the row")
	}
}

func TestConditionedSetting_UndecidableFailsClosedByEffect(t *testing.T) {
	// A non-empty Expr with no evaluator is undecidable: drop an Allow (never
	// grant on uncertainty), keep a Deny/Ask (stay restrictive on uncertainty).
	expr := &Condition{Expr: "request.now < opening_hours"}
	if _, applies := ConditionedSetting(SettingAllow, expr, RequestContext{}, nil); applies {
		t.Fatal("undecidable Allow must fail closed (drop)")
	}
	if got, applies := ConditionedSetting(SettingDeny, expr, RequestContext{}, nil); !applies || got != SettingDeny {
		t.Fatalf("undecidable Deny must fail closed (keep); got (%q, %v)", got, applies)
	}
	if got, applies := ConditionedSetting(SettingAsk, expr, RequestContext{}, nil); !applies || got != SettingAsk {
		t.Fatalf("undecidable Ask must fail closed (keep); got (%q, %v)", got, applies)
	}
}

// HostOf is the gate-side adapter from a tool call's resource string to the Host
// the WHEN layer matches on. It must agree with hostMatches' notion of a host.
func TestHostOf(t *testing.T) {
	cases := map[string]string{
		"https://api.dixa.io/v1/conversations": "api.dixa.io",
		"http://API.Dixa.io":                   "api.dixa.io",
		"https://api.coolrunner.dk:443/x":      "api.coolrunner.dk",
		"api.dixa.io":                          "api.dixa.io",
		"api.dixa.io/path":                     "api.dixa.io",
		"api.dixa.io:8443":                     "api.dixa.io",
		"  api.dixa.io  ":                      "api.dixa.io",
		"":                                     "",
	}
	for in, want := range cases {
		if got := HostOf(in); got != want {
			t.Errorf("HostOf(%q) = %q, want %q", in, got, want)
		}
	}
}

// ActionOf is the gate-side adapter from a tool/capability key to the action verb
// an action-scoped Condition matches on. It must agree with containsFold's notion
// of a verb (lower-cased, trimmed).
func TestActionOf(t *testing.T) {
	cases := map[string]string{
		"repo.read":         "read",
		"repo.checkout":     "checkout",
		"repo.push":         "push",
		"credential.reveal": "reveal",
		"credential.rotate": "rotate",
		"credential.use":    "use",
		"REPO.CHECKOUT":     "checkout", // case-folded
		"  repo.push  ":     "push",     // trimmed
		"tools:Bash":        "",         // bare Claude tool, colon-namespaced
		"mcp__dixa__send":   "",         // MCP tool
		"connection:repo.x": "",         // colon namespace that contains a dot
		"issue.read":        "",         // unverbed namespace
		"repo":              "",         // no verb segment
		"repo.":             "",         // empty verb
		"":                  "",
	}
	for in, want := range cases {
		if got := ActionOf(in); got != want {
			t.Errorf("ActionOf(%q) = %q, want %q", in, got, want)
		}
	}
}

// ActionOf must feed an Action an action-scoped Condition then accepts/rejects,
// end to end through the same path a gate uses.
func TestActionOf_FeedsActionsTerm(t *testing.T) {
	c := Condition{Actions: []string{"checkout", "read"}}
	if ok, _ := c.Matches(RequestContext{Action: ActionOf("repo.checkout")}, nil); !ok {
		t.Fatal("repo.checkout should match an Actions term that lists checkout")
	}
	if ok, _ := c.Matches(RequestContext{Action: ActionOf("repo.push")}, nil); ok {
		t.Fatal("repo.push must not match an Actions term that omits push")
	}
	// A key with no derivable verb yields Action "" — an Actions-scoped rule then
	// simply does not bite (containsFold of "" against a non-empty list is false).
	if ok, _ := c.Matches(RequestContext{Action: ActionOf("tools:Bash")}, nil); ok {
		t.Fatal("a verbless key must not satisfy an Actions term")
	}
}

// HostOf must feed a Host a host-allowlist Condition then accepts/rejects.
func TestHostOf_FeedsHostAllowlist(t *testing.T) {
	c := Condition{HostAllowlist: []string{"*.coolrunner.dk"}}
	if ok, _ := c.Matches(RequestContext{Host: HostOf("https://api.coolrunner.dk/parcels")}, nil); !ok {
		t.Fatal("subdomain of an allowlisted suffix should match end to end")
	}
	if ok, _ := c.Matches(RequestContext{Host: HostOf("https://evil.example.com")}, nil); ok {
		t.Fatal("host outside the allowlist must not match")
	}
}
