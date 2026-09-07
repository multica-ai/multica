package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ── Claude model discovery ───────────────────────────────────────────
//
// Claude Code has no `--list-models` flag and no models subcommand, which is
// why this package carried a hand-maintained claudeStaticModels() for so long.
// It does have a discovery hook, just not on the command line: the stream-json
// control protocol answers a `list_models` control request with the same
// ModelInfo rows the interactive /model picker renders.
//
// That distinction is the whole point of MUL-6961. A static catalog cannot know
// what the CLI in front of it can actually run, so every new Anthropic model
// opened a window where Multica offered a model that 400s ("Claude Code 2.1.246
// does not support this model; version 2.1.251 or newer is required"). The
// control protocol closes the window at the source: the answer is computed by
// the installed binary, against the logged-in account, so a model the local CLI
// cannot run is either absent or explicitly flagged disabled. Verified against
// 2.1.223 / 2.1.228 / 2.1.246 / 2.1.258 — the three older builds return Fable
// 5.1 as a disabled row carrying "Update to 2.1.255+ to use Fable 5.1", and
// 2.1.258 returns it as a normal selectable entry.
//
// This governs SELECTION, and only selection. An agent already configured with
// a model, whose CLI is downgraded afterwards, still starts and still gets the
// upstream 400 — discovery cannot reach a value already persisted. That is the
// accepted boundary, decided on MUL-6961 rather than assumed: the downgrade is
// a rare case, the upstream error is accurate, and a run that fails loudly at
// the moment it starts is a fine way to report it. Do not "improve" this by
// clearing the configured model or substituting a runnable one when the catalog
// no longer offers it. Clearing turns a legible error into a silent config
// change the user did not make, and substituting is worse — it succeeds, using
// a model nobody chose.
//
// No version gate guards the request, unlike codexSupportsDebugModels. It would
// buy nothing: an unsupported subtype is answered, not ignored — every build
// tested replies `Unsupported control request subtype: ...` in about two
// seconds and exits 0 — so an old CLI costs one cheap round trip and falls back,
// with no risk of hanging until the timeout. Gating instead on a version floor
// would mean hand-maintaining exactly the kind of number this change exists to
// stop hand-maintaining.
//
// "One round trip" is only true because that specific answer is remembered; see
// claudeListModelsUnsupported below for why it has to be.

// claudeListModelsArgs is the argv for a discovery-only Claude session.
//
// `--print` with stream-json on both ends is the control-protocol channel the
// daemon already speaks for task execution. No user message is ever written, so
// no model is invoked and nothing is billed: the process answers the control
// request, sees stdin close, and exits 0.
//
// `--strict-mcp-config` without any `--mcp-config` resolves to "no MCP servers
// at all". Discovery has no use for the user's servers and every reason not to
// boot them — a stdio server that spawns a container would make enumerating a
// model list arbitrarily slow and arbitrarily side-effecting.
//
// Kept as a package-level var rather than a literal at the call site so tests
// can pin the exact argv a real `claude` invocation receives; the argv shape is
// as much of the contract as the parser is.
var claudeListModelsArgs = []string{
	"--print",
	"--verbose",
	"--input-format", "stream-json",
	"--output-format", "stream-json",
	"--strict-mcp-config",
}

// claudeListModelsRequestID labels our control request so the reply can be
// picked out of the stream. Claude answers a discovery-only session in a single
// stdout line today, but matching on the id keeps that from being load-bearing.
const claudeListModelsRequestID = "multica-list-models"

// claudeListModelsTimeout bounds one discovery round trip.
//
// Measured on an M-series laptop over five runs each: 1.57–2.19s against 2.1.258
// and 1.64–1.79s against 2.1.246, plus ~0.2s against an empty config dir. The
// ceiling is ~10x the observed worst case because the failure it exists for is
// not a slow answer but no answer — a wrapper script that never execs, or a CLI
// wedged on a keychain prompt — and the machines that hit that are the loaded,
// cold-cache ones the measurements above do not represent. Nothing user-facing
// blocks on it: exceeding it degrades to the static catalog.
const claudeListModelsTimeout = 20 * time.Second

// errClaudeListModelsUnsupported marks the one discovery failure that is a
// property of the binary rather than a bad moment: the CLI answered, and its
// answer was that it does not know this control request. Distinguishing it is
// what lets discoverClaudeCatalog remember it, and remembering it is not an
// optimisation — every Claude task carrying a thinking_level reads the catalog
// before it starts (see the ValidateThinkingLevelWith call in the daemon), and
// cachedDiscovery deliberately refuses to memoise a fallback result (#3729,
// MUL-5549). Without this, "one cheap round trip" would mean one round trip per
// task, forever, on exactly the old installs that can never succeed.
var errClaudeListModelsUnsupported = errors.New("claude CLI does not support the list_models control request")

