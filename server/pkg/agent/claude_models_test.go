package agent

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// The two fixtures under testdata are verbatim stdout from a real
// `claude --print --input-format stream-json --output-format stream-json`
// answering our list_models control request. They are the whole point of
// MUL-6961 and are captured, not written by hand:
//
//   - 2.1.258 offers Fable 5.1 as a normal selectable row.
//   - 2.1.246 — the build whose 400 opened the issue — does not offer it at
//     all. It reports a disabled row instead, carrying the upstream remedy
//     ("Update to 2.1.255+ to use Fable 5.1"). That row is the reason Multica
//     does not need a per-model minimum-version table: the CLI already knows.
func loadClaudeListModelsFixture(t *testing.T, version string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "claude-code-"+version+"-list-models.jsonl"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return raw
}

func TestParseClaudeModelCatalog_RealCLIOutput(t *testing.T) {
	t.Parallel()
	infos, err := parseClaudeModelCatalog(loadClaudeListModelsFixture(t, "2.1.258"))
	if err != nil {
		t.Fatalf("parseClaudeModelCatalog: %v", err)
	}
	if len(infos) != 5 {
		t.Fatalf("got %d rows, want 5", len(infos))
	}
	if infos[0].Value != claudeDefaultModelValue {
		t.Errorf("first row = %q, want the %q sentinel", infos[0].Value, claudeDefaultModelValue)
	}
	if infos[2].ResolvedModel != "claude-fable-5-1" {
		t.Errorf("fable row resolved to %q, want claude-fable-5-1", infos[2].ResolvedModel)
	}
}

// TestParseClaudeModelCatalog_SkipsUnrelatedLines proves the parser reads a
// stream rather than a single document: stdout is shared with whatever else the
// session prints, so noise before the answer must not be mistaken for failure.
func TestParseClaudeModelCatalog_SkipsUnrelatedLines(t *testing.T) {
	t.Parallel()
	answer := string(loadClaudeListModelsFixture(t, "2.1.258"))
	raw := "not json at all\n" +
		`{"type":"system","subtype":"init","session_id":"x"}` + "\n" +
		`{"type":"control_response","response":{"subtype":"success","request_id":"someone-else","response":{"models":[]}}}` + "\n" +
		answer
	infos, err := parseClaudeModelCatalog([]byte(raw))
	if err != nil {
		t.Fatalf("parseClaudeModelCatalog: %v", err)
	}
	if len(infos) != 5 {
		t.Fatalf("got %d rows, want 5 — the reply was not matched by request id", len(infos))
	}
}

// TestParseClaudeModelCatalog_ErrorSubtype covers the old-CLI path. This is the
// reply every build without list_models sends, and it is why discovery needs no
// version gate: the failure is explicit, immediate, and cheap.
func TestParseClaudeModelCatalog_ErrorSubtype(t *testing.T) {
	t.Parallel()
	raw := `{"type":"control_response","response":{"subtype":"error","request_id":"multica-list-models","error":"Unsupported control request subtype: list_models"}}`
	_, err := parseClaudeModelCatalog([]byte(raw))
	if err == nil {
		t.Fatal("expected an error for an error-subtype response")
	}
	if !strings.Contains(err.Error(), "Unsupported control request subtype") {
		t.Errorf("error should quote the runtime verbatim, got %q", err)
	}
}

func TestParseClaudeModelCatalog_NoResponse(t *testing.T) {
	t.Parallel()
	for name, raw := range map[string]string{
		"empty":    "",
		"junk":     "hello\nworld\n",
		"wrong id": `{"type":"control_response","response":{"subtype":"success","request_id":"other","response":{"models":[]}}}`,
	} {
		if _, err := parseClaudeModelCatalog([]byte(raw)); err == nil {
			t.Errorf("%s: expected an error, got nil", name)
		}
	}
}

