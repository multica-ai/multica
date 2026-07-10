package loops

import "testing"

func TestEvaluateGate(t *testing.T) {
	cfg := CheckGateConfig{Checks: [][]string{
		{"pytest", "tests/a"},
		{"go", "test", "./..."},
	}}

	cases := []struct {
		name     string
		outcomes []CheckOutcome
		want     GateState
	}{
		{
			name: "all passed",
			outcomes: []CheckOutcome{
				{Argv: []string{"pytest", "tests/a"}, Ran: true, ExitCode: 0},
				{Argv: []string{"go", "test", "./..."}, Ran: true, ExitCode: 0},
			},
			want: GatePassed,
		},
		{
			name: "one failed",
			outcomes: []CheckOutcome{
				{Argv: []string{"pytest", "tests/a"}, Ran: true, ExitCode: 0},
				{Argv: []string{"go", "test", "./..."}, Ran: true, ExitCode: 1},
			},
			want: GateFailed,
		},
		{
			name: "failure wins over pending",
			outcomes: []CheckOutcome{
				{Argv: []string{"pytest", "tests/a"}, Ran: true, ExitCode: 2},
			},
			want: GateFailed,
		},
		{
			name: "one not reported yet",
			outcomes: []CheckOutcome{
				{Argv: []string{"pytest", "tests/a"}, Ran: true, ExitCode: 0},
			},
			want: GatePending,
		},
		{
			name: "reported but not ran",
			outcomes: []CheckOutcome{
				{Argv: []string{"pytest", "tests/a"}, Ran: true, ExitCode: 0},
				{Argv: []string{"go", "test", "./..."}, Ran: false},
			},
			want: GatePending,
		},
		{
			name:     "no outcomes",
			outcomes: nil,
			want:     GatePending,
		},
		{
			name: "stale extra outcome ignored",
			outcomes: []CheckOutcome{
				{Argv: []string{"pytest", "tests/a"}, Ran: true, ExitCode: 0},
				{Argv: []string{"go", "test", "./..."}, Ran: true, ExitCode: 0},
				{Argv: []string{"rm", "-rf", "/"}, Ran: true, ExitCode: 1},
			},
			want: GatePassed,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EvaluateGate(cfg, tc.outcomes); got != tc.want {
				t.Fatalf("EvaluateGate = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestEvaluateGate_EmptyConfigIsPending(t *testing.T) {
	if got := EvaluateGate(CheckGateConfig{}, nil); got != GatePending {
		t.Fatalf("empty config should be pending, got %s", got)
	}
}
