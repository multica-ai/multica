package service

import "testing"

func TestParseResponseEngineMode(t *testing.T) {
	tests := []struct {
		name    string
		enabled string
		enforce string
		want    ResponseEngineMode
		wantErr bool
	}{
		{name: "disabled", want: ResponseEngineLegacy},
		{name: "observe", enabled: "true", want: ResponseEngineObserve},
		{name: "enforce", enabled: "1", enforce: "yes", want: ResponseEngineEnforce},
		{name: "enforce requires enabled", enforce: "true", wantErr: true},
		{name: "invalid enabled", enabled: "maybe", wantErr: true},
		{name: "invalid enforce", enabled: "true", enforce: "2", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseResponseEngineMode(tc.enabled, tc.enforce)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseResponseEngineMode(%q,%q) expected error", tc.enabled, tc.enforce)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseResponseEngineMode(%q,%q): %v", tc.enabled, tc.enforce, err)
			}
			if got != tc.want {
				t.Fatalf("mode = %q, want %q", got, tc.want)
			}
		})
	}
}
