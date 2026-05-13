package handler

import (
	"reflect"
	"testing"
)

func TestMergeAgentConfigs_AllEmpty(t *testing.T) {
	m := MergeAgentConfigs()
	if m.Instructions != "" {
		t.Errorf("expected empty instructions, got %q", m.Instructions)
	}
	if m.CustomEnv != nil {
		t.Errorf("expected nil custom_env, got %v", m.CustomEnv)
	}
	if m.CustomArgs != nil {
		t.Errorf("expected nil custom_args, got %v", m.CustomArgs)
	}
	if m.SkillIDs != nil {
		t.Errorf("expected nil skill_ids, got %v", m.SkillIDs)
	}
}

func TestMergeAgentConfigs_SingleLayer(t *testing.T) {
	m := MergeAgentConfigs(AgentConfigLayer{
		Instructions: "be helpful",
		CustomEnv:    map[string]string{"KEY": "val"},
		CustomArgs:   []string{"--verbose"},
		Skills:       []string{"s1", "s2"},
	})
	if m.Instructions != "be helpful" {
		t.Errorf("instructions = %q", m.Instructions)
	}
	if m.CustomEnv["KEY"] != "val" {
		t.Errorf("custom_env = %v", m.CustomEnv)
	}
	if !reflect.DeepEqual(m.CustomArgs, []string{"--verbose"}) {
		t.Errorf("custom_args = %v", m.CustomArgs)
	}
	if !reflect.DeepEqual(m.SkillIDs, []string{"s1", "s2"}) {
		t.Errorf("skill_ids = %v", m.SkillIDs)
	}
}

func TestMergeAgentConfigs_InstructionsAppendOrder(t *testing.T) {
	m := MergeAgentConfigs(
		AgentConfigLayer{Instructions: "system rules"},
		AgentConfigLayer{Instructions: "personal prefs"},
		AgentConfigLayer{Instructions: "agent specific"},
	)
	want := "system rules\npersonal prefs\nagent specific"
	if m.Instructions != want {
		t.Errorf("instructions =\n%q\nwant:\n%q", m.Instructions, want)
	}
}

func TestMergeAgentConfigs_InstructionsSkipEmpty(t *testing.T) {
	m := MergeAgentConfigs(
		AgentConfigLayer{Instructions: "system"},
		AgentConfigLayer{Instructions: ""},      // empty — skip
		AgentConfigLayer{Instructions: "  \t "}, // whitespace — skip
		AgentConfigLayer{Instructions: "agent"},
	)
	if m.Instructions != "system\nagent" {
		t.Errorf("instructions = %q", m.Instructions)
	}
}

func TestMergeAgentConfigs_EnvOverridePriority(t *testing.T) {
	m := MergeAgentConfigs(
		AgentConfigLayer{CustomEnv: map[string]string{"A": "1", "B": "sys"}},
		AgentConfigLayer{CustomEnv: map[string]string{"B": "personal", "C": "3"}},
		AgentConfigLayer{CustomEnv: map[string]string{"B": "agent", "D": "4"}},
	)
	want := map[string]string{"A": "1", "B": "agent", "C": "3", "D": "4"}
	if !reflect.DeepEqual(m.CustomEnv, want) {
		t.Errorf("custom_env = %v, want %v", m.CustomEnv, want)
	}
}

func TestMergeAgentConfigs_EnvNilLayersSkipped(t *testing.T) {
	m := MergeAgentConfigs(
		AgentConfigLayer{CustomEnv: nil},
		AgentConfigLayer{CustomEnv: map[string]string{"X": "1"}},
	)
	if m.CustomEnv["X"] != "1" || len(m.CustomEnv) != 1 {
		t.Errorf("custom_env = %v", m.CustomEnv)
	}
}

