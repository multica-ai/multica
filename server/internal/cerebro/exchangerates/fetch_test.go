package exchangerates

import (
	"testing"
)

func TestParseSnapshot(t *testing.T) {
	body := []byte(`{"amount":1.0,"base":"USD","date":"2026-06-04","rates":{"DKK":6.8123,"EUR":0.9211}}`)
	snap, err := ParseSnapshot(body)
	if err != nil {
		t.Fatalf("ParseSnapshot returned error: %v", err)
	}
	if snap.Date != "2026-06-04" {
		t.Fatalf("date = %q, want 2026-06-04", snap.Date)
	}
	if got := snap.Rates["DKK"]; got != 6.8123 {
		t.Fatalf("DKK rate = %v, want 6.8123", got)
	}
	if got := snap.Rates["EUR"]; got != 0.9211 {
		t.Fatalf("EUR rate = %v, want 0.9211", got)
	}
}

func TestParseSnapshotRejectsEmpty(t *testing.T) {
	for _, body := range []string{
		`{"base":"USD","date":"2026-06-04","rates":{}}`,
		`{"base":"USD","date":"","rates":{"DKK":6.8}}`,
		`not json`,
	} {
		if _, err := ParseSnapshot([]byte(body)); err == nil {
			t.Fatalf("ParseSnapshot(%q) = nil error, want error", body)
		}
	}
}

func TestToNumericPreservesPrecision(t *testing.T) {
	n, err := toNumeric(6.81234567)
	if err != nil {
		t.Fatalf("toNumeric error: %v", err)
	}
	f, err := n.Float64Value()
	if err != nil {
		t.Fatalf("Float64Value error: %v", err)
	}
	if !f.Valid {
		t.Fatal("numeric not valid")
	}
	// 8-decimal round-trip should be exact to within a tiny epsilon.
	if diff := f.Float64 - 6.81234567; diff > 1e-8 || diff < -1e-8 {
		t.Fatalf("round-trip drift too large: got %v", f.Float64)
	}
}
