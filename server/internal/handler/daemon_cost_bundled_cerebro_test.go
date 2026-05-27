package handler

// CEREBRO-PATCH(daemon-bundled-saving): tests for bundled_read measurement params (FIR-2384).

import "testing"

// TestBundledReadMeasurementParams: "on" records an applied (real) save,
// "shadow" a would-save (not applied). Both arms share the same baseline (the
// separate reads the bundled call replaces) and effective=1 (the one call) so
// the dashboard can compare them directly.
func TestBundledReadMeasurementParams(t *testing.T) {
	t.Parallel()

	ws := parseUUID("44444444-4444-4444-4444-444444444444")
	task := parseUUID("55555555-5555-5555-5555-555555555555")

	on := bundledReadMeasurementParams(ws, task, costSavingModeOn)
	if !on.Applied || on.HeldOut {
		t.Fatalf("on mode must be applied and not held out, got applied=%v held=%v", on.Applied, on.HeldOut)
	}
	if on.SavingKey != costSavingBundledKey || on.Metric != costMetricPlatformCalls {
		t.Fatalf("unexpected key/metric: %+v", on)
	}
	if on.BaselineValue != bundledReadBaseline || on.EffectiveValue != 1 {
		t.Fatalf("expected baseline=%d effective=1, got baseline=%d effective=%d", bundledReadBaseline, on.BaselineValue, on.EffectiveValue)
	}

	shadow := bundledReadMeasurementParams(ws, task, costSavingModeShadow)
	if shadow.Applied {
		t.Fatalf("shadow mode must not be applied, got %+v", shadow)
	}
	if shadow.BaselineValue != bundledReadBaseline || shadow.EffectiveValue != 1 {
		t.Fatalf("shadow would-save must share baseline=%d effective=1, got baseline=%d effective=%d", bundledReadBaseline, shadow.BaselineValue, shadow.EffectiveValue)
	}
}
