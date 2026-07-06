package featureflag

import (
	"context"
	"testing"
)

func newMockEnv(env map[string]string) *EnvProvider {
	p := NewEnvProvider("FF_")
	p.lookup = func(name string) (string, bool) {
		v, ok := env[name]
		return v, ok
	}
	return p
}

func TestEnvProviderTrueFalse(t *testing.T) {
	t.Parallel()
	cases := []struct {
		raw     string
		want    bool
		variant string
	}{
		{"true", true, "on"},
		{"TRUE", true, "on"},
		{"on", true, "on"},
		{"1", true, "on"},
		{"yes", true, "on"},
		{"false", false, "off"},
		{"OFF", false, "off"},
		{"0", false, "off"},
		{"no", false, "off"},
	}
	for _, tc := range cases {
		p := newMockEnv(map[string]string{"FF_DEMO": tc.raw})
		d, ok := p.Lookup(context.Background(), "demo")
		if !ok {
			t.Fatalf("%q: env provider must report found", tc.raw)
		}
		if d.Enabled != tc.want || d.Variant != tc.variant {
			t.Fatalf("%q: got %+v, want enabled=%v variant=%q", tc.raw, d, tc.want, tc.variant)
		}
	}
}

func TestEnvProviderExplicitEmpty(t *testing.T) {
	t.Parallel()
	// An explicitly empty variable means "I want this flag off". This is
	// the contract for kill switches set via ConfigMap.
	p := newMockEnv(map[string]string{"FF_DEMO": ""})
	d, ok := p.Lookup(context.Background(), "demo")
	if !ok {
		t.Fatalf("empty env value must be treated as 'set'")
	}
	if d.Enabled {
		t.Fatalf("empty env value must disable the flag, got %+v", d)
	}
}

func TestEnvProviderMissingFallsThrough(t *testing.T) {
	t.Parallel()
	p := newMockEnv(map[string]string{})
	_, ok := p.Lookup(context.Background(), "demo")
	if ok {
		t.Fatalf("missing env var must report not-found so callers can fall through")
	}
}

func TestEnvProviderPercent(t *testing.T) {
	t.Parallel()
	p := newMockEnv(map[string]string{"FF_DEMO": "100%"})
	ctx := WithEvalContext(context.Background(), EvalContext{UserID: "anyone"})
	d, ok := p.Lookup(ctx, "demo")
	if !ok || !d.Enabled || d.Reason != ReasonPercent {
		t.Fatalf("100%% must enable everyone with ReasonPercent, got %+v", d)
	}

	p = newMockEnv(map[string]string{"FF_DEMO": "0%"})
	d, _ = p.Lookup(ctx, "demo")
	if d.Enabled {
		t.Fatalf("0%% must disable everyone")
	}
}

func TestEnvProviderMalformedPercent(t *testing.T) {
	t.Parallel()
	p := newMockEnv(map[string]string{"FF_DEMO": "abc%"})
	d, ok := p.Lookup(context.Background(), "demo")
	if !ok {
		t.Fatalf("malformed percent must still return a decision so it does not fall through")
	}
	if d.Reason != ReasonError {
		t.Fatalf("malformed percent must report ReasonError, got %+v", d)
	}
	if d.Enabled {
		t.Fatalf("malformed percent must default to disabled, got %+v", d)
	}
}

func TestEnvProviderOutOfRangePercent(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"-5%", "150%"} {
		p := newMockEnv(map[string]string{"FF_DEMO": raw})
		d, _ := p.Lookup(context.Background(), "demo")
		if d.Reason != ReasonError {
			t.Fatalf("%q: out-of-range percent must report ReasonError, got %+v", raw, d)
		}
	}
}

func TestEnvProviderVariantValue(t *testing.T) {
	t.Parallel()
	p := newMockEnv(map[string]string{"FF_ALGO": "experiment-v2"})
	d, ok := p.Lookup(context.Background(), "algo")
	if !ok || !d.Enabled || d.Variant != "experiment-v2" {
		t.Fatalf("variant value must be passed through verbatim, got %+v", d)
	}
}

func TestFlagKeyToEnv(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want string
	}{
		{"checkout_new_payment_flow", "CHECKOUT_NEW_PAYMENT_FLOW"},
		{"checkout.newPayment", "CHECKOUT_NEWPAYMENT"},
		{"checkout-new-payment", "CHECKOUT_NEW_PAYMENT"},
		{"  weird  spaces  ", "WEIRD_SPACES"},
		{"a..b", "A_B"},
	}
	for _, tc := range cases {
		if got := flagKeyToEnv(tc.in); got != tc.want {
			t.Fatalf("flagKeyToEnv(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
