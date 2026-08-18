package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestCompilePlatformExtension_RejectsUntrustedCommandSuffixDeclarations(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*PlatformExtensionSource)
	}{
		{"empty declaration", func(source *PlatformExtensionSource) { source.CommandSuffixes = PlatformExtensionCommandSuffixes{} }},
		{"changed flow suffix", func(source *PlatformExtensionSource) { source.CommandSuffixes.Flow = []string{".workflow"} }},
		{"tool bypass", func(source *PlatformExtensionSource) {
			source.CommandSuffixes.Tool = nil
			source.Commands = append(source.Commands, PlatformExtensionCommand{Name: "shell.tool", Metadata: json.RawMessage(`{}`)})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := readPlatformExtensionSource(t)
			tc.change(&source)
			_, err := CompilePlatformExtension(source)
			assertPlatformExtensionCode(t, err, "COMMAND_SUFFIX_POLICY_MISMATCH")
		})
	}
}

func TestCompilePlatformExtension_TreatsE2ECommandNameAsFlow(t *testing.T) {
	source := readPlatformExtensionSource(t)
	source.Commands[0].Name = "delegate-e2e"

	bundle, err := CompilePlatformExtension(source)
	if err != nil {
		t.Fatalf("CompilePlatformExtension() error = %v", err)
	}
	if got := platformExtensionCommandNames(bundle.FlowCommands); !reflect.DeepEqual(got, []string{"delegate-e2e"}) {
		t.Fatalf("flow commands = %#v, want delegate-e2e", got)
	}
	if got := platformExtensionCommandNames(bundle.RuntimeCommands); !reflect.DeepEqual(got, []string{"summarize"}) {
		t.Fatalf("runtime commands = %#v, want summarize", got)
	}
	if err := ValidatePlatformExtensionBundle(bundle); err != nil {
		t.Fatalf("ValidatePlatformExtensionBundle() rejected -e2e flow command: %v", err)
	}
}

func TestPlatformExtensionImportConfigurationDefaultsAndValidatesAgentRuntimeBindings(t *testing.T) {
	source := readPlatformExtensionSource(t)
	source.Commands[0].Name = "delegate-e2e"
	bundle, err := CompilePlatformExtension(source)
	if err != nil {
		t.Fatalf("CompilePlatformExtension() error = %v", err)
	}
	firstRuntimeID := "11111111-1111-4111-8111-111111111111"
	secondRuntimeID := "22222222-2222-4222-8222-222222222222"
	runtimes := []db.AgentRuntime{
		{ID: parseUUID(firstRuntimeID)},
		{ID: parseUUID(secondRuntimeID)},
	}

	config, err := platformExtensionImportConfigurationForBundle(bundle, runtimes, "")
	if err != nil {
		t.Fatalf("default configuration error = %v", err)
	}
	if config.SquadBaseName != "delegate" || config.AgentRuntimeIDs["lead-researcher"] != firstRuntimeID {
		t.Fatalf("default configuration = %+v", config)
	}

	config, err = platformExtensionImportConfigurationForBundle(bundle, runtimes, `{"squad_base_name":"review","agent_runtime_ids":{"lead-researcher":"22222222-2222-4222-8222-222222222222"}}`)
	if err != nil {
		t.Fatalf("custom configuration error = %v", err)
	}
	if config.SquadBaseName != "review" || config.AgentRuntimeIDs["lead-researcher"] != secondRuntimeID {
		t.Fatalf("custom configuration = %+v", config)
	}

	_, err = platformExtensionImportConfigurationForBundle(bundle, runtimes, `{"agent_runtime_ids":{"lead-researcher":"33333333-3333-4333-8333-333333333333"}}`)
	assertPlatformExtensionCode(t, err, "EXTENSION_IMPORT_CONFIG_INVALID")
}

