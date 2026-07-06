package featureflag

import (
	"context"
	"os"
	"strconv"
	"strings"
)

// EnvProvider reads flag configuration from process environment variables.
// It is intended for emergency overrides, local development, and the kind
// of "kill switch I need to flip without redeploying" use case Ops Toggles
// were invented for.
//
// Variables are keyed by Prefix + UPPER_SNAKE_CASE(flag_key). For a Prefix
// of "FF_" and a flag named "checkout_new_payment_flow", the env variable
// is FF_CHECKOUT_NEW_PAYMENT_FLOW.
//
// Supported value formats (case-insensitive):
//
//	"true", "on", "1", "yes"      -> Enabled=true,  Variant="on"
//	"false", "off", "0", "no"     -> Enabled=false, Variant="off"
//	""                            -> Enabled=false, Variant="off"  (explicitly disabled)
//	"42%"                         -> deterministic percent rollout
//	any other non-empty value     -> treated as a variant identifier
//	                                 (Enabled=true, Variant=<raw>)
//
// Malformed percent values (negative, >100, non-numeric) yield a Decision
// with Reason=ReasonError. The Service still treats that as a real
// decision and does not fall through to a less specific provider; an Ops
// engineer who set FF_FOO=abc% expects to be told something is wrong, not
// for the override to silently disappear.
type EnvProvider struct {
	// Prefix is prepended to every lookup. Empty disables prefixing,
	// which is rarely what you want.
	Prefix string

	// lookup is overridable for tests. Must return (value, true) when
	// the variable is set (even to the empty string) and ("", false)
	// when it is missing. Defaults to os.LookupEnv.
	lookup func(string) (string, bool)
}

// NewEnvProvider returns an EnvProvider with the supplied prefix. Pass
// "FF_" for the conventional multica prefix.
func NewEnvProvider(prefix string) *EnvProvider {
	return &EnvProvider{Prefix: prefix, lookup: os.LookupEnv}
}

// Name implements Provider.
func (*EnvProvider) Name() string { return "env" }

// Lookup implements Provider.
func (p *EnvProvider) Lookup(ctx context.Context, key string) (Decision, bool) {
	envName := p.Prefix + flagKeyToEnv(key)
	get := p.lookup
	if get == nil {
		get = os.LookupEnv
	}
	raw, present := get(envName)
	if !present {
		return Decision{}, false
	}

	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return Decision{
			Key:     key,
			Enabled: false,
			Variant: "off",
			Reason:  ReasonStatic,
			Source:  "env",
		}, true
	}

	if strings.HasSuffix(trimmed, "%") {
		pctStr := strings.TrimSuffix(trimmed, "%")
		pct, err := strconv.Atoi(strings.TrimSpace(pctStr))
		if err != nil || pct < 0 || pct > 100 {
			return Decision{
				Key:     key,
				Enabled: false,
				Variant: "off",
				Reason:  ReasonError,
				Source:  "env",
			}, true
		}
		ec := EvalContextFrom(ctx)
		ident, _ := ec.Lookup("user_id")
		enabled := inPercent(key, ident, pct)
		return Decision{
			Key:     key,
			Enabled: enabled,
			Variant: boolToVariant(enabled),
			Reason:  ReasonPercent,
			Source:  "env",
		}, true
	}

	switch strings.ToLower(trimmed) {
	case "true", "on", "1", "yes":
		return Decision{
			Key:     key,
			Enabled: true,
			Variant: "on",
			Reason:  ReasonStatic,
			Source:  "env",
		}, true
	case "false", "off", "0", "no":
		return Decision{
			Key:     key,
			Enabled: false,
			Variant: "off",
			Reason:  ReasonStatic,
			Source:  "env",
		}, true
	}

	// Treat any other value as a variant identifier. We must not parse
	// the variant any further; callers know what their variants mean.
	return Decision{
		Key:     key,
		Enabled: true,
		Variant: trimmed,
		Reason:  ReasonStatic,
		Source:  "env",
	}, true
}

// flagKeyToEnv converts a flag key into its env-variable form. We
// uppercase everything and replace any non-alphanumeric run with a single
// underscore. The conversion is intentionally lossy (case-insensitive,
// merges punctuation runs) so common variants like "checkout.newPayment"
// and "checkout-new-payment" route to the same env name; if you need
// distinct env vars for variants of the same key, choose distinct flag
// keys instead.
func flagKeyToEnv(key string) string {
	var b strings.Builder
	b.Grow(len(key))
	prevUnderscore := false
	for _, r := range key {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevUnderscore = false
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - 32)
			prevUnderscore = false
		default:
			if !prevUnderscore {
				b.WriteByte('_')
				prevUnderscore = true
			}
		}
	}
	return strings.Trim(b.String(), "_")
}
