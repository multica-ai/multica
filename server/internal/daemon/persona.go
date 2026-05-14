package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
// CEREBRO-PATCH(persona): persona integration changes.
	"strings"
	"time"

	"github.com/hvejsel/firtal-persona/sdk/go"
)

// personaSpawn carries everything needed to gate a spawned agent through
// persona: the actor we spawn as, the env we inject, and the settings.json
// path Claude Code reads at startup. Returned from preparePersonaSpawn for
// the caller (runTask) to inject into agent.Config.
type personaSpawn struct {
	ActorID      string
	ActorName    string
	SandboxName  string
	SettingsPath string // absolute path; empty = no settings file written
	Env          map[string]string
	// finishHook clears the per-spawn fields (workdir, etc.) on the actor's
	// attributes when the task completes. Called by runTask in defer so it
	// runs regardless of how the task ended. nil-safe.
	finishHook func(ctx context.Context)
}

// spawnPersonaSubject is the on-the-wire JEH-1080 spawn-subject the server
// resolves at claim time. The daemon forwards it to the persona-hook via
// env so the hook can include the facts in CheckResource args.
type spawnPersonaSubject struct {
	UserID   string
	GroupIDs []string
}

// personaEnabled reports whether the daemon is configured for persona.
// All three pieces (URL, token, hook binary) are required — any missing
// piece falls back to existing behaviour for ALL agents.
func (d *Daemon) personaEnabled() bool {
	return d.cfg.PersonaURL != "" && d.cfg.PersonaToken != "" && d.cfg.PersonaHookBin != ""
}