// TestClaudeModelsFromInfos_CurrentCLI pins the whole projection against real
// 2.1.258 output: the `default` sentinel folds into a badge rather than
// becoming a pickable row, the two rows resolving to Opus collapse to one, and
// the context-window tag survives because `claude-opus-5[1m]` is what the CLI
// would actually run for that row.
func TestClaudeModelsFromInfos_CurrentCLI(t *testing.T) {
	t.Parallel()
	infos, err := parseClaudeModelCatalog(loadClaudeListModelsFixture(t, "2.1.258"))
	if err != nil {
		t.Fatalf("parseClaudeModelCatalog: %v", err)
	}
	models, unavailable := claudeModelsFromInfos(infos)
	if len(unavailable) != 0 {
		t.Errorf("a current CLI should report nothing unavailable, got %+v", unavailable)
	}

	wantIDs := []string{
		"claude-opus-5[1m]",
		"claude-fable-5-1",
		"claude-sonnet-5",
		"claude-haiku-4-5-20251001",
	}
	gotIDs := make([]string, 0, len(models))
	for _, m := range models {
		gotIDs = append(gotIDs, m.ID)
	}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("catalog ids = %v, want %v", gotIDs, wantIDs)
	}

	if models[0].Label != "Opus (1M context)" {
		t.Errorf("opus label = %q, want the row's own display name", models[0].Label)
	}
	if !models[0].Default {
		t.Error("the model the `default` row resolves to should carry the Default badge")
	}
	for _, m := range models[1:] {
		if m.Default {
			t.Errorf("%s must not be flagged default; only one entry may be", m.ID)
		}
	}
	for _, m := range models {
		if m.Provider != "anthropic" {
			t.Errorf("%s provider = %q, want anthropic", m.ID, m.Provider)
		}
	}

	// Effort levels come from the row itself, replacing the `claude --help`
	// scrape plus the hand-kept claudeModelEffortAllow table.
	opus := models[0]
	if opus.Thinking == nil {
		t.Fatal("opus should advertise a thinking catalog")
	}
	gotLevels := make([]string, 0, len(opus.Thinking.SupportedLevels))
	for _, l := range opus.Thinking.SupportedLevels {
		gotLevels = append(gotLevels, l.Value)
	}
	if want := []string{"low", "medium", "high", "xhigh", "max"}; !reflect.DeepEqual(gotLevels, want) {
		t.Errorf("opus levels = %v, want %v", gotLevels, want)
	}
	// The rows carry no default-effort field, and inventing one is what the
	// static path did. Empty means "the runtime decides".
	if opus.Thinking.DefaultLevel != "" {
		t.Errorf("DefaultLevel = %q, want empty", opus.Thinking.DefaultLevel)
	}
	// Haiku advertises no effort support at all, so it must get no picker.
	if haiku := models[3]; haiku.Thinking != nil {
		t.Errorf("haiku advertises no effort support; Thinking should be nil, got %+v", haiku.Thinking)
	}
}

// TestClaudeModelsFromInfos_OldCLIDisabledRow is the regression this whole
// change exists for. On 2.1.246 the catalog must NOT offer Fable 5.1 as
// selectable — that combination is the guaranteed 400 from the issue — and must
// still tell the user it exists and how to reach it.
func TestClaudeModelsFromInfos_OldCLIDisabledRow(t *testing.T) {
	t.Parallel()
	infos, err := parseClaudeModelCatalog(loadClaudeListModelsFixture(t, "2.1.246"))
	if err != nil {
		t.Fatalf("parseClaudeModelCatalog: %v", err)
	}
	models, unavailable := claudeModelsFromInfos(infos)

	// The selectable list is the one every consumer reads — the picker, the
	// builder, the capability lookups, and any client too old to know about
	// unavailable rows. Nothing unrunnable may be in it.
	for _, m := range models {
		if m.ID == "claude-fable-5-1" || m.ID == "cc-update-required-1" {
			t.Fatalf("%s must never be selectable on 2.1.246: the run 400s", m.ID)
		}
	}

	if len(unavailable) != 1 {
		t.Fatalf("got %d unavailable rows, want 1: %+v", len(unavailable), unavailable)
	}
	if unavailable[0].Label != "Fable 5.1 (disabled)" {
		t.Errorf("unavailable label = %q", unavailable[0].Label)
	}
	if unavailable[0].Reason != "Update to 2.1.255+ to use Fable 5.1" {
		t.Errorf("unavailable reason = %q, want the runtime's own upgrade hint", unavailable[0].Reason)
	}
	// Fable 5 is the model this CLI *can* run, and it stays selectable.
	found := false
	for _, m := range models {
		if m.ID == "claude-fable-5" {
			found = true
		}
	}
	if !found {
		t.Error("claude-fable-5 should remain selectable on 2.1.246")
	}
}

