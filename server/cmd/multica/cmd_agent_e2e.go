package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/hvejsel/firtal-persona/sdk/go"
	"github.com/multica-ai/multica/server/internal/daemon"
// CEREBRO-PATCH(cmd-agent-e2e): persona integration changes.
)

// agentE2ESpawnCmd is the local-test entry point for the persona ↔ cerebro
// integration. It bypasses the full Multica task-queue flow and spawns
// Claude Code directly with persona-aware env and a PreToolUse hook
// configured. The e2e-persona.sh script invokes this so the integration
// can be verified without standing up the entire Multica server stack.
//
// Production task spawns will go through daemon.runTask (cerebro-integration
// plan B6) and use this same wiring shape — actor resolve, sandbox profile,
// hook env, settings.json. This command is the smallest version that
// exercises that wiring end-to-end.
var agentE2ESpawnCmd = &cobra.Command{
	Use:   "e2e-spawn",
	Short: "Spawn Claude Code locally with a persona actor (for testing)",
	Long: `Spawns Claude Code in an ephemeral workdir wired to persona.
Resolves the persona actor, fetches its sandbox profile, writes a
settings.json with the cerebro-persona-hook configured, and runs the
agent against the given prompt. Used by scripts/e2e-persona.sh to
verify the per-call gating end-to-end.`,
	RunE: runAgentE2ESpawn,
}

var (
	e2ePersonaActor          string
	e2ePersonaURL            string
	e2ePersonaToken          string
	e2eHookBin               string
	e2eClaudeBin             string
	e2ePrompt                string
	e2eRuntimePersonaSandbox string
)

func init() {
	agentE2ESpawnCmd.Flags().StringVar(&e2ePersonaActor, "persona-actor", "", "persona actor UUID for the spawned agent")
	agentE2ESpawnCmd.Flags().StringVar(&e2ePersonaURL, "persona-url", "http://localhost:4500", "persona endpoint")
	agentE2ESpawnCmd.Flags().StringVar(&e2ePersonaToken, "persona-token", "", "persona service token (defaults to $PERSONA_SERVICE_TOKEN)")
	agentE2ESpawnCmd.Flags().StringVar(&e2eHookBin, "hook-bin", "", "path to cerebro-persona-hook binary (defaults to sibling of multica)")
	agentE2ESpawnCmd.Flags().StringVar(&e2eClaudeBin, "claude-bin", "claude", "path or name of claude CLI to spawn")
	agentE2ESpawnCmd.Flags().StringVar(&e2ePrompt, "prompt", "", "prompt to send to the agent")
	// E1: simulate the runtime-level upper bound. When set, the actor's
	// currently-assigned sandbox is overwritten with this name before the
	// spawn — same behaviour as the daemon's preparePersonaSpawn when a
	// runtime cap is in effect. This is what makes the e2e probe actually
	// exercise the upper-bound path, since the test actor was created with
	// a more permissive sandbox at setup time.
	agentE2ESpawnCmd.Flags().StringVar(&e2eRuntimePersonaSandbox, "runtime-persona-sandbox", "", "runtime-level persona sandbox cap (E1 simulation)")
	_ = agentE2ESpawnCmd.MarkFlagRequired("persona-actor")
	_ = agentE2ESpawnCmd.MarkFlagRequired("prompt")
	agentCmd.AddCommand(agentE2ESpawnCmd)
}