// preparePersonaSpawn does the daemon's half of the persona integration.
// Returns nil when persona is disabled at the daemon level OR when neither
// the agent nor the runtime has a persona_sandbox set (existing behaviour
// applies).
//
// E1 enforcement: when runtimePersonaSandbox is non-empty, it is used as
// the hard upper bound — the agent-level value is ignored. This is the
// operator-set runtime cap; bypassing it by switching agent sandboxes must
// be impossible. When the runtime has no cap, the agent's own sandbox
// applies. The DB writes that gate runtime_persona_sandbox to workspace
// admins (E1.b) make this safe; the daemon trusts the value from claim.
//
// On success, writes a settings.json into workdir/.claude/ with the
// PreToolUse hook configured, and returns the env-var bundle the caller
// should merge into the spawn process environment.
//
// JEH-1080: subject carries the spawning user + their group memberships,
// resolved at server-side claim time. The hook reads MULTICA_SPAWNER_USER_ID
// and MULTICA_SPAWNER_GROUP_IDS from the env and includes them as facts in
// CheckResource args so persona's group-grant resolver can match. A
// zero-value subject means "no group facts" and group grants therefore
// never match — fail-safe.
//
// Fail-closed semantics: any failure to reach persona, look up the actor,
// or fetch the profile results in an error — the spawn must NOT proceed
// with a half-configured hook (better to fail loud than silently bypass).
func (d *Daemon) preparePersonaSpawn(ctx context.Context, agent *AgentData, subject spawnPersonaSubject, runtimePersonaSandbox, provider, workdir string, taskLog *slog.Logger) (*personaSpawn, error) {
	if !d.personaEnabled() {
		return nil, nil
	}

	// E1: runtime cap wins when set. Otherwise fall back to the agent's
	// own sandbox. If neither is set, persona is opt-out for this task.
	agentSandbox := ""
	if agent != nil {
		agentSandbox = agent.PersonaSandbox
	}
	effectiveSandbox, source := chooseEffectivePersonaSandbox(runtimePersonaSandbox, agentSandbox)
	if effectiveSandbox == "" || agent == nil {
		return nil, nil
	}

	if _, err := os.Stat(d.cfg.PersonaHookBin); err != nil {
		return nil, fmt.Errorf("persona hook binary not found at %s: %w", d.cfg.PersonaHookBin, err)
	}

	client, err := persona.New(persona.Config{
		Endpoint: d.cfg.PersonaURL,
		Token:    d.cfg.PersonaToken,
	})
	if err != nil {
		return nil, fmt.Errorf("init persona client: %w", err)
	}

	// Map agent → persona actor by name. Auto-create on first sight so
	// operators don't have to pre-provision actors in persona.
	//
	// Attributes (E3 part 3): we push the runtime context every spawn so
	// persona's UI can show "this actor's runtime supports Bash, Read, ..."
	// before any tool is invoked. Refreshing on every spawn keeps the
	// snapshot live (workdir changes per task; provider/tools don't, but
	// re-pushing is cheap and removes a "stale on first run" edge case).
	actorName := personaActorName(agent)

	// W4.1 — soft-delete stale actors that share this agent's uuid8 suffix
	// but a different slug. Happens after an agent rename: the slug shifts
	// from "ceo-agent-72995498" to "cto-agent-72995498" and the old actor
	// becomes orphaned. Best-effort: a list/delete failure here logs but
	// does NOT block the spawn — accumulating cruft is preferable to
	// failing the task on a side concern.
	deleteStalePersonaActors(ctx, client, actorName, agent.ID, taskLog)

	attrs := buildActorAttributes(agent, provider, workdir, effectiveSandbox, source)
	actorID, _, err := client.GetOrCreateActorWithAttributes(ctx, actorName, "Agent", "", attrs)
	if err != nil {
		return nil, fmt.Errorf("resolve persona actor %q: %w", actorName, err)
	}

	// Verify the named sandbox exists by fetching the profile. If the
	// operator typed a sandbox that persona doesn't recognise, fail here
	// rather than at spawn time.
	profile, err := client.GetSandboxProfile(ctx, actorID)
	if err != nil {
		return nil, fmt.Errorf("fetch sandbox profile for %s: %w", actorID, err)
	}
	taskLog.Info("persona spawn prepared",
		"actor", actorName,
		"actor_id", actorID,
		"sandbox", effectiveSandbox,
		"sandbox_source", source,
		"profile_sandbox", profile.SandboxName,
		"allowed_tools", profile.AllowedTools,
	)

	// Ensure the agent's actor has the requested sandbox attached. If the
	// operator changed persona_sandbox on the Multica side, this brings
	// persona in sync. We only PUT when the current assignment differs to
	// avoid spurious activity-log entries.
	if profile.SandboxName != effectiveSandbox {
		// Look up sandboxes by name to find the ID. The SDK doesn't yet have
		// an "assign by name" helper; the Multica daemon does this lazily here
		// so operators don't need to know UUIDs.
		// Done via a direct request to keep the SDK surface small.
		if err := assignSandboxByName(ctx, d.cfg.PersonaURL, d.cfg.PersonaToken, actorID, effectiveSandbox); err != nil {
			return nil, fmt.Errorf("assign sandbox %q to actor %s: %w", effectiveSandbox, actorID, err)
		}
		// Re-fetch so the spawn-time profile reflects the assignment.
		profile, err = client.GetSandboxProfile(ctx, actorID)
		if err != nil {
			return nil, fmt.Errorf("re-fetch sandbox profile after assignment: %w", err)
		}
	}

	settingsPath, err := writePersonaSettingsJSON(workdir, d.cfg.PersonaHookBin)
	if err != nil {
		return nil, fmt.Errorf("write persona settings.json: %w", err)
	}

	spawnID := fmt.Sprintf("agent-%s-%d", agent.ID, ctx.Value(spawnIDKey{}))

	env := map[string]string{
		"PERSONA_URL":           d.cfg.PersonaURL,
		"PERSONA_SERVICE_TOKEN": d.cfg.PersonaToken,
		"PERSONA_ACTOR_ID":      actorID,
		"PERSONA_SPAWN_ID":      spawnID,
	}
	// JEH-1080: forward the spawning user + groups so the hook can carry
	// them as facts in CheckResource args. Empty values stay out of the env
	// so a daemon without group support (older server build) leaves the
	// hook in its pre-1080 behaviour.
	if subject.UserID != "" {
		env["MULTICA_SPAWNER_USER_ID"] = subject.UserID
	}
	if len(subject.GroupIDs) > 0 {
		env["MULTICA_SPAWNER_GROUP_IDS"] = strings.Join(subject.GroupIDs, ",")
	}

	// Build the "task ended" attribute snapshot once, captured in the closure.
	// Same shape as the spawn-time attrs minus workdir — persona's UI shows
	// "currently idle" when workdir is absent, instead of a stale path that
	// looks like an in-flight task.
	idleAttrs := buildActorAttributes(agent, provider, "", effectiveSandbox, source)
	finishURL := d.cfg.PersonaURL
	finishToken := d.cfg.PersonaToken

	return &personaSpawn{
		ActorID:      actorID,
		ActorName:    actorName,
		SandboxName:  profile.SandboxName,
		SettingsPath: settingsPath,
		Env:          env,
		finishHook: func(finishCtx context.Context) {
			finishClient, err := persona.New(persona.Config{Endpoint: finishURL, Token: finishToken})
			if err != nil {
				taskLog.Warn("persona finish hook: client init failed", "error", err)
				return
			}
			if err := finishClient.PutActorAttributes(finishCtx, actorID, idleAttrs); err != nil {
				// Best-effort: log but don't fail the task — the task already
				// completed by the time this runs, and a stale workdir is a
				// UI quirk not a correctness issue.
				taskLog.Warn("persona finish hook: clear workdir failed", "actor_id", actorID, "error", err)
			}
		},
	}, nil
}

