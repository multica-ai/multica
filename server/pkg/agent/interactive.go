package agent

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
)

// InteractiveCapabilities lets the daemon add provider-specific PTY support
// without coupling PTY lifecycle code to one CLI's argument grammar.
type InteractiveCapabilities struct {
	PTY                   bool
	Resume                bool
	StructuredObservation string
}

type InteractiveOptions struct {
	Prompt          string
	Model           string
	ThinkingLevel   string
	ServiceTier     string
	ResumeSessionID string
	ExtraArgs       []string
	CustomArgs      []string
}

type InteractiveLaunch struct {
	Args         []string
	Capabilities InteractiveCapabilities
}

// BuildInteractiveLaunch builds argv for a provider's native interactive CLI.
// It never returns a shell command and rejects transport/workdir/policy
// overrides owned by the daemon.
func BuildInteractiveLaunch(provider string, opts InteractiveOptions, logger *slog.Logger) (InteractiveLaunch, error) {
	if provider != "codex" {
		return InteractiveLaunch{}, fmt.Errorf("interactive PTY is unsupported for provider %q", provider)
	}
	if strings.TrimSpace(opts.Prompt) == "" {
		return InteractiveLaunch{}, errors.New("interactive Codex launch requires a prompt")
	}
	args := NormalizeCodexLaunchArgs(opts.ExtraArgs, opts.CustomArgs, nil, logger)
	filtered := make([]string, 0, len(args)+10)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		key := strings.SplitN(arg, "=", 2)[0]
		switch key {
		case "-C", "--cd", "-a", "--ask-for-approval", "-s", "--sandbox":
			if !strings.Contains(arg, "=") && i+1 < len(args) {
				i++
			}
			continue
		case "--dangerously-bypass-approvals-and-sandbox", "--dangerously-bypass-hook-trust", "--no-alt-screen", "--json", "--output-format":
			if key == "--output-format" && !strings.Contains(arg, "=") && i+1 < len(args) {
				i++
			}
			continue
		case "exec", "app-server", "resume":
			return InteractiveLaunch{}, fmt.Errorf("interactive Codex custom args may not select subcommand %q", arg)
		}
		filtered = append(filtered, arg)
	}
	args = filtered
	if opts.ResumeSessionID != "" {
		if _, err := uuid.Parse(opts.ResumeSessionID); err != nil {
			return InteractiveLaunch{}, errors.New("interactive Codex resume session id is invalid")
		}
		args = append([]string{"resume", opts.ResumeSessionID}, args...)
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.ThinkingLevel != "" {
		args = append(args, "--config", "model_reasoning_effort="+opts.ThinkingLevel)
	}
	if opts.ServiceTier == codexFastServiceTier {
		args = append(args, "--enable", codexFastModeFeature)
	}
	// These are daemon-owned and intentionally last-wins.
	args = append(args, "--ask-for-approval", "never", "--sandbox", "workspace-write", opts.Prompt)
	return InteractiveLaunch{Args: args, Capabilities: InteractiveCapabilities{PTY: true, Resume: true, StructuredObservation: "unavailable"}}, nil
}