// claudeUnsupportedSubtypeMarker is the substring Claude Code uses to say a
// control request subtype is unknown to it ("Unsupported control request
// subtype: list_models"), verified on 2.1.223 and 2.1.258.
//
// Matching on upstream prose is not something to do lightly, and it is only
// safe here because of which way it fails. A miss means the negative cache does
// not engage and behaviour is exactly what it was before — correct, slower. It
// is the opposite mistake that would hurt, caching "unsupported" for a CLI that
// merely had a bad moment, and no wording drift can produce that.
const claudeUnsupportedSubtypeMarker = "unsupported control request subtype"

// claudeCapabilityKey scopes a remembered capability answer to the exact binary
// that gave it. The CLI version is part of the key so an upgrade invalidates the
// memo immediately rather than after a TTL — the whole point is that upgrading
// is the fix we are nudging people toward, so it must take effect at once.
// Detecting the version costs ~0.01s against the ~1.7s probe it avoids.
type claudeCapabilityKey struct {
	command    string
	cliVersion string
}

const claudeCapabilityTTL = 10 * time.Minute

var (
	claudeCapabilityMu sync.Mutex
	// claudeListModelsUnsupported holds expiry times for binaries known not to
	// answer list_models. A TTL still bounds the memo for the case a version
	// string cannot distinguish — a dev build replaced in place.
	claudeListModelsUnsupported = map[claudeCapabilityKey]time.Time{}
)

func claudeListModelsKnownUnsupported(key claudeCapabilityKey) bool {
	if key.cliVersion == "" {
		// No version means no way to notice an upgrade, so nothing is
		// remembered and nothing is trusted.
		return false
	}
	claudeCapabilityMu.Lock()
	defer claudeCapabilityMu.Unlock()
	expiry, ok := claudeListModelsUnsupported[key]
	if !ok || time.Now().After(expiry) {
		return false
	}
	return true
}

func rememberClaudeListModelsUnsupported(key claudeCapabilityKey) {
	if key.cliVersion == "" {
		return
	}
	claudeCapabilityMu.Lock()
	defer claudeCapabilityMu.Unlock()
	claudeListModelsUnsupported[key] = time.Now().Add(claudeCapabilityTTL)
}

// resetClaudeCapabilityCacheForTests is exposed for tests only; production code
// relies on the TTL, the version key, or a process restart.
func resetClaudeCapabilityCacheForTests() {
	claudeCapabilityMu.Lock()
	claudeListModelsUnsupported = map[claudeCapabilityKey]time.Time{}
	claudeCapabilityMu.Unlock()
}

// claudeModelInfo is one row of the control protocol's model catalog.
//
// Value is the picker token (`sonnet`, `opus[1m]`, or the sentinel `default`);
// ResolvedModel is what that token actually runs and is what Multica persists.
// Disabled marks a row the CLI shows greyed out — visible on purpose, so the
// user learns the model exists and why it is out of reach, with Description
// carrying the runtime's own remedy.
type claudeModelInfo struct {
	Value                 string   `json:"value"`
	ResolvedModel         string   `json:"resolvedModel"`
	DisplayName           string   `json:"displayName"`
	Description           string   `json:"description"`
	SupportsEffort        bool     `json:"supportsEffort"`
	SupportedEffortLevels []string `json:"supportedEffortLevels"`
	Disabled              bool     `json:"disabled"`
}

// claudeControlResponse is the control-protocol envelope. The doubled
// `response` nesting is Claude's shape, not a transcription slip: the outer one
// is the envelope (subtype/request_id/error), the inner one is the payload.
type claudeControlResponse struct {
	Type     string `json:"type"`
	Response struct {
		Subtype   string `json:"subtype"`
		RequestID string `json:"request_id"`
		Error     string `json:"error"`
		Response  struct {
			Models []claudeModelInfo `json:"models"`
		} `json:"response"`
	} `json:"response"`
}

// claudeDefaultModelValue is the picker row meaning "whatever this CLI resolves
// to", not a model in its own right. Multica already spells that "leave the
// agent's model empty", so the row is folded into the Default flag rather than
// offered as a pick.
const claudeDefaultModelValue = "default"

