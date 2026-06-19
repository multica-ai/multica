package issuedatetime

import "testing"

func ptr(s string) *string { return &s }

func TestParseTime(t *testing.T) {
	tests := []struct {
		name      string
		in        *string
		wantValid bool
		wantMicro int64
		wantErr   bool
	}{
		{"nil clears", nil, false, 0, false},
		{"empty clears", ptr(""), false, 0, false},
		{"whitespace clears", ptr("   "), false, 0, false},
		{"midnight", ptr("00:00"), true, 0, false},
		{"morning", ptr("08:00"), true, 8 * 3600 * microsPerSecond, false},
		{"afternoon", ptr("16:30"), true, (16*3600 + 30*60) * microsPerSecond, false},
		{"with seconds", ptr("16:30:45"), true, (16*3600 + 30*60 + 45) * microsPerSecond, false},
		{"end of day", ptr("23:59"), true, (23*3600 + 59*60) * microsPerSecond, false},
		{"hour out of range", ptr("24:00"), false, 0, true},
		{"minute out of range", ptr("10:60"), false, 0, true},
		{"garbage", ptr("not-a-time"), false, 0, true},
		{"missing minute", ptr("10"), false, 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseTime(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got none (value=%+v)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Valid != tc.wantValid {
				t.Fatalf("valid = %v, want %v", got.Valid, tc.wantValid)
			}
			if got.Valid && got.Microseconds != tc.wantMicro {
				t.Fatalf("micros = %d, want %d", got.Microseconds, tc.wantMicro)
			}
		})
	}
}

func TestTimeToStringRoundTrip(t *testing.T) {
	for _, in := range []string{"00:00", "08:00", "16:30", "23:59"} {
		pt, err := parseTime(&in)
		if err != nil {
			t.Fatalf("parse %q: %v", in, err)
		}
		out := timeToString(pt)
		if out == nil {
			t.Fatalf("round-trip %q -> nil", in)
		}
		if *out != in {
			t.Fatalf("round-trip %q -> %q", in, *out)
		}
	}
	cleared, err := parseTime(nil)
	if err != nil {
		t.Fatalf("parseTime(nil): %v", err)
	}
	if timeToString(cleared) != nil {
		t.Fatalf("nil time should render nil")
	}
}
