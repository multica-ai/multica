package piharnesseval

import "testing"

func TestScoreRequiresEveryDeliveryAndEveryCheck(t *testing.T) {
	results := []DeliveryResult{
		{ID: "D0", Checks: []CheckResult{{Passed: true}}},
		{ID: "D1", Checks: []CheckResult{{Passed: true}, {Passed: true}}},
		{ID: "D2", Checks: []CheckResult{{Passed: false}}},
	}
	report := Score([]string{"D0", "D1", "D2"}, results)
	if report.Passed {
		t.Fatal("report passed with a failing delivery")
	}
	if report.PassedDeliveries != 2 || report.RequiredDeliveries != 3 {
		t.Fatalf("unexpected score: %#v", report)
	}
	results[2].Checks[0].Passed = true
	report = Score([]string{"D0", "D1", "D2"}, results)
	if !report.Passed {
		t.Fatalf("fully green report failed: %#v", report)
	}
	for _, result := range report.Results {
		if !result.Passed {
			t.Fatalf("green delivery was not marked passed: %#v", result)
		}
	}
}

func TestScoreFailsWhenARequiredDeliveryIsMissing(t *testing.T) {
	report := Score([]string{"D0", "D1"}, []DeliveryResult{{ID: "D0", Checks: []CheckResult{{Passed: true}}}})
	if report.Passed {
		t.Fatal("report passed without D1 evidence")
	}
}
