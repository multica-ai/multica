// CEREBRO-PATCH(daemon-runtime-tool-probe): FIR-3762 — measure a runtime's tool
// inventory instead of hand-maintaining it, for EVERY supported provider.
//
// Why this exists. The capability register is what claim-time tool exposure is
// resolved against (toolaccess.ListEffectiveTools lists capabilities reported by
// the runtime subject). For a local runtime the register is filled from the
// snapshot the daemon reports here. Before this file the snapshot was probed for
// exactly one provider — claude — and every other provider fell back to the
// static providerRegistry row, which is empty for pi, firtal-local,
// firtal-gateway and the eight providers that had no row at all. Empty inventory
// means zero exposed tools, an empty task mandate, and taskmandate.Authorize
// denying every call: the agent starts and cannot read its own issue. Measured
// live on 2026-07-26: 14 of 39 runtimes in the Firtal workspace reported zero
// tools, and agent Mette (pi) had 0 of 442 capabilities callable.
//
// The design point is that EVERY provider has an entry in providerInventories.
// A provider is either measured by a probe, or it carries a written reason why
// it cannot be — never an unexplained gap. TestEveryProviderHasAnInventoryEntry
// enforces that, so adding a backend to agent.New forces a decision here.
//
// A probe measures what the runtime will actually call, not what we think it
// should have: a hand-typed list drifts the day the CLI adds a tool, and a
// fabricated row is worse than an empty one because it looks governable in the
// admin table while every call is still denied.
package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/cerebro/mcpscan"
)

// probeTimeout is the per-provider budget. A runtime slower than this looks
// broken to a human too, so the timeout is the right signal — and the daemon
// must not stall its register/heartbeat path waiting for a sick CLI.
const probeTimeout = 30 * time.Second

// toolProbe measures the callable tool names a runtime exposes on this host.
// The names must be the ones the runtime sends to the tool-policy resolve at
// call time, because localtoolpolicy.ProviderPolicyToolKey turns that same
// string into the capability key the mandate is matched on ("tools:<name>" for
// a bare name, passed through unchanged for "mcp__server__tool" / "server:tool").
// A probe that returns a different spelling produces rows nothing matches.
type toolProbe func(ctx context.Context, executable string) ([]string, error)

// providerInventory is the per-provider decision: measure it, or say why not.
type providerInventory struct {
	// Probe measures the inventory. Nil means not measurable today.
	Probe toolProbe
	// Reason documents why Probe is nil. Required when Probe is nil, and it is
	// the text a future implementer needs — what was tried and what blocked it.
	Reason string
	// Native lists tool names the runtime dispatches itself and the probe
	// channel STRUCTURALLY cannot see, so they are unioned onto the measured
	// result instead of replacing it. This is not a licence to hand-type an
	// inventory — the rule in this file's header still stands. It exists
	// because a probe that measures one channel is blind to a second one by
	// construction, and an unlisted name is denied at call time no matter what
	// the policy chain says. Every entry must cite the live measurement it came
	// from, so it is evidence rather than a guess.
	Native []string
	// Channels are further MEASURED channels the primary probe is structurally
	// blind to. Native hand-lists names; a channel asks a live server, so it
	// cannot drift the way a hand-typed list does. Each result is spelled with
	// the channel's Prefix, because the same server is spelled differently by
	// different runtimes (Claude Code calls an MCP tool
	// "mcp__<server>__<tool>", the Pi harness registers it bare). A channel is
	// best-effort: its failure leaves the primary measurement intact.
	Channels []toolChannel
}

// toolChannel is one additional measured source of callable names, with the
// spelling the runtime uses when it sends that name to /tool-policy/resolve.
type toolChannel struct {
	// Probe measures the channel's inventory. Names come back bare.
	Probe toolProbe
	// Prefix is prepended to every measured name to produce the call-time
	// spelling. Empty means the runtime calls the names bare.
	Prefix string
	// Reason documents which channel this is and the live measurement it was
	// added from, so a future reader can re-verify instead of trusting it.
	Reason string
}

