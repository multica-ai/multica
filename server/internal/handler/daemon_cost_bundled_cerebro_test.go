package handler

// CEREBRO-PATCH(daemon-bundled-saving): tests for bundled_read measurement params (FIR-2384).

import "testing"

// TestBundledReadMeasurementParams: "on" records an applied (real) save,
// "shadow" a would-save (not applied). Both arms share the same baseline (the
// separate reads the bundled call replaces) and effective=1 (the one call) so
// the dashboard can compare them directly.
func TestBundledReadMeasurementParams(t *testing.T) {
	t.Parallel()

	// CEREBRO-PATCH(cost-optimization-token-metrics): bundled_read records token deltas.
	ws := parseUUID("44444444-4444-4444-4444-444444444444")
	task := parseUUID("55555555-5555-5555-5555-555555555555")

	on := bundledReadMeasurementParams(ws, task, costSavingModeOn)
	if !on.Applied || on.HeldOut {
		t.Fatalf("on mode must be applied and not held out, got applied=%v held=%v", on.Applied, on.HeldOut)
	}
	if on.SavingKey != costSavingBundledKey || on.Metric != costMetricInputTokens {
		t.Fatalf("unexpected key/metric: %+v", on)
	}
	wantBaseline := int64(bundledReadBaseline) * estimatedTokensPerSeparateRead
	wantEffective := int64(estimatedTokensPerBundledCall)
	if on.BaselineValue != wantBaseline || on.EffectiveValue != wantEffective {
		t.Fatalf("expected baseline=%d effective=%d, got baseline=%d effective=%d", wantBaseline, wantEffective, on.BaselineValue, on.EffectiveValue)
	}

	shadow := bundledReadMeasurementParams(ws, task, costSavingModeShadow)
	if shadow.Applied {
		t.Fatalf("shadow mode must not be applied, got %+v", shadow)
	}
	if shadow.BaselineValue != wantBaseline || shadow.EffectiveValue != wantEffective {
		t.Fatalf("shadow would-save must share token baseline/effective, got baseline=%d effective=%d", shadow.BaselineValue, shadow.EffectiveValue)
	}
}

// TestShouldCreditBundledReadUsage: a real `issue context` call earns a
// bundled_read row only when bundled_read is "on" AND snapshot_prompt is not
// "on". Snapshot precedence (no double-count) must hold even if the agent calls
// the endpoint during a snapshot run.
func TestShouldCreditBundledReadUsage(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		bundled    string
		snapshot   string
		wantCredit bool
	}{
		{"bundled on, snapshot off", costSavingModeOn, "", true},
		{"bundled on, snapshot shadow", costSavingModeOn, costSavingModeShadow, true},
		{"bundled on, snapshot on -> snapshot wins, no double-count", costSavingModeOn, costSavingModeOn, false},
		{"bundled shadow -> claim-time records, endpoint does not", costSavingModeShadow, "", false},
		{"bundled off", "", "", false},
		{"bundled off, snapshot on", "", costSavingModeOn, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldCreditBundledReadUsage(tc.bundled, tc.snapshot); got != tc.wantCredit {
				t.Fatalf("shouldCreditBundledReadUsage(%q, %q) = %v, want %v", tc.bundled, tc.snapshot, got, tc.wantCredit)
			}
		})
	}
}
