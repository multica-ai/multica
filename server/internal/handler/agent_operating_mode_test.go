package handler

import (
	"encoding/json"
	"testing"
)

func TestAgentOperatingModeWireFields(t *testing.T) {
	mode := "hybrid"
	create := CreateAgentRequest{OperatingMode: mode}
	update := UpdateAgentRequest{OperatingMode: &mode}
	response := AgentResponse{OperatingMode: mode}
	claim := TaskAgentData{OperatingMode: mode}

	for name, value := range map[string]any{
		"create":   create,
		"update":   update,
		"response": response,
		"claim":    claim,
	} {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("marshal %s: %v", name, err)
		}
		if !json.Valid(encoded) {
			t.Fatalf("%s did not marshal to valid JSON", name)
		}
	}
}

func TestDefaultAndValidateAgentOperatingMode(t *testing.T) {
	t.Run("omitted defaults to coding", func(t *testing.T) {
		mode := ""
		if err := defaultAndValidateAgentOperatingMode(map[string]json.RawMessage{}, &mode); err != nil {
			t.Fatalf("default operating mode: %v", err)
		}
		if mode != "coding" {
			t.Fatalf("operating mode = %q, want coding", mode)
		}
	})

	for _, mode := range []string{"coding", "operational", "hybrid"} {
		mode := mode
		t.Run("accepts_"+mode, func(t *testing.T) {
			raw := map[string]json.RawMessage{"operating_mode": json.RawMessage(`"` + mode + `"`)}
			if err := defaultAndValidateAgentOperatingMode(raw, &mode); err != nil {
				t.Fatalf("validate operating mode: %v", err)
			}
		})
	}

	for _, mode := range []string{"", "admin", "CODING"} {
		mode := mode
		t.Run("rejects_"+mode, func(t *testing.T) {
			raw := map[string]json.RawMessage{"operating_mode": json.RawMessage(`"` + mode + `"`)}
			if err := defaultAndValidateAgentOperatingMode(raw, &mode); err == nil {
				t.Fatalf("operating mode %q unexpectedly accepted", mode)
			}
		})
	}
}

func TestNormaliseStoredAgentOperatingMode(t *testing.T) {
	for _, tc := range []struct {
		stored string
		want   string
	}{
		{stored: "", want: "coding"},
		{stored: "unknown", want: "coding"},
		{stored: "coding", want: "coding"},
		{stored: "operational", want: "operational"},
		{stored: "hybrid", want: "hybrid"},
	} {
		if got := normaliseStoredAgentOperatingMode(tc.stored); got != tc.want {
			t.Errorf("normaliseStoredAgentOperatingMode(%q) = %q, want %q", tc.stored, got, tc.want)
		}
	}
}