func runAgentE2ESpawn(cmd *cobra.Command, _ []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Minute)
	defer cancel()

	token := e2ePersonaToken
	if token == "" {
		token = os.Getenv("PERSONA_SERVICE_TOKEN")
	}
	if token == "" {
		return fmt.Errorf("persona token required (--persona-token or $PERSONA_SERVICE_TOKEN)")
	}

	hookBin := e2eHookBin
	if hookBin == "" {
		// Default: sibling of the multica binary in the same install dir.
		self, err := os.Executable()
		if err != nil {
			return fmt.Errorf("locate multica binary: %w", err)
		}
		hookBin = filepath.Join(filepath.Dir(self), "cerebro-persona-hook")
	}
	if _, err := os.Stat(hookBin); err != nil {
		return fmt.Errorf("hook binary not found at %s: %w (build with `make` or pass --hook-bin)", hookBin, err)
	}

	// Fetch sandbox profile so we know what tools are gated and can surface
	// the assignment in the daemon log. Not used for spawn config in this
	// MVP — Claude Code respects the hook's allow/deny independently.
	pc, err := persona.New(persona.Config{Endpoint: e2ePersonaURL, Token: token})
	if err != nil {
		return fmt.Errorf("init persona client: %w", err)
	}
	profile, err := pc.GetSandboxProfile(ctx, e2ePersonaActor)
	if err != nil {
		return fmt.Errorf("fetch sandbox profile for %s: %w", e2ePersonaActor, err)
	}

	// E1 simulation: if a runtime-cap was passed, reassign before the spawn.
	// This mirrors what daemon.preparePersonaSpawn does when the runtime has
	// an admin-set sandbox — the agent-level value is shadowed by the cap.
	// Without this step the e2e probe can't exercise the upper-bound path
	// because the actor was set up with the agent-level sandbox at fixture
	// time. The daemon does the same call there.
	if e2eRuntimePersonaSandbox != "" && profile.SandboxName != e2eRuntimePersonaSandbox {
		fmt.Fprintf(os.Stderr, "→ runtime cap active: reassigning %s → %q (was %q)\n",
			e2ePersonaActor, e2eRuntimePersonaSandbox, profile.SandboxName)
		if err := daemon.AssignSandboxByName(ctx, e2ePersonaURL, token, e2ePersonaActor, e2eRuntimePersonaSandbox); err != nil {
			return fmt.Errorf("apply runtime persona-sandbox cap: %w", err)
		}
		profile, err = pc.GetSandboxProfile(ctx, e2ePersonaActor)
		if err != nil {
			return fmt.Errorf("re-fetch sandbox profile after cap: %w", err)
		}
	}

	fmt.Fprintf(os.Stderr, "→ spawning as %q (sandbox: %s; allowed_tools: %v)\n",
		profile.ActorName, defaultIfEmpty(profile.SandboxName, "<none>"), profile.AllowedTools)

	// Build an ephemeral workdir with a Claude Code settings.json that wires
	// our PreToolUse hook. Use the workdir as both --add-dir and --settings
	// scope so settings don't leak into the operator's real ~/.claude.
	workdir, err := os.MkdirTemp("", "cerebro-persona-e2e-*")
	if err != nil {
		return fmt.Errorf("temp workdir: %w", err)
	}
	defer os.RemoveAll(workdir)

	settingsPath := filepath.Join(workdir, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return err
	}
	settings := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []map[string]any{
				{
					"matcher": "*",
					"hooks": []map[string]any{
						{
							"type":    "command",
							"command": hookBin,
						},
					},
				},
			},
		},
	}
	settingsBytes, _ := json.MarshalIndent(settings, "", "  ")
	if err := os.WriteFile(settingsPath, settingsBytes, 0o644); err != nil {
		return fmt.Errorf("write settings.json: %w", err)
	}

	// Spawn id uniquely identifies this run for the activity feed.
	spawnID := fmt.Sprintf("e2e-%d", time.Now().UnixNano())

	claudeCmd := exec.CommandContext(ctx, e2eClaudeBin,
		"--print",                  // non-interactive
		"--add-dir", workdir,       // make settings.json discoverable
		"--settings", settingsPath, // explicit settings file
		e2ePrompt,
	)
	claudeCmd.Dir = workdir
	claudeCmd.Env = append(os.Environ(),
		"PERSONA_URL="+e2ePersonaURL,
		"PERSONA_SERVICE_TOKEN="+token,
		"PERSONA_ACTOR_ID="+e2ePersonaActor,
		"PERSONA_SPAWN_ID="+spawnID,
	)
	stdout, err := claudeCmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := claudeCmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := claudeCmd.Start(); err != nil {
		return fmt.Errorf("start claude: %w", err)
	}
	go func() { _, _ = io.Copy(os.Stderr, stderr) }()
	output, _ := io.ReadAll(stdout)
	werr := claudeCmd.Wait()
	// Always print the agent's stdout to ours so the e2e script can grep it.
	fmt.Print(string(output))

	// claude --print exits 0 on success. Tool denial may make the LLM
	// produce different output but exit 0; that's fine — assertions in the
	// e2e script grep the output to differentiate.
	if werr != nil {
		// Forward exit code as informational only — the script decides.
		fmt.Fprintf(os.Stderr, "claude exited with: %v\n", werr)
	}
	return nil
}

func defaultIfEmpty(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}
