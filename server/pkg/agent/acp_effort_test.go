package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
)

// reasonixEffortSessionResult is the shape reasonix v1.21.x returns from
// `session/new`, captured from the real CLI over stdio (GitHub #6720). Note the
// option id is `effort` while the category is `thought_level`: matching either
// is what lets one parser serve reasonix and CodeBuddy.
const reasonixEffortSessionResult = `{"sessionId":"ses-reasonix",` +
	`"models":{"currentModelId":"deepseek-v4-flash",` +
	`"availableModels":[{"modelId":"deepseek-v4-flash","name":"DeepSeek V4 Flash"},` +
	`{"modelId":"deepseek-v4","name":"DeepSeek V4"}]},` +
	`"configOptions":[` +
	`{"type":"select","id":"mode","name":"Mode","currentValue":"default","options":[{"value":"default","name":"Default"}]},` +
	`{"type":"select","id":"effort","name":"Effort","category":"thought_level","currentValue":"max","options":[` +
	`{"value":"auto","name":"Auto"},{"value":"disabled","name":"Disabled"},{"value":"low","name":"Low"},` +
	`{"value":"high","name":"High"},{"value":"max","name":"Max"}]}]}`

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// ── parseACPEffortOption ─────────────────────────────────────────────

// TestParseACPEffortOptionReasonix is the case the generalization exists for:
// the levels must come through verbatim, including `auto` and `disabled`, which
// CodeBuddy's flag whitelist would have dropped.
func TestParseACPEffortOptionReasonix(t *testing.T) {
	t.Parallel()
	option, ok := parseACPEffortOption(json.RawMessage(reasonixEffortSessionResult))
	if !ok {
		t.Fatal("parseACPEffortOption found no effort option in the reasonix capture")
	}
	if option.ConfigID != "effort" {
		t.Errorf("ConfigID = %q, want effort (the id set_config_option needs)", option.ConfigID)
	}
	if got := strings.Join(option.values(), ","); got != "auto,disabled,low,high,max" {
		t.Errorf("values = %q, want all five advertised levels in order", got)
	}
	if option.CurrentValue != "max" {
		t.Errorf("CurrentValue = %q, want max", option.CurrentValue)
	}
	// Labels come from the backend's own `name` so the picker reads the way
	// reasonix's own UI does, rather than a Title-cased guess.
	if option.Choices[1].Label != "Disabled" {
		t.Errorf("Choices[1].Label = %q, want Disabled", option.Choices[1].Label)
	}
}

// TestParseACPEffortOptionSnakeCase covers the runtimes that emit snake_case
// keys; the model parser already accepts both spellings and this must match.
func TestParseACPEffortOptionSnakeCase(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{"config_options":[{"id":"thought_level","current_value":"high",` +
		`"options":[{"value":"low","name":"Low"},{"value":"high","name":"High"}]}]}`)
	option, ok := parseACPEffortOption(raw)
	if !ok {
		t.Fatal("snake_case config_options was not recognised")
	}
	if option.ConfigID != "thought_level" || option.CurrentValue != "high" {
		t.Errorf("option = %+v, want thought_level/high", option)
	}
}

// TestParseACPEffortOptionAbsent pins the Hermes v0.18.2 shape: session/new
// carries models and modes but no configOptions, so there is no dial to offer.
func TestParseACPEffortOptionAbsent(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		`{"sessionId":"s","models":{"currentModelId":"m","availableModels":[{"modelId":"m"}]},"modes":{}}`,
		`{"sessionId":"s","configOptions":[{"id":"mode","currentValue":"default","options":[{"value":"default"}]}]}`,
		`not json at all`,
	} {
		if _, ok := parseACPEffortOption(json.RawMessage(raw)); ok {
			t.Errorf("parseACPEffortOption(%s) reported an effort option where there is none", raw)
		}
	}
}

// TestParseACPEffortOptionUnusableCurrentValue: a currentValue the backend does
// not also advertise as a choice cannot be offered, so it must not become the
// default. This is the CodeBuddy `enabled` case in provider-neutral form.
func TestParseACPEffortOptionUnusableCurrentValue(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{"configOptions":[{"id":"effort","currentValue":"enabled",` +
		`"options":[{"value":"low"},{"value":"high"}]}]}`)
	option, ok := parseACPEffortOption(raw)
	if !ok {
		t.Fatal("expected the effort option to parse")
	}
	if option.CurrentValue != "" {
		t.Errorf("CurrentValue = %q, want empty so the UI shows a generic Default", option.CurrentValue)
	}
}

