package daemon

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestTaskIdentityEnvKeepsMulticaKeysOnly(t *testing.T) {
	t.Parallel()

	got := taskIdentityEnv(map[string]string{
		"MULTICA_TOKEN":   "mat_task",
		"MULTICA_TASK_ID": "task-1",
		"PATH":            "/bin",
		"TMPDIR":          "/tmp",
	})
	want := map[string]string{
		"MULTICA_TOKEN":   "mat_task",
		"MULTICA_TASK_ID": "task-1",
	}
	if len(got) != len(want) {
		t.Fatalf("taskIdentityEnv() = %#v, want %#v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("taskIdentityEnv()[%q] = %q, want %q", k, got[k], v)
		}
	}
}

func TestInsertTaskIdentityPinAfterWrapper(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		prefix []string
		pin    string
		want   []string
	}{
		{
			name:   "doppler run dashdash opencode",
			prefix: []string{"run", "--", "opencode"},
			pin:    "/tmp/reassert-task-env",
			want:   []string{"run", "--", "/tmp/reassert-task-env", "opencode"},
		},
		{
			name:   "no wrapper separator",
			prefix: []string{"--dangerously-skip-permissions"},
			pin:    "/tmp/reassert-task-env",
			want:   []string{"--dangerously-skip-permissions"},
		},
		{
			name:   "empty pin",
			prefix: []string{"run", "--", "opencode"},
			pin:    "",
			want:   []string{"run", "--", "opencode"},
		},
		{
			name:   "wrapper with no command after dashdash",
			prefix: []string{"run", "--"},
			pin:    "/tmp/reassert-task-env",
			want:   []string{"run", "--", "/tmp/reassert-task-env"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := insertTaskIdentityPinAfterWrapper(tt.prefix, tt.pin)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("insertTaskIdentityPinAfterWrapper() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestWriteTaskIdentityPinSkipsWhenNothingToPin(t *testing.T) {
	t.Parallel()

	path, err := writeTaskIdentityPin(t.TempDir(), map[string]string{"PATH": "/bin"})
	if err != nil {
		t.Fatalf("writeTaskIdentityPin() error = %v", err)
	}
	if path != "" {
		t.Fatalf("writeTaskIdentityPin() = %q, want empty", path)
	}
}

func TestWriteTaskIdentityPinCreatesScript(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path, err := writeTaskIdentityPin(dir, map[string]string{
		"MULTICA_TOKEN":   "mat_task",
		"MULTICA_TASK_ID": "task-1",
		"PATH":            "/bin",
	})
	if err != nil {
		t.Fatalf("writeTaskIdentityPin() error = %v", err)
	}
	if path == "" {
		t.Fatal("writeTaskIdentityPin() returned empty path")
	}
	if filepath.Dir(path) != dir {
		t.Fatalf("pin path %q not under %q", path, dir)
	}
}
