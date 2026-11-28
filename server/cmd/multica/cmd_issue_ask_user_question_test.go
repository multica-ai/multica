package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// askCmd builds a throwaway command carrying the ask_user_question flags that
// buildAskUserQuestionMeta reads.
func askCmd() *cobra.Command {
	c := &cobra.Command{Use: "test"}
	c.Flags().String("target-user", "", "")
	c.Flags().String("question", "", "")
	c.Flags().String("options-json", "", "")
	c.Flags().Bool("multi-select", false, "")
	c.Flags().Bool("allow-custom", false, "")
	return c
}

func TestBuildAskUserQuestionMeta_Valid(t *testing.T) {
	c := askCmd()
	// A UUID target-user short-circuits member resolution, so no client needed.
	_ = c.Flags().Set("target-user", "f31a2be0-0f7d-46b5-abf4-b8ff65846261")
	_ = c.Flags().Set("question", "Which cache?")
	_ = c.Flags().Set("options-json", `[{"label":"Redis","description":"distributed"},{"label":"Local","description":"single-node"}]`)

	meta, fallback, err := buildAskUserQuestionMeta(context.Background(), nil, c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	aq, ok := meta["ask_user_question"].(map[string]any)
	if !ok {
		t.Fatalf("missing ask_user_question key: %#v", meta)
	}
	if aq["target_user"] != "f31a2be0-0f7d-46b5-abf4-b8ff65846261" {
		t.Errorf("target_user: got %v", aq["target_user"])
	}
	if aq["question"] != "Which cache?" {
		t.Errorf("question: got %v", aq["question"])
	}
	// source_user must NOT be set client-side (server fills it from the author).
	if _, present := aq["source_user"]; present {
		t.Errorf("source_user must not be set by CLI")
	}
	// Options must round-trip.
	b, _ := json.Marshal(aq["options"])
	if !strings.Contains(string(b), "Redis") || !strings.Contains(string(b), "single-node") {
		t.Errorf("options not preserved: %s", b)
	}
	// Fallback markdown must mention both labels for old-client degradation.
	if !strings.Contains(fallback, "Redis") || !strings.Contains(fallback, "Local") {
		t.Errorf("fallback markdown missing labels: %q", fallback)
	}
}

func TestBuildAskUserQuestionMeta_Errors(t *testing.T) {
	cases := []struct {
		name        string
		target      string
		question    string
		optionsJSON string
		wantErr     string
	}{
		{"missing target", "", "q", `[{"label":"a","description":"b"}]`, "target-user"},
		{"missing question", "f31a2be0-0f7d-46b5-abf4-b8ff65846261", "", `[{"label":"a","description":"b"}]`, "question"},
		{"missing options", "f31a2be0-0f7d-46b5-abf4-b8ff65846261", "q", "", "options-json"},
		{"bad json", "f31a2be0-0f7d-46b5-abf4-b8ff65846261", "q", `not json`, "invalid --options-json"},
		{"empty options", "f31a2be0-0f7d-46b5-abf4-b8ff65846261", "q", `[]`, "at least one option"},
		{"option missing label", "f31a2be0-0f7d-46b5-abf4-b8ff65846261", "q", `[{"description":"b"}]`, "label is required"},
		{"option missing description", "f31a2be0-0f7d-46b5-abf4-b8ff65846261", "q", `[{"label":"a"}]`, "description is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := askCmd()
			_ = c.Flags().Set("target-user", tc.target)
			_ = c.Flags().Set("question", tc.question)
			_ = c.Flags().Set("options-json", tc.optionsJSON)
			_, _, err := buildAskUserQuestionMeta(context.Background(), nil, c)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestBuildAskUserQuestionMeta_MultiSelectAndCustom(t *testing.T) {
	c := askCmd()
	_ = c.Flags().Set("target-user", "f31a2be0-0f7d-46b5-abf4-b8ff65846261")
	_ = c.Flags().Set("question", "Pick some")
	_ = c.Flags().Set("options-json", `[{"label":"A","description":"a"},{"label":"B","description":"b"}]`)
	_ = c.Flags().Set("multi-select", "true")
	_ = c.Flags().Set("allow-custom", "true")

	meta, fallback, err := buildAskUserQuestionMeta(context.Background(), nil, c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	aq := meta["ask_user_question"].(map[string]any)
	if aq["multi_select"] != true {
		t.Errorf("multi_select: got %v", aq["multi_select"])
	}
	if aq["allow_custom"] != true {
		t.Errorf("allow_custom: got %v", aq["allow_custom"])
	}
	// Fallback markdown should mention multi + custom hint and an "其他" row.
	if !strings.Contains(fallback, "多选") || !strings.Contains(fallback, "其他") {
		t.Errorf("fallback missing multi/custom markers: %q", fallback)
	}
}

func TestBuildAskUserQuestionMeta_DefaultsFalse(t *testing.T) {
	c := askCmd()
	_ = c.Flags().Set("target-user", "f31a2be0-0f7d-46b5-abf4-b8ff65846261")
	_ = c.Flags().Set("question", "Q")
	_ = c.Flags().Set("options-json", `[{"label":"A","description":"a"}]`)

	meta, _, err := buildAskUserQuestionMeta(context.Background(), nil, c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	aq := meta["ask_user_question"].(map[string]any)
	if aq["multi_select"] != false || aq["allow_custom"] != false {
		t.Errorf("defaults should be false: multi=%v custom=%v", aq["multi_select"], aq["allow_custom"])
	}
}