// TestClaudeModelsFromInfos_DefaultWithNoSiblingRow covers the org-restricted
// shape where `default` resolves to a model no other row names.
//
// Dropping it costs more than one picker entry. ValidateThinkingLevelWith
// resolves an empty model — "follow the CLI default", which is what most agents
// are configured with — by looking for the catalog entry flagged Default, and
// fails closed when there is none. With no such entry every default-model task
// would silently lose its configured effort.
func TestClaudeModelsFromInfos_DefaultWithNoSiblingRow(t *testing.T) {
	t.Parallel()
	models, unavailable := claudeModelsFromInfos([]claudeModelInfo{
		{
			Value: "default", ResolvedModel: "claude-sonnet-5",
			DisplayName:    "Default (recommended)",
			SupportsEffort: true, SupportedEffortLevels: []string{"low", "high"},
		},
		{Value: "haiku", ResolvedModel: "claude-haiku-4-5-20251001", DisplayName: "Haiku"},
	})
	if len(unavailable) != 0 {
		t.Errorf("nothing here is unavailable, got %+v", unavailable)
	}

	var def *Model
	for i := range models {
		if models[i].Default {
			def = &models[i]
		}
	}
	if def == nil {
		t.Fatalf("the default model must survive as a real entry, got %+v", models)
	}
	if def.ID != "claude-sonnet-5" {
		t.Errorf("default entry id = %q, want claude-sonnet-5", def.ID)
	}
	// Materialised from the same row, so it keeps that row's capabilities
	// rather than becoming a bare id with no effort picker.
	if def.Thinking == nil || len(def.Thinking.SupportedLevels) != 2 {
		t.Errorf("materialised default lost its effort catalog: %+v", def.Thinking)
	}
	// And it does not displace the row that was already there.
	found := false
	for _, m := range models {
		if m.ID == "claude-haiku-4-5-20251001" {
			found = true
			if m.Default {
				t.Error("only the model the default row resolves to may be badged")
			}
		}
	}
	if !found {
		t.Error("haiku should still be selectable")
	}
}

// TestClaudeModelsFromInfos_DisabledDefaultIsNotMaterialised guards the one case
// where materialising would reintroduce the bug: a default row that is itself
// unrunnable must not be conjured into a selectable model.
func TestClaudeModelsFromInfos_DisabledDefaultIsNotMaterialised(t *testing.T) {
	t.Parallel()
	models, _ := claudeModelsFromInfos([]claudeModelInfo{
		{Value: "default", ResolvedModel: "cc-update-required-1", DisplayName: "Nope", Disabled: true},
		{Value: "haiku", ResolvedModel: "claude-haiku-4-5-20251001", DisplayName: "Haiku"},
	})
	for _, m := range models {
		if m.ID == "cc-update-required-1" {
			t.Fatal("an unrunnable default must never be materialised as selectable")
		}
	}
}

// TestDiscoverClaudeModels_ArgvAndStdin runs the real discovery path against a
// stand-in binary, checking both halves of the contract: the argv the process
// receives, and that the control request actually arrives on its stdin. A probe
// that spawns correctly but never writes the request would hang in production
// and pass a parser-only test.
func TestDiscoverClaudeModels_ArgvAndStdin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}
	t.Parallel()

	dir := t.TempDir()
	argvFile := filepath.Join(dir, "argv.txt")
	stdinFile := filepath.Join(dir, "stdin.txt")
	fixture := filepath.Join(dir, "reply.jsonl")
	if err := os.WriteFile(fixture, loadClaudeListModelsFixture(t, "2.1.258"), 0o600); err != nil {
		t.Fatalf("stage fixture: %v", err)
	}
	fake := filepath.Join(dir, "claude")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" > '" + argvFile + "'\n" +
		"cat > '" + stdinFile + "'\n" +
		"cat '" + fixture + "'\n"
	writeTestExecutable(t, fake, []byte(script))

	models, unavailable, err := discoverClaudeModels(context.Background(), Command{Path: fake})
	if err != nil {
		t.Fatalf("discoverClaudeModels: %v", err)
	}
	if len(models) != 4 {
		t.Fatalf("got %d models, want 4", len(models))
	}
	if len(unavailable) != 0 {
		t.Errorf("2.1.258 reports nothing unavailable, got %+v", unavailable)
	}

	gotArgv, err := os.ReadFile(argvFile)
	if err != nil {
		t.Fatalf("read argv: %v", err)
	}
	if got := splitNonEmptyLines(string(gotArgv)); !reflect.DeepEqual(got, claudeListModelsArgs) {
		t.Errorf("fake claude received argv %v, want %v", got, claudeListModelsArgs)
	}

	gotStdin, err := os.ReadFile(stdinFile)
	if err != nil {
		t.Fatalf("read stdin: %v", err)
	}
	if !strings.Contains(string(gotStdin), `"subtype":"list_models"`) {
		t.Errorf("control request never reached stdin, got %q", gotStdin)
	}
	if !strings.Contains(string(gotStdin), claudeListModelsRequestID) {
		t.Errorf("request id missing from stdin, got %q", gotStdin)
	}
	if !strings.HasSuffix(string(gotStdin), "\n") {
		t.Error("request must be newline-terminated or the CLI never parses the line")
	}
}