func TestCompilePlatformExtensionWithPolicy_UsesExplicitTrustedSuffixes(t *testing.T) {
	policy := PlatformExtensionPolicy{CommandSuffixes: PlatformExtensionCommandSuffixes{
		Flow: []string{".workflow"},
		Tool: []string{".action"},
	}}
	source := readPlatformExtensionSource(t)
	source.CommandSuffixes = policy.CommandSuffixes
	source.Commands[0].Name = "delegate.workflow"

	bundle, err := CompilePlatformExtensionWithPolicy(source, policy)
	if err != nil {
		t.Fatalf("CompilePlatformExtensionWithPolicy() error = %v", err)
	}
	if got, want := platformExtensionCommandNames(bundle.FlowCommands), []string{"delegate.workflow"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("flow commands = %#v, want %#v", got, want)
	}
	if err := ValidatePlatformExtensionBundleWithPolicy(bundle, policy); err != nil {
		t.Fatalf("ValidatePlatformExtensionBundleWithPolicy() error = %v", err)
	}
	assertPlatformExtensionCode(t, ValidatePlatformExtensionBundle(bundle), "COMMAND_SUFFIX_POLICY_MISMATCH")

	source.Commands = append(source.Commands, PlatformExtensionCommand{Name: "shell.action", Metadata: json.RawMessage(`{}`)})
	_, err = CompilePlatformExtensionWithPolicy(source, policy)
	assertPlatformExtensionCode(t, err, "TOOL_COMMAND_UNSUPPORTED")
}

func TestCompilePlatformExtensionWithPolicy_RequiresStableDeclarationOrder(t *testing.T) {
	policy := PlatformExtensionPolicy{CommandSuffixes: PlatformExtensionCommandSuffixes{
		Flow: []string{".flow", ".route"},
		Tool: []string{".tool", ".action"},
	}}
	source := readPlatformExtensionSource(t)
	source.CommandSuffixes = PlatformExtensionCommandSuffixes{
		Flow: []string{".route", ".flow"},
		Tool: []string{".tool", ".action"},
	}
	_, err := CompilePlatformExtensionWithPolicy(source, policy)
	assertPlatformExtensionCode(t, err, "COMMAND_SUFFIX_POLICY_MISMATCH")
}

func TestCompilePlatformExtensionWithPolicy_RejectsInvalidTrustedPolicy(t *testing.T) {
	for _, tc := range []struct {
		name   string
		policy PlatformExtensionPolicy
	}{
		{"missing tool suffix", PlatformExtensionPolicy{CommandSuffixes: PlatformExtensionCommandSuffixes{Flow: []string{".flow"}}}},
		{"empty suffix", PlatformExtensionPolicy{CommandSuffixes: PlatformExtensionCommandSuffixes{Flow: []string{""}, Tool: []string{".tool"}}}},
		{"duplicate suffix", PlatformExtensionPolicy{CommandSuffixes: PlatformExtensionCommandSuffixes{Flow: []string{".flow", ".flow"}, Tool: []string{".tool"}}}},
		{"overlapping classifications", PlatformExtensionPolicy{CommandSuffixes: PlatformExtensionCommandSuffixes{Flow: []string{".workflow"}, Tool: []string{"flow"}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := readPlatformExtensionSource(t)
			source.CommandSuffixes = tc.policy.CommandSuffixes
			_, err := CompilePlatformExtensionWithPolicy(source, tc.policy)
			assertPlatformExtensionCode(t, err, "COMMAND_SUFFIX_POLICY_INVALID")
		})
	}
}

func TestValidatePlatformExtensionBundleWithPolicy_RejectsMissingTrustedToolSuffix(t *testing.T) {
	bundle, err := CompilePlatformExtension(readPlatformExtensionSource(t))
	if err != nil {
		t.Fatalf("CompilePlatformExtension() error = %v", err)
	}
	bundle.CommandSuffixes = PlatformExtensionCommandSuffixes{Flow: []string{".flow"}}
	bundle.Digest = rawPlatformExtensionBundleDigest(t, bundle)
	policy := PlatformExtensionPolicy{CommandSuffixes: bundle.CommandSuffixes}
	assertPlatformExtensionCode(t, ValidatePlatformExtensionBundleWithPolicy(bundle, policy), "COMMAND_SUFFIX_POLICY_INVALID")
}

func TestValidatePlatformExtensionBundle_RejectsUntrustedCommandSuffixDeclaration(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*PlatformExtensionBundle)
	}{
		{"empty declaration", func(bundle *PlatformExtensionBundle) { bundle.CommandSuffixes = PlatformExtensionCommandSuffixes{} }},
		{"changed flow suffix", func(bundle *PlatformExtensionBundle) { bundle.CommandSuffixes.Flow = []string{".workflow"} }},
		{"tool bypass", func(bundle *PlatformExtensionBundle) {
			bundle.CommandSuffixes.Tool = nil
			bundle.RuntimeCommands = append(bundle.RuntimeCommands, PlatformExtensionCommand{Name: "shell.tool", Metadata: json.RawMessage(`{}`)})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bundle, err := CompilePlatformExtension(readPlatformExtensionSource(t))
			if err != nil {
				t.Fatalf("CompilePlatformExtension() error = %v", err)
			}
			tc.change(&bundle)
			bundle.Digest = rawPlatformExtensionBundleDigest(t, bundle)
			assertPlatformExtensionCode(t, ValidatePlatformExtensionBundle(bundle), "COMMAND_SUFFIX_POLICY_MISMATCH")
		})
	}
}