// spawnIDKey is a unique context key for spawn-id propagation. Future task
// loops can stash a per-task ID here so persona's activity-log lines tie
// back to a specific Multica task.
type spawnIDKey struct{}

// mcpServerNames extracts the configured MCP server names from an agent's
// raw mcp_config JSON. The expected shape is the standard Claude Code one:
//
//	{ "mcpServers": { "supabase": {...}, "stripe": {...} } }
//
// Returns the keys of "mcpServers" in sorted order so persona's UI shows
// a stable list. Returns an empty (non-nil) slice for nil/empty/malformed
// input or when the JSON object has no "mcpServers" key — persona's UI
// iterates over the value and serialises it as a JSON array, so nil would
// surface as `null` and break the rendering.
func mcpServerNames(raw json.RawMessage) []string {
	out := []string{}
	if len(raw) == 0 {
		return out
	}
	var parsed struct {
		McpServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return out
	}
	if len(parsed.McpServers) == 0 {
		return out
	}
	for name := range parsed.McpServers {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// buildActorAttributes assembles the runtime-context snapshot we push to
// persona on every spawn (E3 part 3). The shape is loose so persona's UI
// can grow new sections without a coordinated cross-repo change.
//
// Tools come from providerCapabilities — the same static list the daemon
// reports to Multica's /api/runtimes/register at boot. Keeping a single
// source means the two surfaces (Multica's runtime view + persona's actor
// view) never disagree about what the runtime can do.
//
// mcp_servers is per-agent: when the agent has its own mcp_config we
// surface those names; otherwise we fall back to providerCapabilities'
// (currently empty) static list. This gives persona's UI a real picture
// of what MCP servers each actor's runtime is configured with, instead
// of the misleading "[]" the static list always reports.
func buildActorAttributes(agent *AgentData, provider, workdir, sandbox, source string) map[string]any {
	if agent == nil {
		return map[string]any{}
	}
	caps := providerCapabilities(provider)
	tools, _ := caps["tools"].([]string)
	mcp, _ := caps["mcp_servers"].([]string)
	if names := mcpServerNames(agent.McpConfig); len(names) > 0 {
		mcp = names
	}
	attrs := map[string]any{
		"source":           "cerebro",
		"agent_id":         agent.ID,
		"agent_name":       agent.Name,
		"runtime_provider": provider,
		"runtime_tools":    tools,
		"mcp_servers":      mcp,
		"workdir":          workdir,
	}
	if sandbox != "" {
		attrs["effective_sandbox"] = sandbox
		attrs["sandbox_source"] = source // "runtime" | "agent"
	}
	return attrs
}

// refreshPersonaActorAttrs walks every persona-gated agent in a workspace
// and pushes a fresh attribute snapshot to its persona actor. Called on
// daemon start (per workspace) so persona's UI reflects the current
// capability list — e.g. a daemon upgrade that added new tools shows up
// in persona without waiting for the next spawn. Best-effort: a single
// agent failing logs a warning but does not abort the loop.
func (d *Daemon) refreshPersonaActorAttrs(ctx context.Context, workspaceID string) {
	apiCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	agents, err := d.client.ListWorkspacePersonaAgents(apiCtx, workspaceID)
	if err != nil {
		d.logger.Warn("persona refresh: list agents failed", "workspace_id", workspaceID, "error", err)
		return
	}
	if len(agents) == 0 {
		return
	}

	pc, err := persona.New(persona.Config{Endpoint: d.cfg.PersonaURL, Token: d.cfg.PersonaToken})
	if err != nil {
		d.logger.Warn("persona refresh: client init failed", "error", err)
		return
	}

	refreshed := 0
	for _, a := range agents {
		// We re-use the same actor name + attrs builder the spawn-time path
		// uses, with workdir empty since no task is running yet. A single
		// failed agent must not stop the others; persona's UI just shows
		// stale attrs for that one until the next spawn.
		agentData := &AgentData{ID: a.ID, Name: a.Name, PersonaSandbox: a.PersonaSandbox, McpConfig: a.McpConfig}
		actorName := personaActorName(agentData)
		effSandbox, source := chooseEffectivePersonaSandbox(a.RuntimePersonaSandbox, a.PersonaSandbox)
		attrs := buildActorAttributes(agentData, a.Provider, "", effSandbox, source)

		_, _, err := pc.GetOrCreateActorWithAttributes(apiCtx, actorName, "Agent", "", attrs)
		if err != nil {
			d.logger.Warn("persona refresh: actor sync failed",
				"workspace_id", workspaceID,
				"agent_id", a.ID,
				"agent_name", a.Name,
				"actor_name", actorName,
				"error", err,
			)
			continue
		}
		refreshed++
	}
	d.logger.Info("persona refresh: actor attrs synced",
		"workspace_id", workspaceID,
		"agents", len(agents),
		"refreshed", refreshed,
	)
}

// chooseEffectivePersonaSandbox picks the persona sandbox name that should
// gate this spawn. E1: a non-empty runtime cap always wins over the agent's
// own value — the cap is operator-set policy, the agent value is config the
// agent owner controls. Returns ("", "") when neither is set, signalling
// "no persona gating, follow existing pre-persona behaviour".
//
// Pulled out as a pure function so the precedence rule is unit-testable
// without spinning up persona / writing files / faking HTTP.
func chooseEffectivePersonaSandbox(runtime, agent string) (sandbox, source string) {
	if runtime != "" {
		return runtime, "runtime"
	}
	if agent != "" {
		return agent, "agent"
	}
	return "", ""
}

// personaActorName produces the persona-actor name we use for an agent.
// Format: "cerebro:agent:<slug>:<short-uuid>" — the slug is human-readable
// in persona's UI (so an operator can pick the right actor without
// cross-referencing Multica), and the short-uuid suffix keeps the name
// stable when two agents are renamed the same. Pure UUIDs were unreadable
// in the activity log and on the actors-list page; slug-only would
// collide on rename.
func personaActorName(a *AgentData) string {
	if a == nil {
		return "cerebro:agent:unknown"
	}
	slug := slugifyAgentName(a.Name)
	if slug == "" {
		slug = "agent"
	}
	suffix := a.ID
	if len(suffix) > 8 {
		suffix = suffix[:8]
	}
	return fmt.Sprintf("cerebro:agent:%s-%s", slug, suffix)
}

// deleteStalePersonaActors finds actors whose name shares this agent's
// uuid8 suffix but differs from the current slug, and soft-deletes them.
// W4.1 in docs/persona-finish-plan.md — without this, every agent rename
// leaves a permanent orphan actor in persona, and the actors-list page
// gradually fills with old "ceo-agent-…", "cto-agent-…" entries.
//
// Best-effort: any HTTP failure logs at warn-level and returns; the
// caller's spawn proceeds either way.
func deleteStalePersonaActors(ctx context.Context, client *persona.Client, currentName, agentID string, taskLog *slog.Logger) {
	suffix := agentID
	if len(suffix) > 8 {
		suffix = suffix[:8]
	}
	if suffix == "" {
		return
	}
	// Match the persona actor format: "cerebro:agent:<slug>-<suffix>".
	// We only soft-delete actors whose name ends in "-<suffix>" AND starts
	// with our prefix — a 4-byte UUID8 collision with an unrelated actor
	// is unlikely but the prefix check makes it impossible.
	const prefix = "cerebro:agent:"
	suffixWithDash := "-" + suffix

	all, err := client.ListActors(ctx)
	if err != nil {
		taskLog.Warn("persona stale-actor cleanup: list failed", "error", err)
		return
	}
	for _, a := range all {
		if a.Name == currentName {
			continue
		}
		if a.Status == "deleted" {
			continue
		}
		if !strings.HasPrefix(a.Name, prefix) {
			continue
		}
		if !strings.HasSuffix(a.Name, suffixWithDash) {
			continue
		}
		if err := client.DeleteActor(ctx, a.ID); err != nil {
			taskLog.Warn("persona stale-actor cleanup: delete failed", "actor", a.Name, "actor_id", a.ID, "error", err)
			continue
		}
		taskLog.Info("persona stale-actor cleanup: soft-deleted",
			"old_name", a.Name,
			"new_name", currentName,
			"actor_id", a.ID,
		)
	}
}

// slugifyAgentName collapses an agent's display name into a persona-friendly
// identifier: lowercase, [a-z0-9-] only, no leading/trailing dashes, max 40
// chars. Persona's actor name has a UNIQUE constraint, so the daemon also
// suffixes a short UUID — slugify only has to be readable, not unique.
func slugifyAgentName(name string) string {
	var b []rune
	prevDash := true // start true so we strip leading separators
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b = append(b, r)
			prevDash = false
		case r >= 'A' && r <= 'Z':
			b = append(b, r+('a'-'A'))
			prevDash = false
		default:
			if !prevDash && len(b) > 0 {
				b = append(b, '-')
				prevDash = true
			}
		}
		if len(b) >= 40 {
			break
		}
	}
	for len(b) > 0 && b[len(b)-1] == '-' {
		b = b[:len(b)-1]
	}
	return string(b)
}

