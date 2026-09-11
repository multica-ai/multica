package service

import "testing"

func TestParseAgentTaskMaxAttemptsSetting(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want int32
		ok   bool
	}{
		{"empty", "", 0, false},
		{"empty object", `{}`, 0, false},
		{"valid 4", `{"agent_task":{"max_attempts":4}}`, 4, true},
		{"valid 1 disables retry", `{"agent_task":{"max_attempts":1}}`, 1, true},
		{"valid 10", `{"agent_task":{"max_attempts":10}}`, 10, true},
		{"too low", `{"agent_task":{"max_attempts":0}}`, 0, false},
		{"too high", `{"agent_task":{"max_attempts":11}}`, 0, false},
		{"wrong type", `{"agent_task":{"max_attempts":"3"}}`, 0, false},
		{"null", `{"agent_task":{"max_attempts":null}}`, 0, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := ParseAgentTaskMaxAttemptsSetting([]byte(tc.in))
			if ok != tc.ok || got != tc.want {
				t.Fatalf("Parse(%q) = (%d,%v), want (%d,%v)", tc.in, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestDefaultAgentTaskMaxAttempts(t *testing.T) {
	if DefaultAgentTaskMaxAttempts != 4 {
		t.Fatalf("DefaultAgentTaskMaxAttempts = %d, want 4", DefaultAgentTaskMaxAttempts)
	}
}
