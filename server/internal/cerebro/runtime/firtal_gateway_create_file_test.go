package runtime

import "testing"

func TestEnsureExt(t *testing.T) {
	cases := []struct {
		name, ext, want string
	}{
		{"rapport", "csv", "rapport.csv"}, // no extension → append
		{"tal.txt", "csv", "tal.txt.csv"}, // wrong extension → append correct one
		{"data.csv", "csv", "data.csv"},   // already correct → unchanged
		{"DATA.CSV", "csv", "DATA.CSV"},   // case-insensitive match → unchanged
		{"notes", "", "notes"},            // empty ext → unchanged
		{"a.b.json", "json", "a.b.json"},  // multi-dot, correct ext → unchanged
	}
	for _, tc := range cases {
		if got := ensureExt(tc.name, tc.ext); got != tc.want {
			t.Errorf("ensureExt(%q,%q) = %q, want %q", tc.name, tc.ext, got, tc.want)
		}
	}
}

func TestCoerceRows(t *testing.T) {
	rows, err := coerceRows([]any{
		[]any{"navn", "antal"},
		[]any{"æbler", float64(3)},
		[]any{"sandt", true, nil},
	})
	if err != nil {
		t.Fatalf("coerceRows: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	if rows[1][1] != "3" {
		t.Errorf("number cell = %q, want \"3\" (no trailing .0)", rows[1][1])
	}
	if rows[2][1] != "true" || rows[2][2] != "" {
		t.Errorf("bool/nil cells = %q,%q, want \"true\",\"\"", rows[2][1], rows[2][2])
	}
}

func TestCoerceRows_Rejects(t *testing.T) {
	if _, err := coerceRows("not an array"); err == nil {
		t.Error("expected error for non-array rows")
	}
	if _, err := coerceRows([]any{"row-not-array"}); err == nil {
		t.Error("expected error for non-array row")
	}
}