func TestDecodePlatformExtension_RejectsDuplicateObjectKeysThroughoutDocument(t *testing.T) {
	for _, tc := range []struct {
		name   string
		decode func([]byte) error
		input  string
	}{
		{
			name:   "source top level",
			decode: func(data []byte) error { _, err := DecodePlatformExtensionSource(data); return err },
			input: strings.Replace(string(readPlatformExtensionFixture(t, "research-team.source.json")),
				`"schema_version": "platform.extension/v1",`, `"schema_version": "platform.extension/v1", "schema_version": "platform.extension/v1",`, 1),
		},
		{
			name:   "source nested object",
			decode: func(data []byte) error { _, err := DecodePlatformExtensionSource(data); return err },
			input: strings.Replace(string(readPlatformExtensionFixture(t, "research-team.source.json")),
				`"key": "source-review",`, `"key": "source-review", "key": "shadow",`, 1),
		},
		{
			name:   "source metadata",
			decode: func(data []byte) error { _, err := DecodePlatformExtensionSource(data); return err },
			input: strings.Replace(string(readPlatformExtensionFixture(t, "research-team.source.json")),
				`"priority": 2,`, `"priority": 2, "priority": 3,`, 1),
		},
		{
			name:   "bundle metadata",
			decode: func(data []byte) error { _, err := DecodePlatformExtensionBundle(data); return err },
			input: strings.Replace(string(readPlatformExtensionFixture(t, "research-team.bundle.json")),
				`"priority": 1,`, `"priority": 1, "priority": 3,`, 1),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.decode([]byte(tc.input))
			assertPlatformExtensionCode(t, err, "EXTENSION_INVALID")
			if !strings.Contains(err.Error(), "duplicate object key") {
				t.Fatalf("decode error = %v, want duplicate object key", err)
			}
		})
	}
}

func TestCompilePlatformExtension_RejectsDuplicateCommandMetadataKeys(t *testing.T) {
	source := readPlatformExtensionSource(t)
	source.Commands[0].Metadata = json.RawMessage(`{"priority":1,"priority":2}`)
	_, err := CompilePlatformExtension(source)
	assertPlatformExtensionCode(t, err, "COMMAND_METADATA_INVALID")
}