func TestDiscoverClaudeModels_ErrorPaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}
	t.Parallel()

	for name, body := range map[string]string{
		// The reply an old CLI without list_models sends.
		"unsupported subtype": `echo '{"type":"control_response","response":{"subtype":"error","request_id":"multica-list-models","error":"Unsupported control request subtype: list_models"}}'` + "\n",
		"no reply":            "exit 0\n",
		"garbage":             "echo 'not json'\n",
		// A well-formed reply with nothing usable must not pass as a catalog.
		"empty catalog": `echo '{"type":"control_response","response":{"subtype":"success","request_id":"multica-list-models","response":{"models":[]}}}'` + "\n",
		// Rows that exist but none of which can be run: the picker would have
		// nothing to offer, so the static list is the better answer.
		"only unavailable rows": `echo '{"type":"control_response","response":{"subtype":"success","request_id":"multica-list-models","response":{"models":[{"value":"cc-update-required-1","resolvedModel":"cc-update-required-1","displayName":"Nope","disabled":true}]}}}'` + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake := filepath.Join(t.TempDir(), "claude")
			writeTestExecutable(t, fake, []byte("#!/bin/sh\ncat > /dev/null\n"+body))
			if _, _, err := discoverClaudeModels(context.Background(), Command{Path: fake}); err == nil {
				t.Fatal("expected an error so the caller falls back to the static catalog")
			}
		})
	}
}

// TestDiscoverClaudeCatalog_FallsBackToStatic covers the degrade path. The
// Fallback flag is the load-bearing part: it is what stops the server caching a
// discovery failure as this runtime's catalog for a day (MUL-5549).
func TestDiscoverClaudeCatalog_FallsBackToStatic(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "definitely-not-installed")

	catalog := discoverClaudeCatalog(context.Background(), Command{Path: missing})
	if !catalog.Fallback {
		t.Error("a static answer after discovery failed must be flagged Fallback")
	}
	if len(catalog.Models) != len(claudeStaticModels()) {
		t.Errorf("got %d models, want the static catalog's %d",
			len(catalog.Models), len(claudeStaticModels()))
	}
}

func TestDiscoverClaudeCatalog_LiveResultIsAuthoritative(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}
	t.Parallel()

	dir := t.TempDir()
	fixture := filepath.Join(dir, "reply.jsonl")
	if err := os.WriteFile(fixture, loadClaudeListModelsFixture(t, "2.1.246"), 0o600); err != nil {
		t.Fatalf("stage fixture: %v", err)
	}
	fake := filepath.Join(dir, "claude")
	writeTestExecutable(t, fake, []byte("#!/bin/sh\ncat > /dev/null\ncat '"+fixture+"'\n"))

	catalog := discoverClaudeCatalog(context.Background(), Command{Path: fake})
	if catalog.Fallback {
		t.Error("a live catalog must not be flagged Fallback — the server should cache it")
	}
	for _, m := range catalog.Models {
		if m.ID == "claude-fable-5-1" {
			t.Fatal("2.1.246's live catalog must not contain claude-fable-5-1")
		}
	}
}