// providerInventories covers every provider agent.New supports. Keep it in
// step with that list; the coverage test fails the build otherwise.
var providerInventories = map[string]providerInventory{
	// Verified: the claude CLI announces its tool list in the system/init line
	// of its own stream-json output. Live evidence: runtime 3a795630 on
	// Jespers-MacBook-Pro.local reports discovery_method="probed" with 182 tools.
	//
	// The claude probe is blind to ONE channel by construction: it runs the CLI
	// standalone, while the daemon starts a task's claude with an injected
	// `multica` MCP server (CEREBRO-PATCH(daemon-claude-mcp-self-entry) in
	// server/pkg/agent/claude.go) plus `--strict-mcp-config`. Multica's own
	// actions therefore never reach the capability register, never reach the
	// task mandate, and every mcp__multica__* call is refused with "task mandate
	// denied the call" — while the 70 platform-catalog rows on the Capabilities
	// card all read deny with reason "mandate denied capability: tool outside
	// task mandate". Live evidence, agent Sara (d60e3580) on 2026-07-29: 135
	// capability rows, 65 reported by the runtime (firtal-browser, hooks,
	// secret bindings, claude built-ins), 0 spelled mcp__multica__*, and all 70
	// platform rows callable=false. The Multica channel closes exactly that gap
	// — measured, not hand-typed, from the same `multica mcp serve` process the
	// injected server starts.
	"claude": {Probe: probeClaudeToolNames, Channels: []toolChannel{{
		Probe:  probeMulticaMCPTools,
		Prefix: "mcp__multica__",
		Reason: "daemon injects the multica MCP server at spawn; the standalone claude probe cannot see it (FIR-4047)",
	}}},

	// Verified 2026-07-26 on Jespers-MacBook-Pro.local: the channel answers
	// initialize + tools/list in 1.8s with 249 tools. This is not an approximation
	// of Pi's surface — it is the exact channel Pi opens for itself
	// (packages/cerebro-pi-harness/multica-harness.ts spawns `multica mcp serve`
	// on session_start and registers every listed tool under its BARE name), so
	// the measured names are the names Pi sends to /tool-policy/resolve.
	//
	// The channel is Multica's, so it lists Multica's tools and NOTHING of Pi's
	// own — Pi dispatches those from its internal registry, which never crosses
	// the MCP wire. That blind spot is why agent Mette could start on
	// 2026-07-27, read her issue and post a comment (all Multica tools) while
	// every single shell call was denied with "task mandate denied the call":
	// `bash` had no capability row for the mandate to match, so the policy chain
	// was never consulted. Native closes exactly that gap.
	//
	// Native source — the live observed-access register for agent Mette
	// (e7b23ae1) on 2026-07-27: {"name":"bash","uses":116,"status":"unmapped",
	// "drift":true}. That is Pi calling its own tool 116 times and being denied
	// every time, not a guess about Pi's surface. Add further names the same
	// way — from an observed call, never from an assumption.
	"pi": {Probe: probeMulticaMCPTools, Native: []string{"bash"}},

	// Verified 2026-07-26: `opencode serve` exposes GET /experimental/tool/ids,
	// which answered with 14 bare tool names (bash, read, glob, grep, edit,
	// write, task, webfetch, todowrite, websearch, skill, apply_patch, question,
	// invalid). Vendor-marked experimental, so a failure here is expected to be
	// survivable: the probe error path keeps the static row.
	"opencode": {Probe: probeOpencodeTools},

	// Hermes had no entry that could be measured, so every hermes runtime
	// reported discovery_method="not_measured" with zero tools and the claim-time
	// guard in daemon_effective_tools_cerebro.go rejected EVERY task with HTTP
	// 500 ("failed to resolve agent tool mandate"). Live evidence, MUL-4634:
	// runtime 97afbd97 on sara.local failed 1.529 consecutive claims between
	// 2026-08-06 16:27 and 2026-08-07 10:00 while claude, codex and opencode on
	// the same host claimed normally.
	//
	// The hermes CLI still has no command that lists individual tool names, all
	// four channels re-checked against hermes 0.16.0 on 2026-08-07: `hermes tools
	// list` answers at TOOLSET grain (23 groups), `hermes tools --summary` is the
	// same grain and refuses to run without a TTY, `hermes prompt-size --json`
	// reports a count and no names ("tools": {"count": 32}), and `hermes mcp
	// serve` exposes the conversation gateway (conversations_list, messages_send,
	// …), not the agent's own inventory.
	//
	// What IS measurable is the two channels hermes actually calls through, both
	// measured in live `hermes acp` runs on 2026-08-07:
	//
	//   - MCP tools. Hermes registers them as `mcp_<server>_<tool>` with every
	//     character outside [A-Za-z0-9_] replaced by "_" (tools/mcp_tool.py,
	//     sanitize_mcp_name_component). Live: a call to the Multica MCP server's
	//     get_me arrived as tool call `mcp_multica_get_me`. The probe asks the
	//     same `multica mcp serve` process the daemon hands hermes, and spells
	//     the answer the way hermes will spell it at call time.
	//   - Hermes' own built-ins, which never cross the MCP wire and are therefore
	//     structurally invisible to that probe. Native lists the ones observed
	//     executing in those runs — hermes logs `tool <name> completed` for each:
	//     terminal, read_file, write_file, patch, search_files, todo.
	//
	// Native is deliberately NOT the full 32-tool surface: the rest were not
	// observed, and this file does not hand-type unmeasured names. That is safe
	// because of what the gate actually sees. Hermes asks its ACP client for
	// permission on exactly two paths — file edits (write_file, patch, via
	// acp_adapter/edit_approval.py) and dangerous shell commands (terminal, via
	// acp_adapter/permissions.py) — and all three are listed here. Every other
	// built-in executes without ever reaching /tool-policy/resolve; see the
	// measured note in server/internal/cerebro/localtoolpolicy/provider_adapter.go.
	"hermes": {Probe: probeHermesTools, Native: []string{
		"terminal", "read_file", "write_file", "patch", "search_files", "todo",
	}},

	// Kimi and Kiro are the same ACP family as hermes, but neither CLI is
	// installed on any Firtal host today, so nothing about their tool-listing
	// surface or their call-time spelling has been measured. probeHermesTools is
	// not reused for them: it encodes hermes' own MCP spelling, and a
	// row spelled for the wrong runtime matches no call. Measure one live run
	// per CLI first — do not guess a command shape.
	"kimi": {Reason: "ACP runtime; CLI not installed on any Firtal host, tool-listing surface and call-time spelling unverified"},
	"kiro": {Reason: "ACP runtime; CLI not installed on any Firtal host, tool-listing surface and call-time spelling unverified"},

	// Codex, cursor and gemini carry curated static tool lists that match their
	// live call names and are exposing tools correctly today (5, 8 and 5 rows).
	// A probe would be an improvement, not a fix, so it is not urgent — but the
	// static list is hand-maintained and will drift.
	"codex":  {Reason: "curated static list matches live call names and resolves today; probe is an improvement, not a fix"},
	"cursor": {Reason: "curated static list matches live call names and resolves today; probe is an improvement, not a fix"},
	"gemini": {Reason: "curated static list matches live call names and resolves today; probe is an improvement, not a fix"},

	// `agy --help` exposes no tool-listing command and antigravity honours no
	// MCP config (see execOptionsSupport), so there is no channel to ask.
	"antigravity": {Reason: "agy exposes no tool-listing command and honours no MCP config, so there is no channel to ask"},

	// Not installed on any Firtal host; nothing verified. Neither honours an MCP
	// config, so the MCP probe above does not apply either.
	"copilot":  {Reason: "CLI not installed on any Firtal host and honours no MCP config; tool-listing surface unverified"},
	"openclaw": {Reason: "CLI not installed on any Firtal host and honours no MCP config; tool-listing surface unverified"},

	// API backends: no argv, no local process to ask. firtal-gateway's inventory
	// is already written server-side by cloudtoolscan (the gateway enumerates the
	// built-ins it has actually implemented — 67 rows live), which is why gateway
	// agents have tools despite an empty static row. openai-eu and firtal-local
	// have no equivalent enumeration yet.
	"firtal-gateway": {Reason: "cloud runtime; inventory is written server-side by cloudtoolscan, not by a daemon probe"},
	"openai-eu":      {Reason: "API backend with no local process to ask; needs a server-side enumeration like cloudtoolscan"},
	"firtal-local":   {Reason: "API backend with no local process to ask; needs a server-side enumeration like cloudtoolscan"},
}

