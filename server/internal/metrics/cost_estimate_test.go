package metrics

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

// costEstimateCases is the shared fixture (testdata/cost_estimate_cases.json) that
// the TypeScript Analytics estimate is also checked against.
type costEstimateCases struct {
	Cases []struct {
		Name        string   `json:"name"`
		Provider    string   `json:"provider"`
		Model       string   `json:"model"`
		Input       int64    `json:"input_tokens"`
		Output      int64    `json:"output_tokens"`
		CacheRead   int64    `json:"cache_read_tokens"`
		CacheWrite  int64    `json:"cache_write_tokens"`
		ExpectedUSD *float64 `json:"expected_usd"`
	} `json:"cases"`
}

// TestCostEstimateMatchesTheSharedFixture checks the server's estimate against the
// same expected figures the TypeScript Analytics test uses, so the budgets (4.2)
// and the Analytics page agree.
func TestCostEstimateMatchesTheSharedFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/cost_estimate_cases.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var fixture costEstimateCases
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if len(fixture.Cases) == 0 {
		t.Fatal("fixture has no cases")
	}
	for _, c := range fixture.Cases {
		got, ok := EstimateCostUSD(c.Provider, c.Model, c.Input, c.Output, c.CacheRead, c.CacheWrite)
		if c.ExpectedUSD == nil {
			if ok {
				t.Errorf("%s: estimate %v for a model with no rate row", c.Name, got)
			}
			continue
		}
		if !ok {
			t.Errorf("%s: no rate row for %s", c.Name, c.Model)
			continue
		}
		if math.Abs(got-*c.ExpectedUSD) > 1e-9 {
			t.Errorf("%s: estimate = %.10f, want %.10f", c.Name, got, *c.ExpectedUSD)
		}
	}
}
