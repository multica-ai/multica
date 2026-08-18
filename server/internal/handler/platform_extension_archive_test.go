package handler

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodePlatformExtensionArchiveImportHydratesTextAndBinarySkillFiles(t *testing.T) {
	source := readPlatformExtensionSource(t)
	source.Commands[0].Name = "delegate-e2e"
	source.Skills[0].Files = []PlatformExtensionSkillFile{
		{Path: "SKILL.md"},
		{Path: "assets/logo.bin", Encoding: "base64"},
	}
	manifest, err := json.Marshal(source)
	if err != nil {
		t.Fatalf("marshal extension manifest: %v", err)
	}

	logo := []byte{0x00, 0xff, 0x00, 0x7f}
	archive := platformExtensionArchive(t, map[string][]byte{
		"extension.json":                       manifest,
		"skills/source-review/SKILL.md":        []byte("---\nname: source-review\n---\n\nRead evidence."),
		"skills/source-review/assets/logo.bin": logo,
	})

	bundle, canonicalManifest, err := decodePlatformExtensionArchiveImport(archive, DefaultPlatformExtensionV1Policy())
	if err != nil {
		t.Fatalf("decodePlatformExtensionArchiveImport() error = %v", err)
	}
	if len(canonicalManifest) == 0 {
		t.Fatal("canonical manifest is empty")
	}
	if got, want := bundle.Skills[0].Files[0], (PlatformExtensionSkillFile{Path: "SKILL.md", Content: "---\nname: source-review\n---\n\nRead evidence."}); got != want {
		t.Fatalf("text skill file = %#v, want %#v", got, want)
	}
	if got, want := bundle.Skills[0].Files[1], (PlatformExtensionSkillFile{Path: "assets/logo.bin", Content: base64.StdEncoding.EncodeToString(logo), Encoding: "base64"}); got != want {
		t.Fatalf("binary skill file = %#v, want %#v", got, want)
	}
}

func TestDecodePlatformExtensionArchiveImportReadsCodeAgentDirectoryLayout(t *testing.T) {
	archive := platformExtensionArchive(t, map[string][]byte{
		"codeagent-extension.json":               []byte(`{"name":"Runtime Pool Demo","version":"1.0.0","description":"E2E开发"}`),
		"agents/pool-coordinator.md":             []byte("---\nname: Pool Coordinator\ndescription: Coordinates the pool.\nleader: true\n---\nCoordinate the task.\n"),
		"agents/pool-researcher.md":              []byte("---\nname: Pool Researcher\ndescription: Researches evidence.\n---\nResearch the task.\n"),
		"commands/delegate-e2e.md":               []byte("---\ndescription: Delegates an end-to-end task.\n---\nDelegate the task.\n"),
		"commands/evidence.md":                   []byte("---\ndescription: Collects evidence.\n---\nCollect evidence.\n"),
		"skills/pool-evidence/SKILL.md":          []byte("---\nname: Pool Evidence\ndescription: Evidence skill.\n---\n# Pool Evidence\n"),
		"skills/pool-evidence/assets/marker.bin": {0x00, 0xff, 0x50, 0x4b},
	})

	bundle, _, err := decodePlatformExtensionArchiveImport(archive, DefaultPlatformExtensionV1Policy())
	if err != nil {
		t.Fatalf("decode codeagent archive: %v", err)
	}
	if got, want := bundle.Extension.Key, "runtime-pool-demo"; got != want {
		t.Fatalf("extension key = %q, want %q", got, want)
	}
	if got, want := bundle.Extension.Version, "1.0.0"; got != want {
		t.Fatalf("extension version = %q, want %q", got, want)
	}
	if got, want := bundle.Extension.Description, "E2E开发"; got != want {
		t.Fatalf("extension description = %q, want %q", got, want)
	}
	if got, want := bundle.Leader, "pool-coordinator"; got != want {
		t.Fatalf("leader = %q, want %q", got, want)
	}
	if got, want := len(bundle.Agents), 2; got != want {
		t.Fatalf("agent count = %d, want %d", got, want)
	}
	if got, want := len(bundle.FlowCommands), 1; got != want {
		t.Fatalf("flow command count = %d, want %d", got, want)
	}
	if got, want := bundle.Agents[0].Prompt, "Coordinate the task.\n"; got != want {
		t.Fatalf("agent Markdown body = %q, want %q", got, want)
	}
	if got, want := bundle.FlowCommands[0].Content, "Delegate the task.\n"; got != want {
		t.Fatalf("command Markdown body = %q, want %q", got, want)
	}
	if got, want := bundle.Skills[0].Files[1], (PlatformExtensionSkillFile{Path: "assets/marker.bin", Content: "AP9QSw==", Encoding: "base64"}); got != want {
		t.Fatalf("binary skill file = %#v, want %#v", got, want)
	}
}

func TestDecodePlatformExtensionArchiveImportRejectsCodeAgentResourceJSON(t *testing.T) {
	archive := platformExtensionArchive(t, map[string][]byte{
		"codeagent-extension.json": []byte(`{"name":"Demo","version":"1.0.0","description":"Demo"}`),
		"agents/coordinator.json":  []byte(`{"name":"Coordinator","leader":true,"prompt":"Coordinate."}`),
		"commands/delegate-e2e.md": []byte("---\ndescription: Delegate.\n---\nDelegate.\n"),
		"skills/evidence/SKILL.md": []byte("---\nname: Evidence\ndescription: Evidence.\n---\nEvidence.\n"),
	})

	_, _, err := decodePlatformExtensionArchiveImport(archive, DefaultPlatformExtensionV1Policy())
	if err == nil || !strings.Contains(err.Error(), "invalid agent entry agents/coordinator.json") {
		t.Fatalf("resource JSON error = %v, want Markdown-only agent entry error", err)
	}
}