func TestValidatePlatformExtensionBundle_UsesCanonicalMetadataForDigestAndPersistence(t *testing.T) {
	bundle, err := CompilePlatformExtension(readPlatformExtensionSource(t))
	if err != nil {
		t.Fatalf("CompilePlatformExtension() error = %v", err)
	}
	wantDigest := bundle.Digest
	bundle.RuntimeCommands[0].Metadata = json.RawMessage(`{ "scope": "team", "priority": 1 }`)

	if err := ValidatePlatformExtensionBundle(bundle); err != nil {
		t.Fatalf("ValidatePlatformExtensionBundle() with semantic metadata reorder error = %v", err)
	}
	got, err := CanonicalPlatformExtensionBundleJSON(bundle)
	if err != nil {
		t.Fatalf("CanonicalPlatformExtensionBundleJSON() error = %v", err)
	}
	want := readPlatformExtensionFixture(t, "research-team.bundle.json")
	if string(got) != string(want) {
		t.Fatalf("canonicalized bundle differs from golden\ngot: %s\nwant: %s", got, want)
	}

	bundle.Digest = rawPlatformExtensionBundleDigest(t, bundle)
	if bundle.Digest == wantDigest {
		t.Fatal("test setup produced the canonical digest from non-canonical metadata")
	}
	assertPlatformExtensionCode(t, ValidatePlatformExtensionBundle(bundle), "BUNDLE_DIGEST_INVALID")
}

func TestCompilePlatformExtension_RejectsPortableSkillPathCollisions(t *testing.T) {
	for _, tc := range []struct {
		name  string
		paths []string
	}{
		{"case fold", []string{"references/A.md", "references/a.md"}},
		{"windows trailing dot", []string{"references/note", "references/note."}},
		{"windows trailing space component", []string{"references/group/note.md", "references/group /note.md"}},
		{"unicode NFC", []string{"references/caf\u00e9.md", "references/cafe\u0301.md"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := readPlatformExtensionSource(t)
			for _, filePath := range tc.paths {
				source.Skills[0].Files = append(source.Skills[0].Files, PlatformExtensionSkillFile{Path: filePath, Content: "collision"})
			}
			_, err := CompilePlatformExtension(source)
			assertPlatformExtensionCode(t, err, "DUPLICATE_SKILL_FILE_PATH")
		})
	}
}

func TestCompilePlatformExtension_DigestUsesSha256Prefix(t *testing.T) {
	bundle, err := CompilePlatformExtension(readPlatformExtensionSource(t))
	if err != nil {
		t.Fatalf("CompilePlatformExtension() error = %v", err)
	}
	if !strings.HasPrefix(bundle.Digest, "sha256:") || len(bundle.Digest) != len("sha256:")+64 {
		t.Fatalf("digest = %q, want sha256:<64 lowercase hex>", bundle.Digest)
	}
}