func TestMergeAgentConfigs_ArgsConcat(t *testing.T) {
	m := MergeAgentConfigs(
		AgentConfigLayer{CustomArgs: []string{"--sys"}},
		AgentConfigLayer{CustomArgs: []string{"--personal"}},
		AgentConfigLayer{CustomArgs: []string{"--agent1", "--agent2"}},
	)
	want := []string{"--sys", "--personal", "--agent1", "--agent2"}
	if !reflect.DeepEqual(m.CustomArgs, want) {
		t.Errorf("custom_args = %v", m.CustomArgs)
	}
}

func TestMergeAgentConfigs_ArgsNilSkipped(t *testing.T) {
	m := MergeAgentConfigs(
		AgentConfigLayer{CustomArgs: nil},
		AgentConfigLayer{CustomArgs: []string{"--only"}},
	)
	if !reflect.DeepEqual(m.CustomArgs, []string{"--only"}) {
		t.Errorf("custom_args = %v", m.CustomArgs)
	}
}

func TestMergeAgentConfigs_SkillsDedup(t *testing.T) {
	m := MergeAgentConfigs(
		AgentConfigLayer{Skills: []string{"s1", "s2"}},
		AgentConfigLayer{Skills: []string{"s2", "s3"}},
		AgentConfigLayer{Skills: []string{"s3", "s4", "s1"}},
	)
	want := []string{"s1", "s2", "s3", "s4"}
	if !reflect.DeepEqual(m.SkillIDs, want) {
		t.Errorf("skill_ids = %v, want %v", m.SkillIDs, want)
	}
}

func TestMergeAgentConfigs_SkillsEmptyNotDuplicated(t *testing.T) {
	m := MergeAgentConfigs(
		AgentConfigLayer{Skills: nil},
		AgentConfigLayer{Skills: []string{}},
		AgentConfigLayer{Skills: []string{"s1"}},
	)
	if !reflect.DeepEqual(m.SkillIDs, []string{"s1"}) {
		t.Errorf("skill_ids = %v", m.SkillIDs)
	}
}

// --- parseAgentConfigLayer tests ---

func TestParseAgentConfigLayer_EmptyData(t *testing.T) {
	l := parseAgentConfigLayer(nil, "")
	if l.Instructions != "" || l.CustomEnv != nil || l.CustomArgs != nil || l.Skills != nil {
		t.Errorf("expected zero-value layer, got %+v", l)
	}
}

func TestParseAgentConfigLayer_DirectParse(t *testing.T) {
	data := []byte(`{
		"instructions": "be nice",
		"custom_env": {"K": "V"},
		"custom_args": ["--flag"],
		"skills": ["id1"]
	}`)
	l := parseAgentConfigLayer(data, "")
	if l.Instructions != "be nice" {
		t.Errorf("instructions = %q", l.Instructions)
	}
	if l.CustomEnv["K"] != "V" {
		t.Errorf("custom_env = %v", l.CustomEnv)
	}
	if !reflect.DeepEqual(l.CustomArgs, []string{"--flag"}) {
		t.Errorf("custom_args = %v", l.CustomArgs)
	}
	if !reflect.DeepEqual(l.Skills, []string{"id1"}) {
		t.Errorf("skills = %v", l.Skills)
	}
}

func TestParseAgentConfigLayer_NestedKey(t *testing.T) {
	data := []byte(`{
		"agent_defaults": {
			"instructions": "workspace level",
			"custom_env": {"SYS": "true"},
			"skills": ["ws-skill"]
		},
		"other_setting": "ignored"
	}`)
	l := parseAgentConfigLayer(data, "agent_defaults")
	if l.Instructions != "workspace level" {
		t.Errorf("instructions = %q", l.Instructions)
	}
	if l.CustomEnv["SYS"] != "true" {
		t.Errorf("custom_env = %v", l.CustomEnv)
	}
	if !reflect.DeepEqual(l.Skills, []string{"ws-skill"}) {
		t.Errorf("skills = %v", l.Skills)
	}
}

func TestParseAgentConfigLayer_MissingNestedKey(t *testing.T) {
	data := []byte(`{"other": "value"}`)
	l := parseAgentConfigLayer(data, "agent_defaults")
	if l.Instructions != "" || l.CustomEnv != nil {
		t.Errorf("expected zero layer for missing key, got %+v", l)
	}
}

