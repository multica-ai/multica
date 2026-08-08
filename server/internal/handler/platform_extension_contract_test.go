package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

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
		{"tool suffix precedes flow suffix", func(s *PlatformExtensionSource) { s.CommandSuffixes.Tool = []string{".flow"} }, "TOOL_COMMAND_UNSUPPORTED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := readPlatformExtensionSource(t)
			tc.edit(&source)
			_, err := CompilePlatformExtension(source)
			assertPlatformExtensionCode(t, err, tc.code)
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
		{"tool command", func(b *PlatformExtensionBundle) {
			b.CommandSuffixes.Tool = []string{".flow"}
			redigestPlatformExtension(t, b)
		}, "TOOL_COMMAND_UNSUPPORTED"},
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
