package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/spf13/cobra"
)

func TestRunVersionJSONIncludesExecutablePath(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("output", "json", "")
	if err := cmd.Flags().Set("output", "json"); err != nil {
		t.Fatalf("Flags().Set(output) error = %v", err)
	}

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	origStdout := versionCmd.OutOrStdout()
	versionCmd.SetOut(&buf)
	defer versionCmd.SetOut(origStdout)

	if err := runVersion(cmd, nil); err != nil {
		t.Fatalf("runVersion() error = %v", err)
	}

	var got map[string]string
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v; output=%q", err, buf.String())
	}
	if got["executable_path"] == "" {
		t.Fatalf("expected executable_path in JSON output, got %v", got)
	}
}
