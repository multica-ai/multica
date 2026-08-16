package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

func pluginInitTestCommand(output *bytes.Buffer) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetOut(output)
	cmd.Flags().String("key", "dev.example.private-skill", "")
	cmd.Flags().String("name", "Private Skill", "")
	cmd.Flags().String("publisher", "example.local", "")
	return cmd
}

func TestPluginInitValidateAndDeterministicPack(t *testing.T) {
	root := filepath.Join(t.TempDir(), "plugin")
	var output bytes.Buffer
	if err := runPluginInit(pluginInitTestCommand(&output), []string{root}); err != nil {
		t.Fatalf("plugin init: %v", err)
	}

	validate := &cobra.Command{}
	validate.SetOut(&output)
	validate.Flags().String("output", "json", "")
	output.Reset()
	if err := runPluginValidate(validate, []string{root}); err != nil {
		t.Fatalf("plugin validate: %v", err)
	}
	var digest pluginDigestOutput
	if err := json.Unmarshal(output.Bytes(), &digest); err != nil {
		t.Fatalf("decode validate output: %v\n%s", err, output.String())
	}
	if digest.PluginKey != "dev.example.private-skill" || digest.Version != "0.1.0" || digest.ArchiveDigest == "" || digest.ArtifactDigest == "" {
		t.Fatalf("unexpected validate output: %+v", digest)
	}

	first := filepath.Join(t.TempDir(), "first.zip")
	second := filepath.Join(t.TempDir(), "second.zip")
	for _, destination := range []string{first, second} {
		pack := &cobra.Command{}
		pack.SetOut(&output)
		pack.Flags().String("output", destination, "")
		pack.Flags().String("format", "json", "")
		output.Reset()
		if err := runPluginPack(pack, []string{root}); err != nil {
			t.Fatalf("plugin pack: %v", err)
		}
	}
	firstBytes, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	secondBytes, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstBytes, secondBytes) {
		t.Fatal("plugin pack output was not deterministic")
	}
	if _, _, err := loadPluginSource(first); err != nil {
		t.Fatalf("load packed archive: %v", err)
	}
}

func TestPluginInitRefusesNonEmptyDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runPluginInit(pluginInitTestCommand(&bytes.Buffer{}), []string{root}); err == nil {
		t.Fatal("plugin init unexpectedly replaced a non-empty directory")
	}
}

func TestPluginInitRemoteMCPTemplate(t *testing.T) {
	root := filepath.Join(t.TempDir(), "plugin")
	cmd := pluginInitTestCommand(&bytes.Buffer{})
	cmd.Flags().String("contribution", "remote-mcp", "")
	cmd.Flags().String("endpoint-host", "tools.example.com", "")
	if err := runPluginInit(cmd, []string{root}); err != nil {
		t.Fatalf("plugin init remote-mcp: %v", err)
	}
	_, artifact, err := loadPluginSource(root)
	if err != nil {
		t.Fatalf("load remote-mcp template: %v", err)
	}
	if len(artifact.Manifest.Contributes.RemoteMCP) != 1 || len(artifact.Files) != 1 {
		t.Fatalf("remote-mcp template = %+v", artifact.Manifest.Contributes)
	}
}