// probeProviderTools measures the provider's inventory, or reports that it is
// not measurable. ok is false when there is no probe or the probe failed; the
// caller then keeps the static row and marks it as not measured.
func probeProviderTools(providerType, executable string) (tools []string, ok bool) {
	entry, known := providerInventories[providerType]
	if !known || entry.Probe == nil {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	measured, err := entry.Probe(ctx, executable)
	if err != nil || len(measured) == 0 {
		return nil, false
	}
	// Union, never replace: Native and Channels cover the further channels the
	// primary probe is blind to, so dropping them would re-open the lockout the
	// measurement was added to close. dedupeToolNames keeps the result stable if
	// a name appears in more than one channel.
	measured = append(measured, entry.Native...)
	for _, channel := range entry.Channels {
		measured = append(measured, probeToolChannel(providerType, executable, channel)...)
	}
	return dedupeToolNames(measured), true
}

// probeToolChannel measures one additional channel. It is deliberately
// best-effort: a channel that cannot start must never discard the primary
// measurement, because an empty inventory denies every call the runtime makes.
// It carries its own budget so a slow primary probe cannot starve it.
func probeToolChannel(providerType, executable string, channel toolChannel) []string {
	if channel.Probe == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	names, err := channel.Probe(ctx, executable)
	if err != nil {
		slog.Warn("runtime tool channel probe failed",
			"provider", providerType,
			"prefix", channel.Prefix,
			"reason", channel.Reason,
			"error", err,
		)
		return nil
	}
	out := make([]string, 0, len(names))
	for _, name := range names {
		if name = strings.TrimSpace(name); name != "" {
			out = append(out, channel.Prefix+name)
		}
	}
	return out
}

// probeClaudeToolNames adapts the existing claude probe to the toolProbe shape.
func probeClaudeToolNames(_ context.Context, executable string) ([]string, error) {
	return probeClaudeTools(executable)
}

// multicaHarnessCommand resolves the binary the Pi harness itself would spawn,
// so the probe measures the same channel Pi gets. Mirrors the harness's own
// resolution order (MULTICA_PI_HARNESS_COMMAND, then "multica" on PATH), with
// the running daemon binary preferred over PATH because a daemon started from a
// build directory must not silently probe an older installed CLI.
func multicaHarnessCommand() string {
	if override := strings.TrimSpace(os.Getenv("MULTICA_PI_HARNESS_COMMAND")); override != "" {
		return override
	}
	if self, err := os.Executable(); err == nil && strings.TrimSpace(self) != "" {
		return self
	}
	return "multica"
}

// probeMulticaMCPTools asks the Multica MCP channel for its inventory. Names are
// returned bare, exactly as the harness registers local tools and therefore
// exactly as Pi spells them at call time.
func probeMulticaMCPTools(ctx context.Context, _ string) ([]string, error) {
	tools, err := mcpscan.Scan(ctx, mcpscan.ServerSpec{
		Name:    "multica",
		Command: multicaHarnessCommand(),
		Args:    []string{"mcp", "serve"},
		Env:     multicaProbeEnv(),
	}, probeTimeout)
	if err != nil {
		return nil, fmt.Errorf("probe multica mcp: %w", err)
	}
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		if name := strings.TrimSpace(t.Name); name != "" {
			names = append(names, name)
		}
	}
	return names, nil
}