// discoverClaudeCatalog answers a model-listing round for claude, preferring
// the live catalog and degrading to the hand-maintained one.
//
// The fallback is flagged, which is a change in kind rather than a detail. The
// static list used to be returned as authoritative because there was nothing to
// fall back from; now that discovery exists, a static answer means discovery
// failed, and Catalog.Fallback is what stops the server pinning that failure in
// a day-long cache (MUL-5549). The cost is that a CLI too old to answer
// list_models never gets a cached catalog — correct, and the same deal every
// other fallback provider already takes.
func discoverClaudeCatalog(ctx context.Context, runtimeCmd Command) Catalog {
	if runtimeCmd.Path == "" {
		runtimeCmd.Path = "claude"
	}
	// Detected up front because the fallback below needs it anyway
	// (annotateClaudeThinking keys its own cache on the version), so the
	// capability memo rides along for free rather than adding a probe.
	version, _ := DetectVersion(ctx, runtimeCmd)
	key := claudeCapabilityKey{command: runtimeCmd.cacheKey(), cliVersion: version}

	if claudeListModelsKnownUnsupported(key) {
		return claudeStaticCatalog(ctx, runtimeCmd)
	}

	models, unavailable, err := discoverClaudeModels(ctx, runtimeCmd)
	if err == nil {
		return Catalog{Models: models, Unavailable: unavailable}
	}
	if errors.Is(err, errClaudeListModelsUnsupported) {
		rememberClaudeListModelsUnsupported(key)
	}
	if runtimeCmd.logger != nil {
		runtimeCmd.logger.Debug("claude model discovery failed, using static catalog", "error", err)
	}
	return claudeStaticCatalog(ctx, runtimeCmd)
}

func claudeStaticCatalog(ctx context.Context, runtimeCmd Command) Catalog {
	static := claudeStaticModels()
	annotateClaudeThinking(ctx, static, runtimeCmd)
	return Catalog{Models: static, Fallback: true}
}

// discoverClaudeModels enumerates the local Claude Code catalog over the
// control protocol. A failure at any stage — spawn, timeout, malformed reply,
// error subtype, empty list — is returned as an error so the caller can fall
// back to the static catalog; no partial result is ever reported as authoritative.
func discoverClaudeModels(ctx context.Context, runtimeCmd Command) ([]Model, []UnavailableModel, error) {
	if runtimeCmd.Path == "" {
		runtimeCmd.Path = "claude"
	}

	raw, err := runClaudeListModels(ctx, runtimeCmd)
	if err != nil {
		return nil, nil, err
	}
	infos, err := parseClaudeModelCatalog(raw)
	if err != nil {
		return nil, nil, err
	}
	models, unavailable := claudeModelsFromInfos(infos)
	if len(models) == 0 {
		// Unavailable rows alone are not a catalog: with nothing selectable the
		// picker has no answer, and the static list is a better one.
		return nil, nil, errors.New("claude list_models returned no usable models")
	}
	return models, unavailable, nil
}

func runClaudeListModels(ctx context.Context, runtimeCmd Command) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, claudeListModelsTimeout)
	defer cancel()

	request, err := json.Marshal(map[string]any{
		"type":       "control_request",
		"request_id": claudeListModelsRequestID,
		"request":    map[string]any{"subtype": "list_models"},
	})
	if err != nil {
		return nil, err
	}

	cmd := runtimeCmd.exec(ctx, claudeListModelsArgs...)
	// Feeding stdin from a reader rather than a pipe is what makes this a
	// one-shot probe: os/exec closes the pipe once the request is written, the
	// CLI takes that EOF as end of session and exits on its own. Nothing here
	// has to police a lingering process.
	cmd.Stdin = strings.NewReader(string(request) + "\n")
	hideAgentWindow(cmd)
	return outputOwned(cmd, runtimeCmd.logger)
}

// parseClaudeModelCatalog pulls our reply out of the stream-json stdout.
//
// Non-JSON and unrelated lines are skipped rather than treated as corruption:
// stdout is a stream shared with whatever else the session emits, and a
// discovery run that failed to find its own answer is already reported as an
// error below.
func parseClaudeModelCatalog(raw []byte) ([]claudeModelInfo, error) {
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var resp claudeControlResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			continue
		}
		if resp.Type != "control_response" || resp.Response.RequestID != claudeListModelsRequestID {
			continue
		}
		if resp.Response.Subtype != "success" {
			// An old CLI lands here with "Unsupported control request
			// subtype: list_models". Surfacing the runtime's own words keeps
			// the daemon log honest about which of the two failures it hit,
			// and tagging that one case lets the caller stop asking a binary
			// that has told us it cannot answer.
			reason := strings.TrimSpace(resp.Response.Error)
			if reason == "" {
				reason = resp.Response.Subtype
			}
			if strings.Contains(strings.ToLower(reason), claudeUnsupportedSubtypeMarker) {
				return nil, fmt.Errorf("%w: %s", errClaudeListModelsUnsupported, reason)
			}
			return nil, fmt.Errorf("claude list_models failed: %s", reason)
		}
		return resp.Response.Response.Models, nil
	}
	return nil, errors.New("claude list_models produced no control response")
}