func TestRuntimePoolDemoExtensionPackage(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "extensions", "runtime-pool-demo.zip"))
	if err != nil {
		t.Fatalf("read demo extension package: %v", err)
	}
	bundle, _, err := decodePlatformExtensionArchiveImport(data, DefaultPlatformExtensionV1Policy())
	if err != nil {
		t.Fatalf("decode demo extension package: %v", err)
	}
	if bundle.Extension.Key != "runtime-pool-demo" {
		t.Fatalf("extension key = %q, want runtime-pool-demo", bundle.Extension.Key)
	}
	var binaryAsset PlatformExtensionSkillFile
	for _, skill := range bundle.Skills {
		if skill.Key != "pool-evidence" {
			continue
		}
		for _, file := range skill.Files {
			if file.Path == "assets/runtime-pool-marker.bin" {
				binaryAsset = file
			}
		}
	}
	if got, want := binaryAsset.Content, "AP9QSw=="; got != want {
		t.Fatalf("binary demo asset content = %q, want %q", got, want)
	}
}

func TestDecodePlatformExtensionImportRequestUsesContentType(t *testing.T) {
	source := readPlatformExtensionSource(t)
	jsonDocument, err := json.Marshal(source)
	if err != nil {
		t.Fatalf("marshal JSON document: %v", err)
	}
	zipDocument := platformExtensionArchive(t, map[string][]byte{
		"extension.json":                              jsonDocument,
		"skills/source-review/SKILL.md":               []byte(source.Skills[0].Files[0].Content),
		"skills/source-review/references/evidence.md": []byte(source.Skills[0].Files[1].Content),
	})

	for _, tc := range []struct {
		contentType string
		body        []byte
	}{
		{contentType: "application/json", body: jsonDocument},
		{contentType: "application/zip; charset=binary", body: zipDocument},
	} {
		t.Run(tc.contentType, func(t *testing.T) {
			bundle, _, err := decodePlatformExtensionImportRequest(tc.contentType, tc.body, DefaultPlatformExtensionV1Policy())
			if err != nil {
				t.Fatalf("decodePlatformExtensionImportRequest() error = %v", err)
			}
			if bundle.Extension.Key != source.Extension.Key {
				t.Fatalf("extension key = %q, want %q", bundle.Extension.Key, source.Extension.Key)
			}
		})
	}
}

func TestDecodePlatformExtensionImportRequiresExactlyOneE2ECommand(t *testing.T) {
	for _, tc := range []struct {
		name     string
		commands []PlatformExtensionCommand
	}{
		{
			name: "missing e2e command",
			commands: []PlatformExtensionCommand{
				{Name: "delegate.flow", Metadata: json.RawMessage(`{}`)},
				{Name: "summarize", Metadata: json.RawMessage(`{}`)},
			},
		},
		{
			name: "multiple e2e commands",
			commands: []PlatformExtensionCommand{
				{Name: "delegate-e2e", Metadata: json.RawMessage(`{}`)},
				{Name: "review-e2e", Metadata: json.RawMessage(`{}`)},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := readPlatformExtensionSource(t)
			source.Commands = tc.commands
			raw, err := json.Marshal(source)
			if err != nil {
				t.Fatalf("marshal source: %v", err)
			}

			_, _, err = decodePlatformExtensionImport(raw, DefaultPlatformExtensionV1Policy())
			assertPlatformExtensionCode(t, err, "E2E_COMMAND_INVALID")
		})
	}
}

func TestDecodePlatformExtensionArchiveImportRejectsMissingOrUndeclaredSkillFiles(t *testing.T) {
	source := readPlatformExtensionSource(t)
	source.Skills[0].Files = []PlatformExtensionSkillFile{{Path: "SKILL.md"}}
	manifest, err := json.Marshal(source)
	if err != nil {
		t.Fatalf("marshal extension manifest: %v", err)
	}

	for _, tc := range []struct {
		name    string
		entries map[string][]byte
		code    string
	}{
		{
			name: "missing declared file",
			entries: map[string][]byte{
				"extension.json": manifest,
			},
			code: "EXTENSION_ARCHIVE_FILE_MISSING",
		},
		{
			name: "undeclared file",
			entries: map[string][]byte{
				"extension.json":                      manifest,
				"skills/source-review/SKILL.md":       []byte("---\nname: source-review\n---\n"),
				"skills/source-review/unexpected.bin": []byte{0x00},
			},
			code: "EXTENSION_ARCHIVE_INVALID",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := decodePlatformExtensionArchiveImport(platformExtensionArchive(t, tc.entries), DefaultPlatformExtensionV1Policy())
			assertPlatformExtensionCode(t, err, tc.code)
		})
	}
}

func platformExtensionArchive(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var data bytes.Buffer
	writer := zip.NewWriter(&data)
	for name, content := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create archive entry %q: %v", name, err)
		}
		if _, err := entry.Write(content); err != nil {
			t.Fatalf("write archive entry %q: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}
	return data.Bytes()
}
