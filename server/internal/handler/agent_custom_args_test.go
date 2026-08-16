package handler

import (
	"reflect"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestCustomArgsForRuntime(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		runtime db.AgentRuntime
		args    []string
		want    []string
	}{
		{
			name: "windows codex stores the canonical two-token default",
			runtime: db.AgentRuntime{
				Provider: "codex",
				Metadata: []byte(`{"os":"windows"}`),
			},
			args: []string{"--profile", "research"},
			want: []string{"--profile", "research", "-c", `windows.sandbox="unelevated"`},
		},
		{
			name: "windows codex preserves an explicit override without a duplicate",
			runtime: db.AgentRuntime{
				Provider: "codex",
				Metadata: []byte(`{"os":"windows"}`),
			},
			args: []string{"-c", `windows.sandbox="elevated"`, "--profile", "research"},
			want: []string{"-c", `windows.sandbox="elevated"`, "--profile", "research"},
		},
		{
			name: "non-windows codex stays unchanged",
			runtime: db.AgentRuntime{
				Provider: "codex",
				Metadata: []byte(`{"os":"linux"}`),
			},
			args: []string{"--profile", "research"},
			want: []string{"--profile", "research"},
		},
		{
			name: "non-codex windows runtime stays unchanged",
			runtime: db.AgentRuntime{
				Provider: "claude",
				Metadata: []byte(`{"os":"windows"}`),
			},
			args: []string{"--profile", "research"},
			want: []string{"--profile", "research"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := customArgsForRuntime(tt.runtime, tt.args)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("customArgsForRuntime() = %v, want %v", got, tt.want)
			}
		})
	}
}
