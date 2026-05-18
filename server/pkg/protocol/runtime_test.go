package protocol

import "testing"

func TestResolveTraceEnabled(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
		want bool
	}{
		{name: "nil defaults true", raw: nil, want: true},
		{name: "empty object defaults true", raw: []byte(`{}`), want: true},
		{name: "explicit true", raw: []byte(`{"trace_enabled":true}`), want: true},
		{name: "explicit false", raw: []byte(`{"trace_enabled":false}`), want: false},
		{name: "invalid defaults true", raw: []byte(`{`), want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveTraceEnabled(tt.raw); got != tt.want {
				t.Fatalf("ResolveTraceEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveTaskRunMode(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
		want string
	}{
		{name: "nil defaults normal", raw: nil, want: TaskRunModeNormal},
		{name: "empty object defaults normal", raw: []byte(`{}`), want: TaskRunModeNormal},
		{name: "plan", raw: []byte(`{"run_mode":"plan"}`), want: TaskRunModePlan},
		{name: "unknown defaults normal", raw: []byte(`{"run_mode":"other"}`), want: TaskRunModeNormal},
		{name: "invalid defaults normal", raw: []byte(`{`), want: TaskRunModeNormal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveTaskRunMode(tt.raw); got != tt.want {
				t.Fatalf("ResolveTaskRunMode() = %q, want %q", got, tt.want)
			}
		})
	}
}
