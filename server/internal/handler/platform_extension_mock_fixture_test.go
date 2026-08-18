package handler

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimePoolDemoExtensionFixtureCompiles(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("../../../testdata/extensions", "runtime-pool-demo.zip"))
	if err != nil {
		t.Fatalf("read mock extension fixture: %v", err)
	}

	bundle, _, err := decodePlatformExtensionArchiveImport(data, DefaultPlatformExtensionV1Policy())
	if err != nil {
		t.Fatalf("compile mock extension fixture: %v", err)
	}
	if bundle.Extension.Key != "runtime-pool-demo" {
		t.Fatalf("extension key = %q, want runtime-pool-demo", bundle.Extension.Key)
	}
	if len(bundle.Agents) != 3 || len(bundle.Skills) != 2 || len(bundle.FlowCommands) != 1 || len(bundle.RuntimeCommands) != 2 {
		t.Fatalf("compiled resources = agents:%d skills:%d flow:%d runtime:%d; want 3/2/1/2", len(bundle.Agents), len(bundle.Skills), len(bundle.FlowCommands), len(bundle.RuntimeCommands))
	}
}
