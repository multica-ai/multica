package daemon

// CEREBRO-PATCH(run-prompt-snapshot): FIR-3212 — tests for the byte-exact
// per-run production prompt snapshot the daemon captures at spawn.

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// canonicalHash mirrors the documented serialization: SHA-256 over each
// layer's content bytes in order, each terminated by a single 0x00 byte.
func canonicalHash(contents ...string) string {
	h := sha256.New()
	for _, c := range contents {
		h.Write([]byte(c))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func TestBuildPromptSnapshotLayersAndHashes(t *testing.T) {
	task := Task{
		ID:      "task-1",
		AgentID: "agent-1",
		IssueID: "issue-1",
		Agent:   &AgentData{ContextVersion: "1.4.0"},
	}
	brief := "# Brief\nrules here\n"
	prompt := "You are running as a local coding agent.\n"

	snap := buildPromptSnapshot(promptSnapshotInput{
		Task:             task,
		Provider:         "claude",
		Model:            "claude-fable-5",
		RuntimeVersion:   "2.1.209",
		SystemPromptMode: "append",
		RuntimeBrief:     brief,
		BriefInline:      false,
		Prompt:           prompt,
	})

	if snap.TaskID != "task-1" || snap.AgentID != "agent-1" || snap.IssueID != "issue-1" {
		t.Fatalf("identity fields wrong: %+v", snap)
	}
	if snap.AgentContextVersion != "1.4.0" {
		t.Fatalf("context version = %q, want 1.4.0", snap.AgentContextVersion)
	}
	if len(snap.Layers) != 2 {
		t.Fatalf("layers = %d, want 2 (brief + prompt)", len(snap.Layers))
	}
	if snap.Layers[0].Name != "runtime_brief" || snap.Layers[0].Delivery != "workdir_file" {
		t.Fatalf("layer 0 = %+v", snap.Layers[0])
	}
	if snap.Layers[1].Name != "task_prompt" || snap.Layers[1].Delivery != "user_prompt" {
		t.Fatalf("layer 1 = %+v", snap.Layers[1])
	}
	if snap.Layers[0].ContentRedacted != brief || snap.Layers[1].ContentRedacted != prompt {
		t.Fatal("clean content must be stored verbatim")
	}
	wantCombined := canonicalHash(brief, prompt)
	if snap.SHA256Original != wantCombined {
		t.Fatalf("sha256_original = %s, want %s", snap.SHA256Original, wantCombined)
	}
	// Nothing redacted → both hashes equal, redacted=false.
	if snap.SHA256Redacted != snap.SHA256Original || snap.Redacted {
		t.Fatalf("clean snapshot must have equal hashes and redacted=false: %+v", snap)
	}
	if snap.TotalBytes != len(brief)+len(prompt) {
		t.Fatalf("total bytes = %d, want %d", snap.TotalBytes, len(brief)+len(prompt))
	}
}

func TestBuildPromptSnapshotInlineBriefDelivery(t *testing.T) {
	snap := buildPromptSnapshot(promptSnapshotInput{
		Task:         Task{ID: "t", AgentID: "a"},
		Provider:     "kiro",
		RuntimeBrief: "brief",
		BriefInline:  true,
		Prompt:       "p",
	})
	if snap.Layers[0].Delivery != "system_prompt" {
		t.Fatalf("inline brief delivery = %q, want system_prompt", snap.Layers[0].Delivery)
	}
}

func TestBuildPromptSnapshotEmptyBriefOmitsLayer(t *testing.T) {
	snap := buildPromptSnapshot(promptSnapshotInput{
		Task:   Task{ID: "t", AgentID: "a"},
		Prompt: "only prompt",
	})
	if len(snap.Layers) != 1 || snap.Layers[0].Name != "task_prompt" {
		t.Fatalf("empty brief must yield a single task_prompt layer: %+v", snap.Layers)
	}
	if snap.SHA256Original != canonicalHash("only prompt") {
		t.Fatal("hash must cover exactly the layers present")
	}
}

func TestBuildPromptSnapshotRedactsSecretsButKeepsOriginalHash(t *testing.T) {
	secret := "sk-live-supersecretvalue123"
	brief := "config with token=" + secret + " embedded\n"
	prompt := "prompt also leaks " + secret + "\n"

	snap := buildPromptSnapshot(promptSnapshotInput{
		Task:         Task{ID: "t", AgentID: "a"},
		RuntimeBrief: brief,
		Prompt:       prompt,
		Secrets:      []string{secret},
	})

	for _, l := range snap.Layers {
		if strings.Contains(l.ContentRedacted, secret) {
			t.Fatalf("secret survived redaction in layer %s", l.Name)
		}
		if !strings.Contains(l.ContentRedacted, redactedPlaceholder) {
			t.Fatalf("layer %s must mark where redaction happened", l.Name)
		}
	}
	// Original hash proves what was actually sent (pre-redaction)...
	if snap.SHA256Original != canonicalHash(brief, prompt) {
		t.Fatal("sha256_original must cover the pre-redaction bytes")
	}
	// ...while the redacted hash covers what is stored/displayed.
	if snap.SHA256Redacted == snap.SHA256Original {
		t.Fatal("redacted hash must differ when content was redacted")
	}
	if !snap.Redacted {
		t.Fatal("redacted flag must be set")
	}
	// Stored (redacted) view must hash to sha256_redacted — the row is
	// self-verifying.
	var redactedContents []string
	for _, l := range snap.Layers {
		redactedContents = append(redactedContents, l.ContentRedacted)
	}
	if snap.SHA256Redacted != canonicalHash(redactedContents...) {
		t.Fatal("sha256_redacted must match the stored layer contents")
	}
}

func TestBuildPromptSnapshotIgnoresShortSecrets(t *testing.T) {
	// 1-5 char "secrets" (e.g. "1", "ok") would shred the prompt with false
	// positives; the redactor must skip them.
	snap := buildPromptSnapshot(promptSnapshotInput{
		Task:    Task{ID: "t", AgentID: "a"},
		Prompt:  "value is 1 and mode is on",
		Secrets: []string{"1", "on", ""},
	})
	if snap.Redacted {
		t.Fatalf("short secrets must not trigger redaction: %+v", snap.Layers)
	}
}

func TestBuildPromptSnapshotNilAgentSafe(t *testing.T) {
	// The claim path can deliver a task without Agent (see
	// systemPromptModeForTask) — the snapshot must not panic and must leave
	// the version empty rather than inventing one.
	snap := buildPromptSnapshot(promptSnapshotInput{
		Task:   Task{ID: "t", AgentID: "a", Agent: nil},
		Prompt: "p",
	})
	if snap.AgentContextVersion != "" {
		t.Fatalf("nil agent must yield empty context version, got %q", snap.AgentContextVersion)
	}
}

func TestSnapshotSecretsForTaskCollectsSpawnSecrets(t *testing.T) {
	task := Task{
		Agent: &AgentData{
			CustomEnv:        map[string]string{"API_KEY": "customenvsecret1"},
			InfisicalSecrets: map[string]string{"DB_PASS": "infisicalsecret1"},
		},
	}
	got := snapshotSecretsForTask(task, "agenttokensecret")
	want := map[string]bool{
		"customenvsecret1": true,
		"infisicalsecret1": true,
		"agenttokensecret": true,
	}
	if len(got) != len(want) {
		t.Fatalf("secrets = %v", got)
	}
	for _, s := range got {
		if !want[s] {
			t.Fatalf("unexpected secret %q in %v", s, got)
		}
	}
}