// multicaProbeEnv is the explicit environment the Multica MCP channel needs to
// start. mcpscan deliberately does NOT inherit os.Environ (see buildEnv there) so
// a declared MCP server cannot harvest the daemon's environment, and this probe
// keeps that posture: it passes an allowlist instead of the whole environment.
// HOME is required — without it the CLI cannot resolve its config path and exits
// before the handshake ("load config: resolve CLI config path: $HOME is not
// defined"). PATH and XDG_CONFIG_HOME follow for the same reason, and the
// MULTICA_* variables carry the daemon's own identity and endpoint so the channel
// answers for this workspace rather than an unauthenticated session.
func multicaProbeEnv() map[string]string {
	out := map[string]string{}
	for _, key := range []string{"HOME", "PATH", "XDG_CONFIG_HOME"} {
		if value := os.Getenv(key); value != "" {
			out[key] = value
		}
	}
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok && strings.HasPrefix(key, "MULTICA_") {
			out[key] = value
		}
	}
	// CEREBRO-PATCH(probe-identity-from-profile): MUL-4634. The daemon is
	// normally started from launchd or a plain nohup with no MULTICA_TOKEN in
	// its environment, and the mcp serve child then reads the DEFAULT profile,
	// finds no token and exits with "not authenticated" before the handshake.
	// Every provider measured through this channel (hermes, pi) therefore
	// reported not_measured with zero tools, and cerebroEffectiveToolsForClaim
	// rejected every claim with HTTP 500 - 1.529 consecutive failures on
	// sara.local. The daemon has already resolved its own profile credentials by
	// then, so the channel uses those unless the environment supplies its own.
	token, serverURL := probeIdentity()
	if out["MULTICA_TOKEN"] == "" && token != "" {
		out["MULTICA_TOKEN"] = token
	}
	if out["MULTICA_SERVER_URL"] == "" && serverURL != "" {
		out["MULTICA_SERVER_URL"] = serverURL
	}
	return out
}

