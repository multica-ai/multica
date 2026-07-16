package handler

// CEREBRO-PATCH(run-prompt-snapshot): FIR-3212 — tests for the pure
// validation/mapping behind the prompt-snapshot ingest endpoint.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

func promptSnapshotLayer(name, delivery, content string) promptSnapshotLayerPayload {
	sum := sha256.Sum256([]byte(content))
	return promptSnapshotLayerPayload{
		Name:            name,
		Delivery:        delivery,
		ByteSize:        len(content),
		SHA256Original:  hex.EncodeToString(sum[:]),
		SHA256Redacted:  hex.EncodeToString(sum[:]),
		ContentRedacted: content,
	}
}

func validSnapshotPayload() promptSnapshotPayload {
	layers := []promptSnapshotLayerPayload{
		promptSnapshotLayer("runtime_brief", "workdir_file", "brief\n"),
		promptSnapshotLayer("task_prompt", "user_prompt", "prompt\n"),
	}
	h := sha256.New()
	for _, l := range layers {
		h.Write([]byte(l.ContentRedacted))
		h.Write([]byte{0})
	}
	combined := hex.EncodeToString(h.Sum(nil))
	return promptSnapshotPayload{
		Provider:       "claude",
		Layers:         layers,
		SHA256Original: combined, // nothing redacted in this fixture
		SHA256Redacted: combined,
		TotalBytes:     len("brief\n") + len("prompt\n"),
	}
}

func TestValidatePromptSnapshotAcceptsSelfConsistentPayload(t *testing.T) {
	if err := validatePromptSnapshotPayload(validSnapshotPayload()); err != nil {
		t.Fatalf("valid payload rejected: %v", err)
	}
}

func TestValidatePromptSnapshotRejectsRedactedHashMismatch(t *testing.T) {
	p := validSnapshotPayload()
	p.Layers[1].ContentRedacted = "tampered\n"
	err := validatePromptSnapshotPayload(p)
	if err == nil || !strings.Contains(err.Error(), "sha256_redacted") {
		t.Fatalf("tampered content must be rejected with a hash error, got %v", err)
	}
}

func TestValidatePromptSnapshotRejectsMissingFields(t *testing.T) {
	cases := map[string]func(*promptSnapshotPayload){
		"no provider":      func(p *promptSnapshotPayload) { p.Provider = "" },
		"no layers":        func(p *promptSnapshotPayload) { p.Layers = nil },
		"no original hash": func(p *promptSnapshotPayload) { p.SHA256Original = "" },
		"no redacted hash": func(p *promptSnapshotPayload) { p.SHA256Redacted = "" },
		"bad delivery":     func(p *promptSnapshotPayload) { p.Layers[0].Delivery = "carrier_pigeon" },
		"oversized payload": func(p *promptSnapshotPayload) {
			p.Layers[0].ContentRedacted = strings.Repeat("x", maxPromptSnapshotBytes+1)
		},
	}
	for name, mutate := range cases {
		p := validSnapshotPayload()
		mutate(&p)
		if err := validatePromptSnapshotPayload(p); err == nil {
			t.Errorf("%s: expected rejection", name)
		}
	}
}

func TestPromptSnapshotResponseEmitsLayersAsRawJSON(t *testing.T) {
	row := cerebrodb.CerebroRunPromptSnapshot{
		AgentContextVersion: "v1.4.0",
		Provider:            "claude",
		Model:               "claude-sonnet-5",
		RuntimeVersion:      "2.1.209",
		SystemPromptMode:    "append",
		Layers:              []byte(`[{"name":"agent_identity","delivery":"workdir_file"}]`),
		Sha256Original:      "aaa",
		Sha256Redacted:      "bbb",
		TotalBytes:          42,
		Redacted:            true,
	}
	body, err := json.Marshal(promptSnapshotResponse(row))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// The JSONB column is []byte in the generated row; Go's default encoding
	// would base64 it. The member-facing response must carry it as raw JSON.
	if !strings.Contains(string(body), `"layers":[{"name":"agent_identity","delivery":"workdir_file"}]`) {
		t.Fatalf("layers not emitted as raw JSON: %s", body)
	}
	if !strings.Contains(string(body), `"agent_context_version":"v1.4.0"`) {
		t.Fatalf("missing version provenance: %s", body)
	}
}

func TestPromptSnapshotResponseNullsEmptyLayers(t *testing.T) {
	body, err := json.Marshal(promptSnapshotResponse(cerebrodb.CerebroRunPromptSnapshot{}))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// An empty JSONB payload must not produce invalid JSON ("" is not a value).
	if !strings.Contains(string(body), `"layers":[]`) {
		t.Fatalf("empty layers should marshal as []: %s", body)
	}
}
