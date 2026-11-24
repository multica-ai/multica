//go:build !darwin

package agent

import (
	"context"
	"io"
	"log/slog"
	"os/exec"
	"time"
)

func (*antigravityProcess) startManaged(context.Context, *exec.Cmd, io.Writer, *slog.Logger, time.Duration) (bool, error) {
	return false, nil
}
