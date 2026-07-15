package autopilotmodel

import (
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestValidate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"empty is allowed (clears override)", "", false},
		{"haiku", ModelHaiku, false},
		{"sonnet", ModelSonnet, false},
		{"opus", ModelOpus, false},
		{"unknown model", "gpt-4", true},
		{"close-but-not-quite", "claude-sonnet-4", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := Validate(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate(%q) err=%v wantErr=%v", tc.input, err, tc.wantErr)
			}
			if tc.wantErr && !errors.Is(err, ErrUnknownModel) {
				t.Fatalf("error must wrap ErrUnknownModel, got %v", err)
			}
			if tc.wantErr && !strings.Contains(err.Error(), tc.input) {
				t.Fatalf("error must include offending value, got %q", err.Error())
			}
		})
	}
}

func TestResolve(t *testing.T) {
	t.Parallel()
	textValid := func(s string) pgtype.Text { return pgtype.Text{String: s, Valid: true} }
	textNull := pgtype.Text{}

	cases := []struct {
		name         string
		taskOverride pgtype.Text
		agentModel   pgtype.Text
		want         string
	}{
		{"task override wins over agent default", textValid(ModelHaiku), textValid(ModelSonnet), ModelHaiku},
		{"falls back to agent when no override", textNull, textValid(ModelSonnet), ModelSonnet},
		{"empty override is ignored", textValid(""), textValid(ModelSonnet), ModelSonnet},
		{"both null returns empty (CLI default)", textNull, textNull, ""},
		{"both empty returns empty", textValid(""), textValid(""), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Resolve(tc.taskOverride, tc.agentModel)
			if got != tc.want {
				t.Fatalf("Resolve(%+v, %+v) = %q want %q", tc.taskOverride, tc.agentModel, got, tc.want)
			}
		})
	}
}

func TestRuntimeBriefLine(t *testing.T) {
	t.Parallel()
	if got := RuntimeBriefLine(""); got != "" {
		t.Fatalf("empty model should yield empty line, got %q", got)
	}
	got := RuntimeBriefLine(ModelHaiku)
	if !strings.Contains(got, ModelHaiku) {
		t.Fatalf("brief line must include the model id, got %q", got)
	}
}

// The named constants are shorthand for entries in the provider catalog, not a
// second source of truth. If the catalog drops or renames one, every caller
// using the constant silently pins a model no runtime accepts — exactly the
// FIR-3287 failure. Fail here instead.
func TestNamedModelsResolveInClaudeCatalog(t *testing.T) {
	t.Parallel()
	for _, model := range []string{ModelHaiku, ModelSonnet, ModelOpus} {
		if err := ValidateForProvider("claude", model); err != nil {
			t.Errorf("named constant %q is not in the claude catalog: %v", model, err)
		}
	}
}

// FIR-3287: a wakeup pinned claude-haiku on an agent whose runtime is Codex.
// Create accepted it (the old registry only knew Anthropic IDs and never looked
// at the runtime), then the woken run died on a 400 from the Codex CLI:
// "The 'claude-haiku-4-5-20251001' model is not supported when using Codex with
// a ChatGPT account." The model must be checked against the provider that runs it.
func TestValidateForProviderRejectsCrossProviderModel(t *testing.T) {
	t.Parallel()
	err := ValidateForProvider("codex", ModelHaiku)
	if !errors.Is(err, ErrModelNotOnProvider) {
		t.Fatalf("ValidateForProvider(codex, %q) = %v, want ErrModelNotOnProvider", ModelHaiku, err)
	}
	// The message has to name the models that WOULD work, otherwise the caller
	// only learns what not to do.
	if !strings.Contains(err.Error(), "gpt-5.5") {
		t.Errorf("error should list the codex catalog, got %q", err)
	}
}

func TestValidateForProviderAcceptsOwnProviderModel(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ provider, model string }{
		{"claude", ModelHaiku},
		{"codex", "gpt-5.5"},
		{"openai-eu", "gpt-4o"},
	} {
		if err := ValidateForProvider(tc.provider, tc.model); err != nil {
			t.Errorf("ValidateForProvider(%q, %q) = %v, want nil", tc.provider, tc.model, err)
		}
	}
}

// A provider whose real catalog is only knowable by asking the live runtime must
// not be second-guessed — rejecting here would block a model that works.
func TestValidateForProviderAllowsUncataloguedProvider(t *testing.T) {
	t.Parallel()
	if err := ValidateForProvider("hermes", "some/model-we-cannot-enumerate"); err != nil {
		t.Errorf("uncatalogued provider should accept any model, got %v", err)
	}
}

// Empty means "no override, use the agent's own model" everywhere.
func TestEmptyModelAlwaysPasses(t *testing.T) {
	t.Parallel()
	if err := ValidateForProvider("codex", ""); err != nil {
		t.Errorf("ValidateForProvider with empty model = %v, want nil", err)
	}
	if !SupportedByProvider("codex", "") {
		t.Error("SupportedByProvider with empty model = false, want true")
	}
}

// Validate is the provider-agnostic union check, used where the target runtime
// is unknown. It must span providers — a Codex agent pinning a real GPT model
// used to be rejected as "not in registry" by the Anthropic-only list.
func TestValidateSpansProviders(t *testing.T) {
	t.Parallel()
	for _, model := range []string{ModelHaiku, "gpt-5.5"} {
		if err := Validate(model); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", model, err)
		}
	}
	if err := Validate("not-a-real-model"); !errors.Is(err, ErrUnknownModel) {
		t.Errorf("Validate(nonsense) = %v, want ErrUnknownModel", err)
	}
}
