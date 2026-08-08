package analytics

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseAcquisitionAttributionMinimizesAndSanitizes(t *testing.T) {
	raw := `{"utm_source":"GitHub","utm_medium":"Community Post","utm_campaign":"Launch 2026!","utm_content":"private-copy","utm_term":"secret","referrer_origin":"https://news.ycombinator.com/private?token=secret"}`
	got := ParseAcquisitionAttribution(raw)
	if got == nil {
		t.Fatal("expected attribution")
	}

	var values map[string]string
	if err := json.Unmarshal(got, &values); err != nil {
		t.Fatalf("unmarshal attribution: %v", err)
	}
	want := map[string]string{
		"source":        "github",
		"medium":        "community_post",
		"campaign":      "launch_2026",
		"referrer_host": "news.ycombinator.com",
	}
	if len(values) != len(want) {
		t.Fatalf("attribution = %#v, want only %#v", values, want)
	}
	for key, expected := range want {
		if values[key] != expected {
			t.Errorf("%s = %q, want %q", key, values[key], expected)
		}
	}
}

func TestParseAcquisitionAttributionRejectsInvalidAndBoundsValues(t *testing.T) {
	if got := ParseAcquisitionAttribution(`not-json`); got != nil {
		t.Fatalf("invalid input = %q, want nil", got)
	}

	got := ParseAcquisitionAttribution(`{"utm_campaign":"` + strings.Repeat("a", 200) + `"}`)
	var values map[string]string
	if err := json.Unmarshal(got, &values); err != nil {
		t.Fatalf("unmarshal attribution: %v", err)
	}
	if len(values["campaign"]) != acquisitionDimensionMaxLength {
		t.Fatalf("campaign length = %d, want %d", len(values["campaign"]), acquisitionDimensionMaxLength)
	}
}

func TestParseAcquisitionAttributionUsesExplicitFallbackBuckets(t *testing.T) {
	got := ParseAcquisitionAttribution(`{"referrer_origin":"https://github.com/sensitive/path"}`)
	var values map[string]string
	if err := json.Unmarshal(got, &values); err != nil {
		t.Fatalf("unmarshal attribution: %v", err)
	}
	if values["source"] != "github.com" || values["medium"] != "none" || values["campaign"] != "none" {
		t.Fatalf("fallback attribution = %#v", values)
	}
}

func TestParseAcquisitionAttributionRejectsURLAndEmailDimensions(t *testing.T) {
	got := ParseAcquisitionAttribution(`{"utm_source":"https://example.com/path","utm_campaign":"person@example.com","referrer_origin":"https://192.0.2.10/private"}`)
	var values map[string]string
	if err := json.Unmarshal(got, &values); err != nil {
		t.Fatalf("unmarshal attribution: %v", err)
	}
	if values["source"] != "direct" || values["campaign"] != "none" {
		t.Fatalf("rejected attribution = %#v", values)
	}
}
