package agent

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// OpenCode 2.0 reorganised the contract `opencode run` speaks. Four things the
// daemon depends on changed at once, and only the first announces itself:
//
//   - `--dir` was removed. The task workdir now comes from the process cwd,
//     which the backend already sets (together with PWD).
//   - `--variant` was removed. A model variant now rides along inside the model
//     string as `provider/model#variant`.
//   - OPENCODE_CONFIG_CONTENT is no longer honoured, and the only channel left
//     for MCP puts credentials somewhere the agent can commit them. Such runs
//     are refused — see ErrOpenCodeV2MCPUnsupported.
//   - `opencode run` became a thin client in front of a resident background
//     service, so signalling the client's process group no longer stops the
//     work that run started.
//
// The two removed flags fail loudly — `Unrecognized flag: --dir`, exit 1 within
// ~130ms (GH #8586), and the same for `--variant` on any agent with a thinking
// level. The other two do not announce themselves at all: before this file, a
// run that merely dropped `--dir` started fine and then executed without its MCP
// servers and without a working cancel. Handling them together is what keeps
// fixing the loud failure from exposing the quiet ones.
//
// Everything else the backend relies on was checked against 2.0.10 and is
// unchanged across the two majors — the `--format json` event vocabulary,
// `--session` resume, `.opencode/skills/` discovery, AGENTS.md, and
// `--dangerously-skip-permissions` (still accepted and still honoured) — so
// none of it is branched on here.

// opencodeInterruptTimeout bounds the session-interrupt call made while a run
// is being cancelled. It is deliberately short: the interrupt is an extra step
// in front of the termination path that already works, so a service that does
// not answer promptly must not delay the signals behind it.
const opencodeInterruptTimeout = 5 * time.Second

// opencodeUsesV2Contract reports whether the resolved OpenCode CLI speaks the
// 2.x contract described above.
//
// Scoped to BuiltinRuntime for the same reason opencodeSeparatesReasoning is: a
// custom runtime profile wraps a binary this package did not choose, and its
// reported version string does not establish which usage convention that binary
// speaks. Reading "2.x" off a wrapper that actually execs OpenCode 1.x would
// break a runtime that works today, so anything unrecognised keeps the 1.x
// argv. A custom profile pointed at OpenCode 2.x therefore stays broken until
// someone can map profiles onto capabilities directly — which is the same
// trade-off the reasoning check already makes, and strictly better than
// regressing working 1.x profiles on a guess.
func opencodeUsesV2Contract(cfg Config) bool {
	if !cfg.BuiltinRuntime {
		return false
	}
	// parseSemver scans for a semver token anywhere in the string, which is what
	// this needs: 2.x reports `opencode v2.0.10` where 1.x reports a bare
	// `1.18.31`, and extractVersionLine keeps whichever whole line it matched.
	version, err := parseSemver(strings.TrimSpace(cfg.CLIVersion))
	if err != nil {
		return false
	}
	return version.Major >= 2
}

// opencodeModelArg folds a thinking level into the model string for the 2.x
// contract, which has no `--variant` flag and instead reads the variant off the
// model as `provider/model#variant`.
//
// The second return reports whether the thinking level was representable. It is
// false only when there is a level but no model to attach it to: 2.x resolves
// the default model server-side, so there is no model string to append to and
// the level cannot be expressed at all. Callers warn rather than fail, because
// losing the reasoning effort is not worth failing an otherwise valid task.
func opencodeModelArg(model, thinkingLevel string) (string, bool) {
	if thinkingLevel == "" {
		return model, true
	}
	if model == "" {
		return model, false
	}
	// A model that already carries a variant was pinned deliberately (by
	// agent.model or custom_args); it wins over the agent-level thinking level
	// rather than growing a second `#` that OpenCode would reject.
	if strings.Contains(model, "#") {
		return model, true
	}
	return model + "#" + thinkingLevel, true
}

// ErrOpenCodeV2MCPUnsupported reports that the MCP servers Multica manages for
// a task cannot be delivered to an OpenCode 2.x runtime yet.
//
// The message deliberately does not name agent.mcp_config. ExecOptions.McpConfig
// is the *composed* set, and only one of its sources is the agent's own column:
// the daemon also folds in the workspace MCP servers bound to the agent (at
// claim), the task's integration/Composio servers and the plugin-hook tool
// server (at run), and the runtime's own servers on top of those. A task whose
// agent has no MCP configuration at all still arrives here with a non-empty
// config, so an error telling the operator to edit agent.mcp_config would point
// at a field that may be empty already and would not release the task.
//
// 2.x honours no environment channel for config — OPENCODE_CONFIG_CONTENT (the
// 1.x channel), OPENCODE_CLI_CONFIG_CONTENT, OPENCODE_CONFIG and
// OPENCODE_CONFIG_DIR were each checked against 2.0.10, with and without
// --standalone, and none of them reach the session. The only channel that works
// is <workdir>/opencode.json, and MCP entries carry bearer headers, OAuth client
// secrets and environment values.
//
// Writing them there is not safe today. The workdir is the agent's own working
// tree, and in local-directory mode it is the user's checkout: the agent can
// commit that file mid-run with an ordinary `git add -A`, which puts the
// credential in Git history where no end-of-task cleanup can reach it. File
// permissions do not help, because the committing process is the same user.
//
// Failing here is deliberate. Running the task anyway would silently drop every
// MCP server the agent was configured with, which is harder to diagnose than a
// refusal naming the cause.
//
// The way out is known and measured, and wants its own change (MUL-7523):
// `--standalone` gives the run a private server that inherits the daemon's
// environment, and 2.x then resolves `{env:NAME}` placeholders inside the
// config, so the file can carry references while the secrets stay in the
// process environment. That costs a server start per run and changes the
// process topology cancellation depends on, so it is not something to fold into
// a compatibility fix.
var ErrOpenCodeV2MCPUnsupported = errors.New(
	"opencode: Multica-managed MCP servers cannot be delivered to an OpenCode 2.x runtime yet. " +
		"2.x accepts MCP configuration only through a file in the task working directory, where " +
		"the agent's own commits would capture the servers' credentials. " +
		"To run this task: point the agent at an OpenCode 1.x runtime, or remove the MCP servers " +
		"Multica supplies it — these can come from the agent's MCP configuration, workspace MCP " +
		"servers bound to the agent, the workspace's integration tools, or an installed plugin's " +
		"hook tools")