func TestCompilePlatformExtension_GoldenParity(t *testing.T) {
	source := readPlatformExtensionSource(t)

	bundle, err := CompilePlatformExtension(source)
	if err != nil {
		t.Fatalf("CompilePlatformExtension() error = %v", err)
	}
	if bundle.SchemaVersion != PlatformExtensionBundleSchemaVersion {
		t.Fatalf("schema version = %q", bundle.SchemaVersion)
	}
	if !reflect.DeepEqual(bundle.CommandSuffixes, source.CommandSuffixes) {
		t.Fatalf("command suffixes = %#v, want %#v", bundle.CommandSuffixes, source.CommandSuffixes)
	}
	assertPlatformExtensionCommand(t, bundle.FlowCommands[0], source.Commands[0])
	assertPlatformExtensionCommand(t, bundle.RuntimeCommands[0], source.Commands[1])

	got, err := CanonicalPlatformExtensionBundleJSON(bundle)
	if err != nil {
		t.Fatalf("CanonicalPlatformExtensionBundleJSON() error = %v", err)
	}
	want := readPlatformExtensionFixture(t, "research-team.bundle.json")
	if string(got) != string(want) {
		t.Fatalf("canonical bundle differs from golden\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestDecodePlatformExtension_RejectsUnknownFieldsAndTrailingData(t *testing.T) {
	for _, tc := range []struct {
		name   string
		decode func([]byte) error
		input  string
	}{
		{
			name:   "source unknown field",
			decode: func(data []byte) error { _, err := DecodePlatformExtensionSource(data); return err },
			input:  `{"schema_version":"platform.extension/v1","extension":{},"leader":"","agents":[],"skills":[],"commands":[],"command_suffixes":{},"unknown":true}`,
		},
		{
			name:   "source trailing value",
			decode: func(data []byte) error { _, err := DecodePlatformExtensionSource(data); return err },
			input:  string(readPlatformExtensionFixture(t, "research-team.source.json")) + `{}`,
		},
		{
			name:   "bundle unknown nested field",
			decode: func(data []byte) error { _, err := DecodePlatformExtensionBundle(data); return err },
			input:  `{"schema_version":"multica.extension-bundle/v1","extension":{"key":"x","unknown":true},"digest":"x","leader":"x","agents":[],"skills":[],"flow_commands":[],"runtime_commands":[],"command_suffixes":{},"squad_instructions":""}`,
		},
		{
			name:   "bundle trailing value",
			decode: func(data []byte) error { _, err := DecodePlatformExtensionBundle(data); return err },
			input:  string(readPlatformExtensionFixture(t, "research-team.bundle.json")) + `null`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.decode([]byte(tc.input)); err == nil {
				t.Fatal("decoder accepted malformed JSON")
			}
		})
	}
}

func TestCompilePlatformExtension_RejectsInvalidSource(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*PlatformExtensionSource)
		code string
	}{
		{"invalid leader", func(s *PlatformExtensionSource) { s.Leader = "missing" }, "LEADER_INVALID"},
		{"duplicate agent", func(s *PlatformExtensionSource) { s.Agents = append(s.Agents, s.Agents[0]) }, "DUPLICATE_NAME"},
		{"missing skill root", func(s *PlatformExtensionSource) { s.Skills[0].Files = s.Skills[0].Files[1:] }, "SKILL_ROOT_INVALID"},
		{"duplicate skill file", func(s *PlatformExtensionSource) { s.Skills[0].Files = append(s.Skills[0].Files, s.Skills[0].Files[1]) }, "DUPLICATE_SKILL_FILE_PATH"},
		{"unsafe skill path", func(s *PlatformExtensionSource) {
			s.Skills[0].Files = append(s.Skills[0].Files, PlatformExtensionSkillFile{Path: "../escape.md"})
		}, "UNSAFE_SKILL_PATH"},
		{"reserved skill path", func(s *PlatformExtensionSource) {
			s.Skills[0].Files = append(s.Skills[0].Files, PlatformExtensionSkillFile{Path: ".platform-agent/context.json"})
		}, "RESERVED_SKILL_PATH"},
		{"required extension key", func(s *PlatformExtensionSource) { s.Extension.Key = "" }, "REQUIRED_FIELD"},
		{"trusted tool suffix", func(s *PlatformExtensionSource) {
			s.Commands = append(s.Commands, PlatformExtensionCommand{Name: "shell.tool", Metadata: json.RawMessage(`{}`)})
		}, "TOOL_COMMAND_UNSUPPORTED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := readPlatformExtensionSource(t)
			tc.edit(&source)
			_, err := CompilePlatformExtension(source)
			assertPlatformExtensionCode(t, err, tc.code)
		})
	}
}

func TestCompilePlatformExtension_RejectsDaemonOwnershipControlPathAtAnyDepth(t *testing.T) {
	for _, filePath := range []string{
		".multica-sidecar-owner",
		"references/.multica-sidecar-owner",
		"references/.MULTICA-SIDECAR-OWNER. ",
	} {
		t.Run(filePath, func(t *testing.T) {
			source := readPlatformExtensionSource(t)
			source.Skills[0].Files = append(source.Skills[0].Files, PlatformExtensionSkillFile{Path: filePath, Content: "must not shadow daemon ownership"})
			_, err := CompilePlatformExtension(source)
			assertPlatformExtensionCode(t, err, "RESERVED_SKILL_PATH")
		})
	}
}

