package handler

import (
	"encoding/json"
	"testing"
)

func TestValidateAnalyticsVisualAcceptsCanonicalQuery(t *testing.T) {
	req := analyticsVisualRequest{Name: "Cost by model", VisualType: "bars", Query: json.RawMessage(`{"population":"all","metrics":["cost_cents"],"dimensions":["model"],"page":{"limit":12}}`)}
	if err := validateAnalyticsVisual(req); err != nil {
		t.Fatal(err)
	}
}

func TestValidateAnalyticsVisualRejectsUnknownMetric(t *testing.T) {
	req := analyticsVisualRequest{Name: "Broken", VisualType: "table", Query: json.RawMessage(`{"population":"all","metrics":["made_up"]}`)}
	if err := validateAnalyticsVisual(req); err == nil {
		t.Fatal("validateAnalyticsVisual() error = nil")
	}
}