// probeIdentityState holds the daemon's own credentials for the Multica MCP
// probe. It is deliberately not an os.Setenv: the daemon spawns every agent
// process with a task-scoped token, and putting its own long-lived token in the
// process environment would hand that token to all of them.
var probeIdentityState struct {
	sync.RWMutex
	token     string
	serverURL string
}

// setProbeIdentity records the credentials the Multica MCP channel starts with.
// Called once the daemon has resolved its profile, before any runtime registers.
func setProbeIdentity(token, serverURL string) {
	probeIdentityState.Lock()
	defer probeIdentityState.Unlock()
	probeIdentityState.token = token
	probeIdentityState.serverURL = serverURL
}

func probeIdentity() (token, serverURL string) {
	probeIdentityState.RLock()
	defer probeIdentityState.RUnlock()
	return probeIdentityState.token, probeIdentityState.serverURL
}

// probeOpencodeTools starts opencode's headless server on a loopback port and
// reads its tool-id list. The server is always stopped before returning.
func probeOpencodeTools(ctx context.Context, executable string) ([]string, error) {
	if strings.TrimSpace(executable) == "" {
		executable = "opencode"
	}
	port, err := freeLoopbackPort()
	if err != nil {
		return nil, fmt.Errorf("probe opencode: reserve port: %w", err)
	}
	cmd := exec.CommandContext(ctx, executable, "serve", "--port", fmt.Sprint(port))
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("probe opencode: start server: %w", err)
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	url := fmt.Sprintf("http://127.0.0.1:%d/experimental/tool/ids", port)
	names, err := getJSONStringsUntilReady(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("probe opencode: %w", err)
	}
	return names, nil
}

// getJSONStringsUntilReady polls url until the server accepts the request or the
// context expires, then decodes a JSON array of strings. Polling is bounded by
// the caller's context — the process is killed either way.
func getJSONStringsUntilReady(ctx context.Context, url string) ([]string, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	var lastErr error
	for {
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return nil, lastErr
			}
			return nil, ctx.Err()
		default:
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(300 * time.Millisecond)
			continue
		}
		var names []string
		decodeErr := json.NewDecoder(resp.Body).Decode(&names)
		_ = resp.Body.Close()
		if decodeErr != nil {
			return nil, fmt.Errorf("decode tool ids: %w", decodeErr)
		}
		return names, nil
	}
}

func freeLoopbackPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		return 0, err
	}
	return port, nil
}

// hermesMCPToolPrefix and hermesUnsafeToolChar reproduce how hermes renames an
// MCP tool before it calls it: `mcp_<server>_<tool>`, with every character
// outside [A-Za-z0-9_] replaced by an underscore (hermes-agent
// tools/mcp_tool.py, sanitize_mcp_name_component). Measured on 2026-08-07 in a
// live `hermes acp` run against the same `multica mcp serve` process the daemon
// hands hermes: the MCP tool get_me arrived as mcp_multica_get_me.
const hermesMCPToolPrefix = "mcp_multica_"

var hermesUnsafeToolChar = regexp.MustCompile(`[^A-Za-z0-9_]`)

// probeHermesTools measures the Multica MCP channel and spells every answer the
// way hermes spells it at call time. Hermes' own built-ins are not on this
// channel and no hermes command lists them — they are the Native names on
// providerInventories["hermes"].
func probeHermesTools(ctx context.Context, _ string) ([]string, error) {
	tools, err := probeMulticaMCPTools(ctx, "")
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, hermesMCPToolPrefix+hermesUnsafeToolChar.ReplaceAllString(tool, "_"))
	}
	return names, nil
}

func dedupeToolNames(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, name := range in {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}