// TestParseACPEffortOptionNeedsAddressableID: an option matched only by
// category but carrying no id cannot be sent back to set_config_option, so
// surfacing it would build a picker with no way to apply it.
func TestParseACPEffortOptionNeedsAddressableID(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{"configOptions":[{"category":"thought_level","currentValue":"low",` +
		`"options":[{"value":"low"},{"value":"high"}]}]}`)
	if _, ok := parseACPEffortOption(raw); ok {
		t.Error("an effort option with no id must be ignored, not offered")
	}
}

// TestParseACPEffortOptionLabelFallback: a backend that omits `name` still gets
// a readable picker entry rather than a blank one.
func TestParseACPEffortOptionLabelFallback(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{"configOptions":[{"id":"effort","options":[{"value":"low"}]}]}`)
	option, ok := parseACPEffortOption(raw)
	if !ok || len(option.Choices) != 1 {
		t.Fatalf("option = %+v, ok = %v", option, ok)
	}
	if option.Choices[0].Label != "Low" {
		t.Errorf("Label = %q, want Low", option.Choices[0].Label)
	}
}

// ── annotateACPThinkingForSessionModel ───────────────────────────────

// TestAnnotateACPThinkingForSessionModel: the advertised catalog describes the
// model the session is on and nothing else, so only that model is annotated.
//
// reasonix v1.21.5 derives the effort levels from the current model's provider
// entry (`EffortCapabilityForEntry`), and the vocabularies genuinely differ —
// `deepseek-v4-flash` has `low`, `deepseek-v4-pro` does not. Copying one
// model's catalog onto its siblings would offer `low` on pro, and the runtime
// would refuse it at apply time while the task ran on at the default.
func TestAnnotateACPThinkingForSessionModel(t *testing.T) {
	t.Parallel()
	models := []Model{
		{ID: "deepseek-v4-flash", Default: true},
		{ID: "deepseek-v4-pro"},
	}
	annotateACPThinkingForSessionModel(models, json.RawMessage(reasonixEffortSessionResult))

	if models[0].Thinking == nil {
		t.Fatal("the session's current model must carry the advertised catalog")
	}
	if len(models[0].Thinking.SupportedLevels) != 5 || models[0].Thinking.DefaultLevel != "max" {
		t.Errorf("current model thinking = %+v", models[0].Thinking)
	}
	if models[1].Thinking != nil {
		t.Errorf("non-current model got %+v; its real catalog is unknown and must stay nil",
			models[1].Thinking)
	}
}

// TestAnnotateACPThinkingForSessionModelNoDefault: if nothing is marked as the
// session's current model there is no model the catalog is known to describe,
// so annotate nothing rather than guessing at the first entry.
func TestAnnotateACPThinkingForSessionModelNoDefault(t *testing.T) {
	t.Parallel()
	models := []Model{{ID: "a"}, {ID: "b"}}
	annotateACPThinkingForSessionModel(models, json.RawMessage(reasonixEffortSessionResult))
	for _, m := range models {
		if m.Thinking != nil {
			t.Errorf("model %q was annotated with no current model to anchor the catalog", m.ID)
		}
	}
}

// TestAnnotateACPThinkingForSessionModelNoOption: no advertised dial means no
// picker. A picker that cannot change anything is worse than none.
func TestAnnotateACPThinkingForSessionModelNoOption(t *testing.T) {
	t.Parallel()
	models := []Model{{ID: "m"}}
	annotateACPThinkingForSessionModel(models, json.RawMessage(`{"sessionId":"s","modes":{}}`))
	if models[0].Thinking != nil {
		t.Errorf("Thinking = %+v, want nil when the session advertises no effort option", models[0].Thinking)
	}
}