// writePersonaSettingsJSON drops a settings.json into <workdir>/.claude/
// that wires the cerebro-persona-hook for every PreToolUse event. Claude
// Code reads it via --settings (we pass the absolute path to the spawn).
func writePersonaSettingsJSON(workdir, hookBin string) (string, error) {
	dir := filepath.Join(workdir, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	settings := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []map[string]any{
				{
					"matcher": "*",
					"hooks": []map[string]any{
						{"type": "command", "command": hookBin},
					},
				},
			},
		},
	}
	data, _ := json.MarshalIndent(settings, "", "  ")
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// AssignSandboxByName issues PUT /v1/actors/{id}/sandbox using the sandbox
// name. Looks up the sandbox ID via /v1/sandboxes since the API takes a UUID,
// not a name. Kept inside daemon (not the SDK) so the SDK surface stays
// minimal — daemon is the primary consumer that needs name-based assignment.
// Exported because the e2e-spawn CLI uses the same helper to simulate the
// runtime-cap precedence (E1) end-to-end.
func AssignSandboxByName(ctx context.Context, baseURL, token, actorID, sandboxName string) error {
	return assignSandboxByName(ctx, baseURL, token, actorID, sandboxName)
}

// assignSandboxByName: see AssignSandboxByName for the public version.
func assignSandboxByName(ctx context.Context, baseURL, token, actorID, sandboxName string) error {
	listURL := baseURL + "/v1/sandboxes"
	req, err := newAuthedJSONRequest(ctx, "GET", listURL, token, nil)
	if err != nil {
		return err
	}
	resp, err := personaHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("list sandboxes: status %d", resp.StatusCode)
	}
	var listBody struct {
		Sandboxes []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"sandboxes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listBody); err != nil {
		return err
	}
	var sandboxID string
	for _, sb := range listBody.Sandboxes {
		if sb.Name == sandboxName {
			sandboxID = sb.ID
			break
		}
	}
	if sandboxID == "" {
		return fmt.Errorf("sandbox %q not found in persona", sandboxName)
	}

	body, _ := json.Marshal(map[string]string{"sandbox_id": sandboxID})
	assignURL := fmt.Sprintf("%s/v1/actors/%s/sandbox", baseURL, actorID)
	areq, err := newAuthedJSONRequest(ctx, "PUT", assignURL, token, body)
	if err != nil {
		return err
	}
	aresp, err := personaHTTP.Do(areq)
	if err != nil {
		return err
	}
	defer aresp.Body.Close()
	if aresp.StatusCode != 200 {
		return fmt.Errorf("assign sandbox: status %d", aresp.StatusCode)
	}
	return nil
}

var _ = errors.New // keep errors import for future use
