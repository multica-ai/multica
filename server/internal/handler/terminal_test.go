package handler

import "testing"

func TestBindTerminalDaemonID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		current   string
		runtimeID string
		valid     bool
		want      string
		wantOK    bool
	}{
		{name: "derive from user PAT authorized runtime", runtimeID: "daemon-a", valid: true, want: "daemon-a", wantOK: true},
		{name: "daemon token matches runtime", current: "daemon-a", runtimeID: "daemon-a", valid: true, want: "daemon-a", wantOK: true},
		{name: "mixed daemon runtime set rejected", current: "daemon-a", runtimeID: "daemon-b", valid: true, want: "daemon-a", wantOK: false},
		{name: "legacy runtime without daemon rejected", runtimeID: "", valid: false, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := bindTerminalDaemonID(tt.current, tt.runtimeID, tt.valid)
			if got != tt.want || ok != tt.wantOK {
				t.Fatalf("bindTerminalDaemonID(%q, %q, %v) = (%q, %v), want (%q, %v)", tt.current, tt.runtimeID, tt.valid, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