// TestDiscoverClaudeCatalog_RemembersUnsupported is the fix for the "one cheap
// round trip" claim that was not true.
//
// Every Claude task carrying a thinking_level loads the catalog before it
// starts, and cachedDiscovery refuses to memoise a fallback — so on a CLI too
// old to answer list_models, the probe ran once per task forever. Once the
// binary has said it does not know the request, we stop asking it.
func TestDiscoverClaudeCatalog_RemembersUnsupported(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}
	// Not parallel: mutates the package-level capability cache.
	resetClaudeCapabilityCacheForTests()
	t.Cleanup(resetClaudeCapabilityCacheForTests)

	dir := t.TempDir()
	countFile := filepath.Join(dir, "probes.txt")
	fake := filepath.Join(dir, "claude")
	// Counts only real probes: the static fallback also runs --version and
	// --help (for the effort superset), and those are not what is being bounded.
	script := "#!/bin/sh\n" +
		"if [ \"$1\" != \"--print\" ]; then echo '2.1.100 (Claude Code)'; exit 0; fi\n" +
		"cat > /dev/null\n" +
		"echo x >> '" + countFile + "'\n" +
		`echo '{"type":"control_response","response":{"subtype":"error","request_id":"multica-list-models",` +
		`"error":"Unsupported control request subtype: list_models"}}'` + "\n"
	writeTestExecutable(t, fake, []byte(script))

	cmd := Command{Path: fake}
	for i := 0; i < 3; i++ {
		catalog := discoverClaudeCatalog(context.Background(), cmd)
		if !catalog.Fallback {
			t.Fatalf("round %d: an old CLI must degrade to the flagged static catalog", i)
		}
		if len(catalog.Models) == 0 {
			t.Fatalf("round %d: the fallback must still be usable", i)
		}
	}

	probes := 0
	if data, err := os.ReadFile(countFile); err == nil {
		probes = len(splitNonEmptyLines(string(data)))
	}
	if probes != 1 {
		t.Errorf("probed %d times across 3 catalog loads, want 1 — the "+
			"unsupported answer is a property of the binary, not a bad moment", probes)
	}
}

// TestDiscoverClaudeCatalog_DoesNotRememberTransientFailures is the other half
// of the contract. Caching a timeout or a crash would turn one bad moment into
// ten minutes of static catalog on a CLI that can actually answer.
func TestDiscoverClaudeCatalog_DoesNotRememberTransientFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}
	resetClaudeCapabilityCacheForTests()
	t.Cleanup(resetClaudeCapabilityCacheForTests)

	dir := t.TempDir()
	countFile := filepath.Join(dir, "probes.txt")
	fake := filepath.Join(dir, "claude")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" != \"--print\" ]; then echo '2.1.258 (Claude Code)'; exit 0; fi\n" +
		"cat > /dev/null\n" +
		"echo x >> '" + countFile + "'\n" +
		"echo 'garbage that is not a control response'\n"
	writeTestExecutable(t, fake, []byte(script))

	cmd := Command{Path: fake}
	for i := 0; i < 3; i++ {
		if catalog := discoverClaudeCatalog(context.Background(), cmd); !catalog.Fallback {
			t.Fatalf("round %d: expected a fallback catalog", i)
		}
	}

	data, err := os.ReadFile(countFile)
	if err != nil {
		t.Fatalf("read probe count: %v", err)
	}
	if probes := len(splitNonEmptyLines(string(data))); probes != 3 {
		t.Errorf("probed %d times, want 3 — a transient failure must stay retryable", probes)
	}
}

// TestValidateThinkingLevel_TaggedCatalogID guards the capability lookup against
// the context-window tag discovery now puts in catalog ids. Without normalising
// the catalog side, `claude-opus-5[1m]` never matches the stripped target and
// every effort level fails closed — the daemon would silently drop --effort.
func TestValidateThinkingLevel_TaggedCatalogID(t *testing.T) {
	t.Parallel()
	catalog := Catalog{Models: []Model{{
		ID:       "claude-opus-5[1m]",
		Label:    "Opus (1M context)",
		Provider: "anthropic",
		Thinking: &ModelThinking{SupportedLevels: []ThinkingLevel{
			{Value: "low"}, {Value: "medium"}, {Value: "high"}, {Value: "xhigh"}, {Value: "max"},
		}},
	}}}
	load := func() (Catalog, error) { return catalog, nil }

	for _, model := range []string{"claude-opus-5[1m]", "claude-opus-5"} {
		ok, err := ValidateThinkingLevelWith(load, "claude", model, "xhigh")
		if err != nil {
			t.Fatalf("%s: %v", model, err)
		}
		if !ok {
			t.Errorf("%s: xhigh should validate against the tagged catalog entry", model)
		}
	}

	ok, err := ValidateThinkingLevelWith(load, "claude", "claude-opus-5", "nonsense")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("a level the catalog does not advertise must still fail closed")
	}
}
