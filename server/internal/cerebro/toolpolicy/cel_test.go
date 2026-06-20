package toolpolicy

import (
	"strings"
	"testing"
)

func newEval(t *testing.T) *CELEvaluator {
	t.Helper()
	e, err := NewCELEvaluator()
	if err != nil {
		t.Fatalf("NewCELEvaluator: %v", err)
	}
	return e
}

func TestCEL_StructuredAttributes(t *testing.T) {
	e := newEval(t)
	cases := []struct {
		expr string
		ctx  RequestContext
		want bool
	}{
		{`action == "checkout"`, RequestContext{Action: "checkout"}, true},
		{`action == "checkout"`, RequestContext{Action: "push"}, false},
		{`host.endsWith("dixa.io")`, RequestContext{Host: "api.dixa.io"}, true},
		{`host.endsWith("dixa.io")`, RequestContext{Host: "evil.com"}, false},
		{`action == "push" && host == "github.com"`, RequestContext{Action: "push", Host: "github.com"}, true},
		{`action in ["read", "checkout"]`, RequestContext{Action: "read"}, true},
		{`action in ["read", "checkout"]`, RequestContext{Action: "push"}, false},
	}
	for _, tc := range cases {
		got, err := e.Eval(tc.expr, tc.ctx)
		if err != nil {
			t.Fatalf("Eval(%q): unexpected error %v", tc.expr, err)
		}
		if got != tc.want {
			t.Errorf("Eval(%q, %+v) = %v, want %v", tc.expr, tc.ctx, got, tc.want)
		}
	}
}

func TestCEL_CompileErrorIsReturnedNotPanicked(t *testing.T) {
	e := newEval(t)
	// Unknown variable — CEL rejects it at compile time.
	if _, err := e.Eval(`unknown_var == "x"`, RequestContext{}); err == nil {
		t.Fatal("an expr referencing an undeclared variable must return a compile error")
	}
	// Syntactically broken.
	if _, err := e.Eval(`action ==`, RequestContext{}); err == nil {
		t.Fatal("a syntactically invalid expr must return a compile error")
	}
}

func TestCEL_NonBoolResultRejected(t *testing.T) {
	e := newEval(t)
	// A string-typed expression is a config error: a condition must be a predicate.
	if _, err := e.Eval(`host`, RequestContext{Host: "api.dixa.io"}); err == nil {
		t.Fatal("a non-bool expr must be rejected")
	} else if !strings.Contains(err.Error(), "bool") {
		t.Fatalf("error should mention the bool requirement, got %v", err)
	}
}

func TestCEL_CompileErrorIsCached(t *testing.T) {
	e := newEval(t)
	const bad = `nope ==`
	if _, err := e.Eval(bad, RequestContext{}); err == nil {
		t.Fatal("expected compile error")
	}
	e.mu.RLock()
	_, cached := e.errs[bad]
	e.mu.RUnlock()
	if !cached {
		t.Fatal("a compile error must be cached so a bad rule is not recompiled every call")
	}
}

func TestCEL_ProgramIsCachedOnSuccess(t *testing.T) {
	e := newEval(t)
	const expr = `action == "checkout"`
	if _, err := e.Eval(expr, RequestContext{Action: "checkout"}); err != nil {
		t.Fatalf("Eval: %v", err)
	}
	e.mu.RLock()
	_, cached := e.progs[expr]
	e.mu.RUnlock()
	if !cached {
		t.Fatal("a successfully compiled program must be cached")
	}
}

// The evaluator must drop cleanly into the ExprEvaluator seam the resolver uses:
// a whitelist Allow with an unmet Expr CLOSES (Deny), a met Expr applies.
func TestCEL_FeedsConditionedSetting(t *testing.T) {
	e := newEval(t)
	cond := &Condition{Expr: `action == "checkout"`}

	got, applies := ConditionedSetting(SettingAllow, cond, RequestContext{Action: "push"}, e.Eval)
	if !applies || got != SettingDeny {
		t.Fatalf("an Allow whose Expr does not hold must close (deny); got (%q, %v)", got, applies)
	}
	got, applies = ConditionedSetting(SettingAllow, cond, RequestContext{Action: "checkout"}, e.Eval)
	if !applies || got != SettingAllow {
		t.Fatalf("an Allow whose Expr holds must apply unchanged; got (%q, %v)", got, applies)
	}

	// A compile error is undecidable → fail closed by effect: Allow denies, Deny stays.
	broken := &Condition{Expr: `bogus ==`}
	if got, applies := ConditionedSetting(SettingAllow, broken, RequestContext{}, e.Eval); !applies || got != SettingDeny {
		t.Fatalf("an undecidable Allow (compile error) must fail closed (deny); got (%q, %v)", got, applies)
	}
	if got, applies := ConditionedSetting(SettingDeny, broken, RequestContext{}, e.Eval); !applies || got != SettingDeny {
		t.Fatalf("an undecidable Deny (compile error) must fail closed and keep the Deny; got (%q, %v)", got, applies)
	}
}
