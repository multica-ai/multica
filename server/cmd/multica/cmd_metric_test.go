package main

import "testing"

// TestParseMetricScopeReadsEachScopeAndRefusesTheRest covers the --scope forms of
// spec 4.1. The server checks the id; the parser checks the shape.
func TestParseMetricScopeReadsEachScopeAndRefusesTheRest(t *testing.T) {
	cases := []struct {
		in, wantType, wantID string
		wantErr              bool
	}{
		{"", "company", "", false},
		{"company", "company", "", false},
		{"initiative:abc-123", "initiative", "abc-123", false},
		{"squad: 9f2 ", "squad", "9f2", false},
		{"floor:f1", "floor", "f1", false},
		{"initiative", "", "", true},
		{"initiative:", "", "", true},
		{"team:x", "", "", true},
		{"company:x", "", "", true},
	}
	for _, c := range cases {
		typ, id, err := parseMetricScope(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("%q: err %v, want error %v", c.in, err, c.wantErr)
			continue
		}
		if typ != c.wantType || id != c.wantID {
			t.Errorf("%q: got %s %q, want %s %q", c.in, typ, id, c.wantType, c.wantID)
		}
	}
}