// opencodeCheckMCPSupport fails a 2.x run that carries MCP configuration, rather
// than starting it without the servers it was supposed to get.
func opencodeCheckMCPSupport(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	// An mcp_config that translates to no servers asks for nothing, so there is
	// nothing to refuse.
	servers, err := translateMCPConfigForOpenCode(raw)
	if err != nil {
		return err
	}
	if len(servers) == 0 {
		return nil
	}
	return ErrOpenCodeV2MCPUnsupported
}

// opencodeSessionTracker carries the session id observed on a run's event
// stream over to that run's cancellation handler, which needs it to interrupt
// the session server-side.
//
// It exists because the two live in different goroutines: the scanner learns the
// id from the first event that carries one, while the cancellation handler is
// parked on the context. Execute gives every run its own tracker, so two runs
// sharing a Backend value cannot see each other's session.
type opencodeSessionTracker struct {
	mu sync.Mutex
	id string
}

// set records the first session id seen. Safe on a nil tracker so backends built
// without one (every unit test that drives processEvents directly) need no
// special casing.
func (t *opencodeSessionTracker) set(id string) {
	if t == nil || id == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.id == "" {
		t.id = id
	}
}

// get returns the observed session id, or "" if no event carried one yet.
func (t *opencodeSessionTracker) get() string {
	if t == nil {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.id
}

// opencodeRunConnection is the connection context a run was launched with, so
// its interrupt reaches the same service the work is actually running on.
//
// `opencode run` accepts `--server <url>` (and users can pass one through
// agent.custom_args), and it resolves the default background service relative to
// the process environment and working directory. An interrupt that dropped any
// of that would report success against a different service while the real
// session kept running.
type opencodeRunConnection struct {
	cmd Command
	// server is the `--server` value the run was launched with, if any.
	server string
	// standalone reports that the run owns a private server. That server is a
	// child of the client, so the existing process-group signalling already
	// stops it and no interrupt is needed.
	standalone bool
	// env and dir mirror the run's process environment and working directory,
	// which is how the CLI discovers the default service.
	env []string
	dir string
}

// opencodeConnectionFromArgs reads the connection flags out of a run's final
// argv, after custom_args have been merged in.
func opencodeConnectionFromArgs(args []string) (server string, standalone bool) {
	for i, arg := range args {
		switch {
		case arg == "--standalone":
			standalone = true
		case arg == "--server" && i+1 < len(args):
			server = args[i+1]
		case strings.HasPrefix(arg, "--server="):
			server = strings.TrimPrefix(arg, "--server=")
		}
	}
	return server, standalone
}

// opencodeInterruptSession asks the OpenCode 2.x service to stop a session. On
// 2.x this is the only thing that actually stops a run.
//
// `opencode run` is a thin client: the work happens inside a background service
// that outlives it, so the SIGTERM→SIGKILL the backend sends to the client's
// process group leaves the agent running — still calling tools, still writing to
// the workdir — after Multica has already recorded the task as finished.
// Reproduced against 2.0.10: with the client confirmed dead, a shell command the
// agent had started went on to complete 35 seconds later.
//
// Best effort by construction. This runs in front of the existing termination
// path and never replaces it, so an unreachable service, a session id that was
// never observed, or a non-zero exit all degrade to exactly the behaviour
// without it. It deliberately does not use the run's context: by the time this
// is called that context is already cancelled, which is what triggered it.
func opencodeInterruptSession(conn opencodeRunConnection, sessionID string, logger *slog.Logger) {
	if sessionID == "" {
		return
	}
	if conn.standalone {
		// A private server dies with the process group below; interrupting the
		// default service here would signal an unrelated session.
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), opencodeInterruptTimeout)
	defer cancel()

	args := []string{"api", "POST", "/api/session/" + sessionID + "/interrupt"}
	if conn.server != "" {
		args = append(args, "--server", conn.server)
	}
	cmd := conn.cmd.exec(ctx, args...)
	hideAgentWindow(cmd)
	// Same environment and working directory as the run, because that is what
	// the CLI uses to find the default background service.
	cmd.Env = conn.env
	cmd.Dir = conn.dir
	// combinedOutputOwned rather than cmd.CombinedOutput: the context timeout
	// alone does not bound this call. CombinedOutput waits for EOF on the output
	// pipes, and an `api` process that exits while a descendant still holds them
	// keeps the read blocked — with the termination path behind it stuck too.
	// runOwned puts the call in its own process tree, applies a WaitDelay to the
	// pipe wait, and kills whatever is left.
	out, err := combinedOutputOwned(cmd, logger)
	if err != nil {
		if logger != nil {
			logger.Warn("opencode: session interrupt failed; the agent may keep running server-side",
				"session", sessionID, "server", conn.server, "error", err, "output", strings.TrimSpace(string(out)))
		}
		return
	}
	if logger != nil {
		logger.Info("opencode: session interrupted", "session", sessionID, "server", conn.server)
	}
}