// ── applyACPEffortOption ─────────────────────────────────────────────

type recordedACPCall struct {
	method string
	params map[string]any
}

func recordingACPRequest(reply string, err error) (acpRequestFn, *[]recordedACPCall) {
	var calls []recordedACPCall
	fn := func(_ context.Context, method string, params any) (json.RawMessage, error) {
		p, _ := params.(map[string]any)
		calls = append(calls, recordedACPCall{method: method, params: p})
		if err != nil {
			return nil, err
		}
		return json.RawMessage(reply), nil
	}
	return fn, &calls
}

// TestApplyACPEffortOptionSendsAdvertisedID pins the happy path: the configId
// comes off the session, not a per-runtime constant.
func TestApplyACPEffortOptionSendsAdvertisedID(t *testing.T) {
	t.Parallel()
	echo := `{"configOptions":[{"id":"effort","currentValue":"low","options":[` +
		`{"value":"low"},{"value":"high"},{"value":"max"}]}]}`
	request, calls := recordingACPRequest(echo, nil)

	applyACPEffortOption(context.Background(), request, "reasonix", discardLogger(),
		"ses-1", json.RawMessage(reasonixEffortSessionResult), "low", true)

	if len(*calls) != 1 {
		t.Fatalf("calls = %+v, want exactly one set_config_option", *calls)
	}
	call := (*calls)[0]
	if call.method != "session/set_config_option" {
		t.Errorf("method = %q", call.method)
	}
	if call.params["configId"] != "effort" {
		t.Errorf("configId = %v, want effort (reasonix's advertised id)", call.params["configId"])
	}
	if call.params["value"] != "low" || call.params["sessionId"] != "ses-1" {
		t.Errorf("params = %+v", call.params)
	}
}

// TestApplyACPEffortOptionSkips covers every case where sending the call would
// be wrong. None of them may fail the task — the prompt still goes out.
func TestApplyACPEffortOptionSkips(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		sessionResult string
		level         string
	}{
		{
			name:          "no persisted level",
			sessionResult: reasonixEffortSessionResult,
			level:         "",
		},
		{
			name:          "session advertises no effort option",
			sessionResult: `{"sessionId":"s","modes":{}}`,
			level:         "high",
		},
		{
			// A level the server accepted against an older catalog. Sending it
			// invites a hard error on a call whose failure we swallow.
			name:          "level not advertised by this session",
			sessionResult: reasonixEffortSessionResult,
			level:         "ultra",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			request, calls := recordingACPRequest(`{}`, nil)
			applyACPEffortOption(context.Background(), request, "reasonix", discardLogger(),
				"ses-1", json.RawMessage(tc.sessionResult), tc.level, true)
			if len(*calls) != 0 {
				t.Errorf("sent %+v, want no request at all", *calls)
			}
		})
	}
}

// TestApplyACPEffortOptionResumedSessionStillApplies: when session/resume does
// echo the option back, a level change between turns must take effect rather
// than being written off as "the session already has one".
func TestApplyACPEffortOptionResumedSessionStillApplies(t *testing.T) {
	t.Parallel()
	echo := `{"configOptions":[{"id":"effort","currentValue":"low","options":[{"value":"low"},{"value":"max"}]}]}`
	request, calls := recordingACPRequest(echo, nil)

	applyACPEffortOption(context.Background(), request, "reasonix", discardLogger(),
		"ses-1", json.RawMessage(reasonixEffortSessionResult), "low", true)

	if len(*calls) != 1 {
		t.Fatalf("calls = %+v, want the level re-applied on resume", *calls)
	}
}

// TestApplyACPEffortOptionResumedWithoutOption: ACP does not require resume to
// re-advertise configOptions. The session keeps the level it was created with,
// so this is a no-op rather than a failure.
func TestApplyACPEffortOptionResumedWithoutOption(t *testing.T) {
	t.Parallel()
	request, calls := recordingACPRequest(`{}`, nil)

	applyACPEffortOption(context.Background(), request, "reasonix", discardLogger(),
		"ses-1", json.RawMessage(`{"sessionId":"ses-1"}`), "max", true)

	if len(*calls) != 0 {
		t.Errorf("sent %+v, want no request when resume advertises no option", *calls)
	}
}

