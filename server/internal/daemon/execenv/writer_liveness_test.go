package execenv

import (
	"testing"
)

func TestWriterAliveMarkerGatesReuse(t *testing.T) {
	t.Parallel()

	workspacesRoot := t.TempDir()
	env, err := Prepare(PrepareParams{
		WorkspacesRoot: workspacesRoot,
		WorkspaceID:    "ws-writer-alive",
		TaskID:         "11112222-3333-4444-5555-666677778888",
		Provider:       "claude",
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	defer env.Cleanup(true)

	if WriterAliveMarkerPresent(env.WorkDir) {
		t.Fatal("fresh workdir must not carry a marker")
	}
	if err := MarkWriterAlive(env.WorkDir); err != nil {
		t.Fatalf("MarkWriterAlive failed: %v", err)
	}
	if reused := Reuse(ReuseParams{WorkDir: env.WorkDir, Provider: "claude"}, testLogger()); reused != nil {
		t.Fatal("Reuse must decline a workdir holding a stale writer-liveness marker")
	}
	ClearWriterAlive(env.WorkDir)
	if WriterAliveMarkerPresent(env.WorkDir) {
		t.Fatal("ClearWriterAlive must remove the marker")
	}
	if reused := Reuse(ReuseParams{WorkDir: env.WorkDir, Provider: "claude"}, testLogger()); reused == nil {
		t.Fatal("Reuse must accept the workdir once the marker is cleared")
	}
}