func TestCompilePlatformExtension_EnforcesLimitsAndClassificationOrder(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*PlatformExtensionSource)
		code   string
	}{
		{"agents", func(s *PlatformExtensionSource) { s.Agents = platformExtensionAgents(PlatformExtensionMaxAgents + 1) }, "AGENT_LIMIT_EXCEEDED"},
		{"skills", func(s *PlatformExtensionSource) { s.Skills = platformExtensionSkills(PlatformExtensionMaxSkills + 1) }, "SKILL_LIMIT_EXCEEDED"},
		{"commands", func(s *PlatformExtensionSource) {
			s.Commands = platformExtensionCommands(PlatformExtensionMaxCommands + 1)
		}, "COMMAND_LIMIT_EXCEEDED"},
		{"skill files", func(s *PlatformExtensionSource) {
			s.Skills[0].Files = platformExtensionSkillFiles(PlatformExtensionMaxSkillFiles+1, "x")
		}, "SKILL_FILE_LIMIT_EXCEEDED"},
		{"skill file bytes", func(s *PlatformExtensionSource) {
			s.Skills[0].Files = platformExtensionSkillFiles(1, strings.Repeat("x", PlatformExtensionMaxSkillFileBytes+1))
		}, "SKILL_FILE_SIZE_EXCEEDED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := readPlatformExtensionSource(t)
			tc.mutate(&source)
			_, err := CompilePlatformExtension(source)
			assertPlatformExtensionCode(t, err, tc.code)
		})
	}

	source := readPlatformExtensionSource(t)
	source.Commands = append(source.Commands,
		PlatformExtensionCommand{Name: "second.flow", Metadata: json.RawMessage(`{"step": 2}`)},
		PlatformExtensionCommand{Name: "ordinary", Metadata: json.RawMessage(`{"step": 3}`)},
	)
	bundle, err := CompilePlatformExtension(source)
	if err != nil {
		t.Fatalf("CompilePlatformExtension() error = %v", err)
	}
	if got, want := platformExtensionCommandNames(bundle.FlowCommands), []string{"delegate.flow", "second.flow"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("flow command order = %#v, want %#v", got, want)
	}
	if got, want := platformExtensionCommandNames(bundle.RuntimeCommands), []string{"summarize", "ordinary"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("runtime command order = %#v, want %#v", got, want)
	}
}

func TestValidatePlatformExtensionBundle_RevalidatesStructureDigestAndInstructions(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*PlatformExtensionBundle)
		code string
	}{
		{"digest mismatch", func(b *PlatformExtensionBundle) { b.Extension.Name = "tampered" }, "BUNDLE_DIGEST_INVALID"},
		{"classified flow as runtime", func(b *PlatformExtensionBundle) {
			b.RuntimeCommands = append(b.RuntimeCommands, b.FlowCommands[0])
			b.FlowCommands = nil
			redigestPlatformExtension(t, b)
		}, "RUNTIME_COMMAND_CLASSIFICATION_INVALID"},
		{"untrusted suffix declaration", func(b *PlatformExtensionBundle) {
			b.CommandSuffixes.Tool = []string{".flow"}
			redigestPlatformExtension(t, b)
		}, "COMMAND_SUFFIX_POLICY_MISMATCH"},
		{"instructions", func(b *PlatformExtensionBundle) { b.SquadInstructions = "untrusted"; redigestPlatformExtension(t, b) }, "SQUAD_INSTRUCTIONS_INVALID"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bundle, err := CompilePlatformExtension(readPlatformExtensionSource(t))
			if err != nil {
				t.Fatalf("CompilePlatformExtension() error = %v", err)
			}
			tc.edit(&bundle)
			assertPlatformExtensionCode(t, ValidatePlatformExtensionBundle(bundle), tc.code)
		})
	}
}

