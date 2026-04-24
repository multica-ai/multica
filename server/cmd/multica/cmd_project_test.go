package main

import "testing"

func TestParseRepoInputMachinePaths(t *testing.T) {
	input := `{"url":"https://example.com/org/repo.git","source_branch":"main","target_branch":"agent/work","machine_paths":{"dev-laptop":"/Users/alice/src/repo"}}`

	got, err := parseRepoInput(input)
	if err != nil {
		t.Fatalf("parseRepoInput() error = %v", err)
	}

	if got.Identifier != "https://example.com/org/repo.git" {
		t.Fatalf("Identifier = %q", got.Identifier)
	}
	if got.SourceBranch != "main" {
		t.Fatalf("SourceBranch = %q", got.SourceBranch)
	}
	if got.TargetBranch != "agent/work" {
		t.Fatalf("TargetBranch = %q", got.TargetBranch)
	}
	if got.MachinePaths["dev-laptop"] != "/Users/alice/src/repo" {
		t.Fatalf("MachinePaths = %#v", got.MachinePaths)
	}
}
