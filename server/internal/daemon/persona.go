package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

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
// Returns nil when persona is disabled at the daemon level OR when the
// agent has no persona_sandbox set (existing behaviour applies).
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
func (d *Daemon) preparePersonaSpawn(ctx context.Context, agent *AgentData, subject spawnPersonaSubject, workdir string, taskLog *slog.Logger) (*personaSpawn, error) {
	if !d.personaEnabled() {
		return nil, nil
	}
	if agent == nil || agent.PersonaSandbox == "" {
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
	actorName := personaActorName(agent)
	actorID, _, err := client.GetOrCreateActor(ctx, actorName, "Agent", "")
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
		"sandbox", agent.PersonaSandbox,
		"profile_sandbox", profile.SandboxName,
		"allowed_tools", profile.AllowedTools,
	)

	// Ensure the agent's actor has the requested sandbox attached. If the
	// operator changed persona_sandbox on the Multica side, this brings
	// persona in sync. We only PUT when the current assignment differs to
	// avoid spurious activity-log entries.
	if profile.SandboxName != agent.PersonaSandbox {
		// Look up sandboxes by name to find the ID. The SDK doesn't yet have
		// an "assign by name" helper; the Multica daemon does this lazily here
		// so operators don't need to know UUIDs.
		// Done via a direct request to keep the SDK surface small.
		if err := assignSandboxByName(ctx, d.cfg.PersonaURL, d.cfg.PersonaToken, actorID, agent.PersonaSandbox); err != nil {
			return nil, fmt.Errorf("assign sandbox %q to actor %s: %w", agent.PersonaSandbox, actorID, err)
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

	return &personaSpawn{
		ActorID:      actorID,
		ActorName:    actorName,
		SandboxName:  profile.SandboxName,
		SettingsPath: settingsPath,
		Env:          env,
	}, nil
}

// spawnIDKey is a unique context key for spawn-id propagation. Future task
// loops can stash a per-task ID here so persona's activity-log lines tie
// back to a specific Multica task.
type spawnIDKey struct{}

// personaActorName produces the persona-actor name we use for an agent.
// Format: "cerebro:agent:<agent-id>" so the actor is unambiguously bound
// to the Multica agent it gates and the operator can tell which is which
// in persona's UI without cross-referencing UUIDs.
func personaActorName(a *AgentData) string {
	if a == nil {
		return "cerebro:agent:unknown"
	}
	return fmt.Sprintf("cerebro:agent:%s", a.ID)
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

// assignSandboxByName issues PUT /v1/actors/{id}/sandbox using the sandbox
// name. Looks up the sandbox ID via /v1/sandboxes since the API takes a UUID,
// not a name. Kept inside daemon (not the SDK) so the SDK surface stays
// minimal — daemon is the only consumer that needs name-based assignment.
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