func readPlatformExtensionSource(t *testing.T) PlatformExtensionSource {
	t.Helper()
	source, err := DecodePlatformExtensionSource(readPlatformExtensionFixture(t, "research-team.source.json"))
	if err != nil {
		t.Fatalf("DecodePlatformExtensionSource() error = %v", err)
	}
	return source
}

func readPlatformExtensionFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "platform_extensions", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return data
}

func assertPlatformExtensionCommand(t *testing.T, got, want PlatformExtensionCommand) {
	t.Helper()
	if got.Name != want.Name || got.Description != want.Description || got.Content != want.Content {
		t.Fatalf("command fields changed: got %#v, want %#v", got, want)
	}
	var gotMetadata, wantMetadata any
	if err := json.Unmarshal(got.Metadata, &gotMetadata); err != nil {
		t.Fatalf("decode got metadata: %v", err)
	}
	if err := json.Unmarshal(want.Metadata, &wantMetadata); err != nil {
		t.Fatalf("decode want metadata: %v", err)
	}
	if !reflect.DeepEqual(gotMetadata, wantMetadata) {
		t.Fatalf("metadata changed: got %#v, want %#v", gotMetadata, wantMetadata)
	}
}

func assertPlatformExtensionCode(t *testing.T, err error, want string) {
	t.Helper()
	var contractErr *PlatformExtensionContractError
	if !errors.As(err, &contractErr) {
		t.Fatalf("error = %T %[1]v, want contract error %q", err, want)
	}
	if contractErr.Code != want {
		t.Fatalf("error code = %q, want %q", contractErr.Code, want)
	}
}

func platformExtensionAgents(count int) []PlatformExtensionAgent {
	agents := make([]PlatformExtensionAgent, count)
	for i := range agents {
		agents[i] = PlatformExtensionAgent{Key: fmt.Sprintf("agent-%d", i), Name: fmt.Sprintf("Agent %d", i)}
	}
	agents[0].Key = "lead-researcher"
	return agents
}

func platformExtensionSkills(count int) []PlatformExtensionSkill {
	skills := make([]PlatformExtensionSkill, count)
	for i := range skills {
		skills[i] = PlatformExtensionSkill{Key: fmt.Sprintf("skill-%d", i), Name: fmt.Sprintf("Skill %d", i), Files: platformExtensionSkillFiles(1, "skill")}
	}
	return skills
}

func platformExtensionCommands(count int) []PlatformExtensionCommand {
	commands := make([]PlatformExtensionCommand, count)
	for i := range commands {
		commands[i] = PlatformExtensionCommand{Name: fmt.Sprintf("command-%d", i), Metadata: json.RawMessage(`{}`)}
	}
	return commands
}

func platformExtensionSkillFiles(count int, content string) []PlatformExtensionSkillFile {
	files := make([]PlatformExtensionSkillFile, count)
	files[0] = PlatformExtensionSkillFile{Path: "SKILL.md", Content: content}
	for i := 1; i < count; i++ {
		files[i] = PlatformExtensionSkillFile{Path: fmt.Sprintf("references/%d.md", i), Content: content}
	}
	return files
}

func platformExtensionCommandNames(commands []PlatformExtensionCommand) []string {
	names := make([]string, len(commands))
	for i, command := range commands {
		names[i] = command.Name
	}
	return names
}

func redigestPlatformExtension(t *testing.T, bundle *PlatformExtensionBundle) {
	t.Helper()
	digest, err := platformExtensionBundleDigest(*bundle)
	if err != nil {
		t.Fatalf("platformExtensionBundleDigest() error = %v", err)
	}
	bundle.Digest = digest
}

func rawPlatformExtensionBundleDigest(t *testing.T, bundle PlatformExtensionBundle) string {
	t.Helper()
	bundle.Digest = ""
	data, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		t.Fatalf("marshal raw bundle: %v", err)
	}
	data = append(data, '\n')
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
