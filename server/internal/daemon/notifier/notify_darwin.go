//go:build darwin

package notifier

import (
	"context"
	"fmt"
	"os/exec"
)

type platformNotifier struct{}

func New() Notifier {
	return platformNotifier{}
}

func (platformNotifier) Notify(ctx context.Context, title, body string) error {
	script := fmt.Sprintf(`display notification %q with title %q`, body, title)
	return exec.CommandContext(ctx, "osascript", "-e", script).Run()
}
