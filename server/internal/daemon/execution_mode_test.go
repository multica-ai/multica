package daemon

import (
	"io"
	"log/slog"
	"testing"
)

func TestNormalizeTaskExecutionMode(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tests := []struct {
		name string
		mode string
		want string
	}{
		{name: "empty defaults normal", mode: "", want: "normal"},
		{name: "normal stays normal", mode: "normal", want: "normal"},
		{name: "goal stays goal", mode: "goal", want: "goal"},
		{name: "unknown defaults normal", mode: "bad", want: "normal"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := normalizeTaskExecutionMode(tt.mode, logger); got != tt.want {
				t.Fatalf("normalizeTaskExecutionMode(%q) = %q, want %q", tt.mode, got, tt.want)
			}
		})
	}
}
