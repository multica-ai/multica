//go:build linux

package notifier

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
)

type platformNotifier struct{}

func New() Notifier {
	return platformNotifier{}
}

func (platformNotifier) Notify(ctx context.Context, title, body string) error {
	if strings.TrimSpace(os.Getenv("DISPLAY")) == "" && strings.TrimSpace(os.Getenv("WAYLAND_DISPLAY")) == "" {
		return errors.New("no desktop session detected")
	}
	if _, err := exec.LookPath("notify-send"); err != nil {
		return err
	}
	return exec.CommandContext(ctx, "notify-send", title, body).Run()
}
