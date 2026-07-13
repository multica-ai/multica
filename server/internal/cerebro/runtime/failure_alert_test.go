package runtime

import (
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

func TestRuntimeFailureAlertCopy(t *testing.T) {
	tests := []struct {
		reason string
		want   string
	}{
		{taskfailure.ReasonAgentRuntimeMissingExecutable.String(), "missing a required executable"},
		{taskfailure.ReasonAgentRuntimeVersionUnsupported.String(), "unsupported runtime version"},
	}
	for _, tt := range tests {
		t.Run(tt.reason, func(t *testing.T) {
			title, body := runtimeFailureAlertCopy("Sara", tt.reason)
			if !strings.Contains(title, "Sara") || !strings.Contains(body, tt.want) {
				t.Fatalf("copy = %q / %q, want runtime name and %q", title, body, tt.want)
			}
		})
	}
}
