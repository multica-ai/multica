package agent

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type codexPreflightResult struct {
	err error
}

var codexAppServerPreflightCache sync.Map

func ensureCodexAppServer(ctx context.Context, execPath string) error {
	if cached, ok := codexAppServerPreflightCache.Load(execPath); ok {
		return cached.(codexPreflightResult).err
	}

	versionCtx, cancelVersion := context.WithTimeout(ctx, 5*time.Second)
	version, _ := detectCLIVersion(versionCtx, execPath)
	cancelVersion()

	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(checkCtx, execPath, "app-server", "--help")
	output, err := cmd.CombinedOutput()
	if err == nil {
		codexAppServerPreflightCache.Store(execPath, codexPreflightResult{})
		return nil
	}

	details := strings.TrimSpace(string(output))
	if details == "" {
		details = err.Error()
	}

	versionSuffix := ""
	if version != "" {
		versionSuffix = fmt.Sprintf(" (detected version: %s)", version)
	}

	preflightErr := fmt.Errorf(
		"codex CLI does not support the `app-server` command%s: %s. Please upgrade codex-cli and restart the daemon",
		versionSuffix,
		details,
	)
	codexAppServerPreflightCache.Store(execPath, codexPreflightResult{err: preflightErr})
	return preflightErr
}
