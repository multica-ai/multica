package daemonsettings

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
)

func TestRefreshExistingWorkspaceCallsRefresh(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	RefreshExistingWorkspace(context.Background(), "ws-1", func(ctx context.Context, workspaceID string) error {
		calls.Add(1)
		if workspaceID != "ws-1" {
			t.Fatalf("workspaceID = %q, want ws-1", workspaceID)
		}
		return nil
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if got := calls.Load(); got != 1 {
		t.Fatalf("refresh calls = %d, want 1", got)
	}
}

func TestRefreshExistingWorkspaceSwallowsRefreshError(t *testing.T) {
	t.Parallel()

	RefreshExistingWorkspace(context.Background(), "ws-1", func(ctx context.Context, workspaceID string) error {
		return errors.New("temporary outage")
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestRefreshExistingWorkspaceAllowsNilDependencies(t *testing.T) {
	t.Parallel()

	RefreshExistingWorkspace(context.Background(), "ws-1", nil, nil)
}