func TestParseAgentConfigLayer_InvalidJSON(t *testing.T) {
	l := parseAgentConfigLayer([]byte("not json"), "")
	if l.Instructions != "" {
		t.Errorf("expected zero layer for invalid JSON, got %+v", l)
	}
}

func TestParseAgentConfigLayer_PartialConfig(t *testing.T) {
	// Only instructions set, other fields absent
	data := []byte(`{"instructions": "just this"}`)
	l := parseAgentConfigLayer(data, "")
	if l.Instructions != "just this" {
		t.Errorf("instructions = %q", l.Instructions)
	}
	if l.CustomEnv != nil || l.CustomArgs != nil || l.Skills != nil {
		t.Errorf("expected nil for unset fields, got env=%v args=%v skills=%v", l.CustomEnv, l.CustomArgs, l.Skills)
	}
}
func TestMergeAgentConfigs_FullThreeLayer(t *testing.T) {
	system := AgentConfigLayer{
		Instructions: "Follow system rules",
		CustomEnv:    map[string]string{"NOTIFY": "email", "LOG_LEVEL": "info"},
		CustomArgs:   []string{"--timeout=30"},
		Skills:       []string{"skill-a", "skill-b"},
	}
	personal := AgentConfigLayer{
		Instructions: "I prefer concise output",
		CustomEnv:    map[string]string{"NOTIFY": "wechat"},
		CustomArgs:   []string{"--lang=zh"},
		Skills:       []string{"skill-b", "skill-c"},
	}
	agent := AgentConfigLayer{
		Instructions: "You are a code reviewer",
		CustomEnv:    map[string]string{"LOG_LEVEL": "debug", "EXTRA": "1"},
		CustomArgs:   []string{"--strict"},
		Skills:       []string{"skill-c", "skill-d"},
	}

	m := MergeAgentConfigs(system, personal, agent)

	wantInstr := "Follow system rules\nI prefer concise output\nYou are a code reviewer"
	if m.Instructions != wantInstr {
		t.Errorf("instructions mismatch:\ngot:  %q\nwant: %q", m.Instructions, wantInstr)
	}

	wantEnv := map[string]string{
		"NOTIFY":    "wechat",   // personal overrides system
		"LOG_LEVEL": "debug",    // agent overrides system
		"EXTRA":     "1",        // agent only
	}
	if !reflect.DeepEqual(m.CustomEnv, wantEnv) {
		t.Errorf("custom_env = %v, want %v", m.CustomEnv, wantEnv)
	}

	wantArgs := []string{"--timeout=30", "--lang=zh", "--strict"}
	if !reflect.DeepEqual(m.CustomArgs, wantArgs) {
		t.Errorf("custom_args = %v", m.CustomArgs)
	}

	wantSkills := []string{"skill-a", "skill-b", "skill-c", "skill-d"}
	if !reflect.DeepEqual(m.SkillIDs, wantSkills) {
		t.Errorf("skill_ids = %v, want %v", m.SkillIDs, wantSkills)
	}
}

func TestMergeAgentConfigs_OnlySystemDefaults(t *testing.T) {
	// Simulates: system has config, personal is empty, agent has its own instructions only
	m := MergeAgentConfigs(
		AgentConfigLayer{
			Instructions: "system default instructions",
			CustomEnv:    map[string]string{"SYS_KEY": "val"},
			Skills:       []string{"s1"},
		},
		AgentConfigLayer{}, // empty personal
		AgentConfigLayer{Instructions: "agent instructions"},
	)
	if m.Instructions != "system default instructions\nagent instructions" {
		t.Errorf("instructions = %q", m.Instructions)
	}
	if m.CustomEnv["SYS_KEY"] != "val" {
		t.Errorf("custom_env = %v", m.CustomEnv)
	}
	if !reflect.DeepEqual(m.SkillIDs, []string{"s1"}) {
		t.Errorf("skill_ids = %v", m.SkillIDs)
	}
}