// TestApplyACPEffortOptionStaleStateDefersToRuntime: after a model switch the
// advertised list describes the model the session opened on, not the one it is
// now running. Testing the level against that list would be testing it against
// the wrong model, so the check is skipped and the runtime's own answer decides.
func TestApplyACPEffortOptionStaleStateDefersToRuntime(t *testing.T) {
	t.Parallel()
	// `ultra` is absent from the session/new list. With current state that is a
	// skip; with stale state it must go out and let the runtime rule on it.
	echo := `{"configOptions":[{"id":"effort","currentValue":"ultra","options":[{"value":"ultra"}]}]}`

	request, calls := recordingACPRequest(echo, nil)
	applyACPEffortOption(context.Background(), request, "reasonix", discardLogger(),
		"ses-1", json.RawMessage(reasonixEffortSessionResult), "ultra", false)
	if len(*calls) != 1 {
		t.Fatalf("calls = %+v, want the request sent when the advertised list is stale", *calls)
	}
	if (*calls)[0].params["value"] != "ultra" {
		t.Errorf("params = %+v", (*calls)[0].params)
	}

	// Same input, state known current: the local check short-circuits it.
	request, calls = recordingACPRequest(echo, nil)
	applyACPEffortOption(context.Background(), request, "reasonix", discardLogger(),
		"ses-1", json.RawMessage(reasonixEffortSessionResult), "ultra", true)
	if len(*calls) != 0 {
		t.Errorf("sent %+v, want the unadvertised level skipped against fresh state", *calls)
	}
}

// TestApplyACPEffortOptionSurvivesUnconfirmedApply pins the Kimi ≤0.28.1 /
// Hermes shape: set_config_option answers success but the session reports a
// different level. The task must continue — the read-back exists to log the
// discrepancy, not to abort.
func TestApplyACPEffortOptionSurvivesUnconfirmedApply(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		reply string
		err   error
	}{
		{
			name:  "backend confirms a different level",
			reply: `{"configOptions":[{"id":"effort","currentValue":"auto","options":[{"value":"auto"},{"value":"max"}]}]}`,
		},
		{
			name:  "backend echoes no currentValue",
			reply: `{"ok":true}`,
		},
		{
			name: "request itself fails",
			err:  errors.New("boom"),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			request, calls := recordingACPRequest(tc.reply, tc.err)
			applyACPEffortOption(context.Background(), request, "reasonix", discardLogger(),
				"ses-1", json.RawMessage(reasonixEffortSessionResult), "max", true)
			if len(*calls) != 1 {
				t.Fatalf("calls = %+v, want the attempt to still have been made", *calls)
			}
		})
	}
}

// ── Server-side gate ─────────────────────────────────────────────────

// TestACPCatalogProviderGate: reasonix now takes the dynamic path, while an ACP
// runtime that has not opted in stays closed. Hermes is the guard case — its
// ACP surface accepts a level and ignores it (MUL-5770), so accepting one here
// would promise something no code can deliver.
func TestACPCatalogProviderGate(t *testing.T) {
	t.Parallel()
	if !ThinkingControlSupported("reasonix") {
		t.Error("reasonix should now advertise reasoning control")
	}
	for _, level := range []string{"auto", "disabled", "low", "high", "max"} {
		if !IsKnownThinkingValue("reasonix", level) {
			t.Errorf("IsKnownThinkingValue(reasonix, %q) = false, want true", level)
		}
	}
	if IsKnownThinkingValue("reasonix", "not a token") {
		t.Error("malformed tokens must still be rejected")
	}
	if ThinkingControlSupported("hermes") || IsKnownThinkingValue("hermes", "high") {
		t.Error("hermes must stay closed until its ACP surface applies an effort")
	}
	// Copilot discovers over ACP but executes through its own CLI, so it must
	// not be swept in by the generalization.
	if ThinkingControlSupported("copilot") {
		t.Error("copilot executes outside ACP; a picker here would do nothing")
	}
}
