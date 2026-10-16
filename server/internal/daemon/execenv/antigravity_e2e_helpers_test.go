package execenv

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
)

type antigravityMCPEvent struct {
	Method   string `json:"method"`
	Session  string `json:"session,omitempty"`
	Name     string `json:"name,omitempty"`
	Argument int    `json:"argument,omitempty"`
	Result   string `json:"result,omitempty"`
}

func validateAntigravityMCPAudit(events []antigravityMCPEvent, session, name string, argument int, result string) error {
	initialized, notified, listed, called := false, false, false, false
	for _, event := range events {
		if event.Method == "server/discover" {
			continue
		}
		if event.Method == "initialize" {
			if event.Session != "" {
				return fmt.Errorf("initialize must start a new session")
			}
			initialized, notified, listed, called = true, false, false, false
			continue
		}
		if !initialized || event.Session != session {
			return fmt.Errorf("%s before initialize or with invalid session", event.Method)
		}
		switch event.Method {
		case "notifications/initialized":
			notified = true
		case "tools/list":
			if !notified {
				return fmt.Errorf("tools/list before initialized notification")
			}
			listed = true
		case "tools/call":
			if !listed || event.Name != name || event.Argument != argument || event.Result != result {
				return fmt.Errorf("invalid tools/call evidence: %+v", event)
			}
			called = true
		}
	}
	if !called {
		return fmt.Errorf("no verified tools/call for %s", name)
	}
	return nil
}

func runAntigravityProbe(ctx context.Context, executable, workDir, prompt string, prefix ...string) (string, error) {
	backend, err := agent.New("antigravity", agent.Config{
		ExecutablePath: executable, LaunchPrefix: prefix,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		return "", err
	}
	session, err := backend.Execute(ctx, prompt, agent.ExecOptions{Cwd: workDir, Timeout: 60 * time.Second})
	if err != nil {
		return "", err
	}
	for range session.Messages {
	}
	result, ok := <-session.Result
	if !ok {
		return "", fmt.Errorf("agent returned no result")
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if result.Status != "completed" {
		return "", fmt.Errorf("agent probe status %s", result.Status)
	}
	return result.Output, nil
}