// claudeModelsFromInfos projects the control-protocol rows onto the catalog.
//
// Rows are keyed by ResolvedModel, not by the picker token: two tokens
// routinely resolve to one model (`default` and `opus[1m]` both mean Opus 5
// with a 1M window), and it is the resolved name that gets persisted on the
// agent, passed to `--model`, and matched by the pricing table. The context
// window tag rides along on purpose — `claude-opus-5[1m]` is what the CLI would
// actually run for that row, so dropping the tag would quietly downgrade a user
// who picked "Opus (1M context)" to the default window.
func claudeModelsFromInfos(infos []claudeModelInfo) ([]Model, []UnavailableModel) {
	models := make([]Model, 0, len(infos))
	var unavailable []UnavailableModel
	index := make(map[string]int, len(infos))
	var defaultRow *claudeModelInfo

	for i := range infos {
		info := infos[i]
		id := claudeModelID(info)
		if id == "" {
			continue
		}
		if strings.TrimSpace(info.Value) == claudeDefaultModelValue {
			// Held back rather than emitted: Multica already spells "whatever
			// this CLI resolves to" as an empty model. Resolved after the loop,
			// once we know whether a real row carries the same model.
			defaultRow = &infos[i]
			continue
		}
		if info.Disabled {
			// Never enters models, so no capability lookup, no picker, and no
			// older client can offer it.
			unavailable = append(unavailable, UnavailableModel{
				ID:     id,
				Label:  claudeModelLabel(info, id),
				Reason: strings.TrimSpace(info.Description),
			})
			continue
		}
		if _, seen := index[id]; seen {
			continue
		}
		index[id] = len(models)
		models = append(models, Model{
			ID:       id,
			Label:    claudeModelLabel(info, id),
			Provider: "anthropic",
			Thinking: claudeThinkingFromInfo(info),
		})
	}

	if defaultRow != nil {
		if id := claudeModelID(*defaultRow); id != "" {
			if i, ok := index[id]; ok {
				models[i].Default = true
			} else if !defaultRow.Disabled {
				// The default resolves to a model no other row names — an
				// org-restricted install can look like this. Materialise it
				// instead of dropping it: without an entry flagged Default,
				// ValidateThinkingLevelWith cannot resolve an empty model and
				// fails closed, which silently discards the user's effort on
				// every default-model task.
				models = append(models, Model{
					ID:       id,
					Label:    claudeModelLabel(*defaultRow, id),
					Provider: "anthropic",
					Default:  true,
					Thinking: claudeThinkingFromInfo(*defaultRow),
				})
			}
		}
	}
	return models, unavailable
}

// claudeModelID is the identity Multica persists and passes to `--model`:
// what the picker token resolves to, falling back to the token itself.
func claudeModelID(info claudeModelInfo) string {
	if id := strings.TrimSpace(info.ResolvedModel); id != "" {
		return id
	}
	return strings.TrimSpace(info.Value)
}

func claudeModelLabel(info claudeModelInfo, id string) string {
	if label := strings.TrimSpace(info.DisplayName); label != "" {
		return label
	}
	return id
}

// claudeThinkingFromInfo builds the per-model effort catalog from the row's own
// advertisement. This is the discovery path's clearest win over the static one:
// loadClaudeThinkingByModel has to scrape `claude --help` for a global superset
// and then narrow it through claudeModelEffortAllow, a hand-kept table of which
// models really take xhigh. Here each model states its own levels.
//
// DefaultLevel is deliberately left empty. The rows carry no default-effort
// field, and empty already means "the runtime picks, we don't know" — a more
// honest answer than the static path's assumed "medium".
func claudeThinkingFromInfo(info claudeModelInfo) *ModelThinking {
	if !info.SupportsEffort || len(info.SupportedEffortLevels) == 0 {
		return nil
	}
	levels := make([]ThinkingLevel, 0, len(info.SupportedEffortLevels))
	for _, value := range info.SupportedEffortLevels {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		label, ok := claudeEffortLabel[value]
		if !ok {
			// A level this daemon has not been taught yet. Show it raw rather
			// than hide it — the CLI is the authority on what it accepts.
			label = strings.ToUpper(value[:1]) + value[1:]
		}
		levels = append(levels, ThinkingLevel{Value: value, Label: label})
	}
	if len(levels) == 0 {
		return nil
	}
	return &ModelThinking{SupportedLevels: levels}
}
