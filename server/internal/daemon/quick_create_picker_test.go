package daemon

import (
	"strings"
	"testing"
)

func TestQuickCreatePickerPrompt(t *testing.T) {
	t.Parallel()
	const (
		agentID = "282bdf4e-b502-4ca9-8fae-0c842f973e63"
		squadID = "aaaa1111-2222-3333-4444-555555555555"
	)
	for _, squad := range []string{"", squadID} {
		t.Run("squad="+squad, func(t *testing.T) {
			prompt := buildQuickCreatePrompt(Task{
				QuickCreatePrompt: "fix the login button color",
				AgentID:           agentID,
				Agent:             &AgentData{ID: agentID, Name: "picker-agent"},
				SquadID:           squad,
			})
			for _, want := range []string{
				"When the user did NOT name an assignee",
				"pass `--assignee-picker`",
				"On no match or ambiguous match",
				"do NOT pass `--assignee`, `--assignee-id`, or `--assignee-picker`",
				"Unrecognized assignee: X",
			} {
				if !strings.Contains(prompt, want) {
					t.Errorf("prompt missing %q", want)
				}
			}
			for _, id := range []string{agentID, squadID} {
				if strings.Contains(prompt, id) {
					t.Errorf("picker identity %s must not be copied through the model", id)
				}
			}
		})
	}
}

func TestQuickCreatePickerEnvironment(t *testing.T) {
	t.Parallel()
	const (
		agentID = "282bdf4e-b502-4ca9-8fae-0c842f973e63"
		squadID = "aaaa1111-2222-3333-4444-555555555555"
		typeKey = "MULTICA_QUICK_CREATE_DEFAULT_ASSIGNEE_TYPE"
		idKey   = "MULTICA_QUICK_CREATE_DEFAULT_ASSIGNEE_ID"
	)
	for _, tc := range []struct {
		name, prompt, squad, wantType, wantID string
	}{
		{name: "ordinary task does not export picker"},
		{name: "squad task outside quick create does not export picker", squad: squadID},
		{name: "agent", prompt: "create work", wantType: "agent", wantID: agentID},
		{name: "squad takes precedence over leader", prompt: "create work", squad: squadID, wantType: "squad", wantID: squadID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := taskMulticaEnvironment(Task{
				AgentID: agentID, QuickCreatePrompt: tc.prompt, SquadID: tc.squad,
			}, "codex", "picker-agent", "mat_fixture", "/task/config", "/task/workspaces", "https://task.example", 19514, 0, "/task/tmp")
			if tc.prompt == "" {
				for _, key := range []string{typeKey, idKey} {
					if _, ok := env[key]; ok {
						t.Errorf("non-quick-create task exported %s", key)
					}
				}
				return
			}
			if env[typeKey] != tc.wantType || env[idKey] != tc.wantID {
				t.Fatalf("picker = (%q, %q), want (%q, %q)", env[typeKey], env[idKey], tc.wantType, tc.wantID)
			}
			layerCustomEnvAndHermesHome(env, map[string]string{typeKey: "member", idKey: "wrong-owner"}, "", nil)
			if env[typeKey] != tc.wantType || env[idKey] != tc.wantID {
				t.Fatal("custom environment replaced daemon-owned picker identity")
			}
		})
	}
}
