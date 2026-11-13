package daemon

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

func receiptSource(t *testing.T, r ContextReceipt, kind, name string) ContextReceiptSource {
	t.Helper()
	for _, s := range r.Sources {
		if s.Kind == kind && s.Name == name {
			return s
		}
	}
	t.Fatalf("receipt has no %s source named %q: %+v", kind, name, r.Sources)
	return ContextReceiptSource{}
}

func receiptHasKind(r ContextReceipt, kind string) bool {
	for _, s := range r.Sources {
		if s.Kind == kind {
			return true
		}
	}
	return false
}

func TestBuildContextReceiptFileDeliveredBrief(t *testing.T) {
	workDir := t.TempDir()
	r := buildContextReceipt(contextReceiptInput{
		Provider:       "kiro",
		RuntimeVersion: "kiro-cli 2.34.0",
		Model:          "claude-opus",
		WorkDir:        workDir,
		Prompt:         "do the task",
		RuntimeBrief:   "# Multica Agent Runtime\nbrief",
		BriefWritten:   true,
		ResumedSession: true,
		MCPConfig:      json.RawMessage(`{"mcpServers":{"zeta":{"command":"z","env":{"TOKEN":"secret"}},"alpha":{"url":"https://x"}}}`),
		TaskCtx: execenv.TaskContextForEnv{
			AgentInstructions:  "be precise",
			ProjectDescription: "project ctx",
		},
		Skills: []execenv.SkillReceipt{{
			Name: "pr-review", Path: ".kiro/skills/pr-review/SKILL.md",
			Bytes: 30, SHA256: "0123456789ab", Available: true,
		}},
		ExistingInstructions: "repo rules",
	})

	if r.SchemaVersion != ContextReceiptSchemaVersion || r.Runtime != "kiro" || !r.ResumedSession {
		t.Fatalf("unexpected header: %+v", r)
	}
	if r.PromptBytes != len("do the task") || r.BriefBytes != len("# Multica Agent Runtime\nbrief") {
		t.Fatalf("sizes: prompt=%d brief=%d", r.PromptBytes, r.BriefBytes)
	}
	if got := strings.Join(r.MCPServers, ","); got != "alpha,zeta" {
		t.Fatalf("mcp servers = %q, want sorted names only", got)
	}

	brief := receiptSource(t, r, ReceiptKindRuntimeBrief, "")
	if brief.Delivery != ReceiptDeliveryFile || brief.Path != "AGENTS.md" {
		t.Fatalf("brief delivery = %+v", brief)
	}
	// kiro was probed at 2.13.0; the run used 2.34.0, so the receipt must not
	// claim a full verification.
	if brief.Load != ReceiptLoadVerifiedOtherVersion || brief.VerifiedVersion != "2.13.0" {
		t.Fatalf("brief load = %q (verified at %q)", brief.Load, brief.VerifiedVersion)
	}
	if r.BriefDelivery != ReceiptDeliveryFile {
		t.Fatalf("brief_delivery = %q", r.BriefDelivery)
	}

	instr := receiptSource(t, r, ReceiptKindAgentInstructions, "")
	if instr.Parent != ReceiptKindRuntimeBrief || instr.Delivery != ReceiptDeliveryEmbedded || instr.Load != brief.Load {
		t.Fatalf("instructions row = %+v", instr)
	}
	if instr.SHA256 != receiptHash("be precise") || len(instr.SHA256) != 12 {
		t.Fatalf("instructions hash = %q", instr.SHA256)
	}
	receiptSource(t, r, ReceiptKindProjectDescription, "")

	existing := receiptSource(t, r, ReceiptKindExistingInstructions, "")
	if existing.Path != "AGENTS.md" || existing.Bytes != len("repo rules") {
		t.Fatalf("existing row = %+v", existing)
	}

	prompt := receiptSource(t, r, ReceiptKindTaskPrompt, "")
	if prompt.Load != ReceiptLoadDelivered || prompt.Delivery != ReceiptDeliveryPrompt {
		t.Fatalf("prompt row = %+v", prompt)
	}

	skill := receiptSource(t, r, ReceiptKindSkill, "pr-review")
	if skill.Path != ".kiro/skills/pr-review/SKILL.md" || skill.Load != ReceiptLoadUnverified {
		t.Fatalf("skill row = %+v", skill)
	}
	if skill.Bytes != 30 || skill.SHA256 != "0123456789ab" {
		t.Fatalf("skill metadata = %+v, want the prepared package's fingerprint", skill)
	}

	// Metadata only: no content or MCP secret may appear in the wire body.
	wire, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"be precise", "project ctx", "repo rules", "do the task", "secret", "https://x", "body"} {
		if strings.Contains(string(wire), leaked) {
			t.Fatalf("receipt leaks content %q: %s", leaked, wire)
		}
	}
}

func TestBuildContextReceiptLoadStates(t *testing.T) {
	workDir := t.TempDir()
	cases := []struct {
		name     string
		in       contextReceiptInput
		delivery string
		load     string
		path     string
	}{
		{
			name:     "probed runtime at its probed version",
			in:       contextReceiptInput{Provider: "claude", RuntimeVersion: "2.1.220 (Claude Code)"},
			delivery: ReceiptDeliveryFile, load: ReceiptLoadVerified, path: "CLAUDE.md",
		},
		{
			name:     "probed runtime with unknown version",
			in:       contextReceiptInput{Provider: "codex"},
			delivery: ReceiptDeliveryFile, load: ReceiptLoadVerifiedOtherVersion, path: "AGENTS.md",
		},
		{
			name:     "unprobed runtime",
			in:       contextReceiptInput{Provider: "qwen", RuntimeVersion: "0.9.0"},
			delivery: ReceiptDeliveryFile, load: ReceiptLoadUnverified, path: "QWEN.md",
		},
		{
			name:     "custom command at vendor probed version is not the probed runtime",
			in:       contextReceiptInput{Provider: "claude", RuntimeVersion: "2.1.220", CustomRuntime: true},
			delivery: ReceiptDeliveryFile, load: ReceiptLoadUnverified, path: "CLAUDE.md",
		},
		{
			name:     "inline system prompt wins over the file",
			in:       contextReceiptInput{Provider: "traecli", InlineBrief: true},
			delivery: ReceiptDeliveryInline, load: ReceiptLoadDelivered,
		},
		{
			name:     "no file target and not inline",
			in:       contextReceiptInput{Provider: "no-such-runtime"},
			delivery: ReceiptDeliveryNone, load: ReceiptLoadNotDelivered,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.in.WorkDir = workDir
			tc.in.RuntimeBrief = "brief"
			tc.in.BriefWritten = true
			r := buildContextReceipt(tc.in)
			brief := receiptSource(t, r, ReceiptKindRuntimeBrief, "")
			if brief.Delivery != tc.delivery || brief.Load != tc.load || brief.Path != tc.path {
				t.Fatalf("brief = %+v, want delivery=%s load=%s path=%q", brief, tc.delivery, tc.load, tc.path)
			}
		})
	}
}

func TestBuildContextReceiptOmitsEmptySections(t *testing.T) {
	r := buildContextReceipt(contextReceiptInput{Provider: "claude", WorkDir: t.TempDir(), ExistingInstructions: ""})
	for _, kind := range []string{ReceiptKindAgentInstructions, ReceiptKindProjectDescription, ReceiptKindExistingInstructions, ReceiptKindSkill} {
		if receiptHasKind(r, kind) {
			t.Fatalf("empty %s should not be listed: %+v", kind, r.Sources)
		}
	}
	if r.MCPServers == nil {
		t.Fatal("mcp_servers must encode as [] rather than null")
	}
}

func TestSkillDeliveryLocationForHomeBasedRuntimes(t *testing.T) {
	if got := execenv.SkillDeliveryLocation(t.TempDir(), "codex"); got != "$CODEX_HOME/skills" {
		t.Fatalf("codex skill path = %q", got)
	}
}

func TestExistingRuntimeConfigContentStripsManagedBlock(t *testing.T) {
	workDir := t.TempDir()
	path := filepath.Join(workDir, "AGENTS.md")
	if err := os.WriteFile(path, []byte("# Repo rules\nuse tabs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := execenv.InjectRuntimeConfig(workDir, "codex", execenv.TaskContextForEnv{IssueID: "i"}); err != nil {
		t.Fatal(err)
	}
	if got := execenv.ExistingRuntimeConfigContent(workDir, "codex"); got != "# Repo rules\nuse tabs\n" {
		t.Fatalf("existing content = %q", got)
	}

	// A file the daemon created from nothing has no pre-existing content.
	fresh := t.TempDir()
	if _, err := execenv.InjectRuntimeConfig(fresh, "claude", execenv.TaskContextForEnv{IssueID: "i"}); err != nil {
		t.Fatal(err)
	}
	if got := execenv.ExistingRuntimeConfigContent(fresh, "claude"); got != "" {
		t.Fatalf("daemon-created file reported existing content %q", got)
	}
}

func TestReportContextReceiptPostsAndToleratesOldServer(t *testing.T) {
	var gotPath string
	var gotBody map[string]json.RawMessage
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.WriteHeader(status)
	}))
	defer srv.Close()

	d := &Daemon{client: NewClient(srv.URL)}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	receipt := buildContextReceipt(contextReceiptInput{Provider: "claude", WorkDir: t.TempDir(), Prompt: "p"})

	d.reportContextReceipt(context.Background(), "task-1", receipt, logger)
	if gotPath != "/api/daemon/tasks/task-1/context-receipt" {
		t.Fatalf("posted to %q", gotPath)
	}
	var echoed ContextReceipt
	if err := json.Unmarshal(gotBody["receipt"], &echoed); err != nil || echoed.Runtime != "claude" {
		t.Fatalf("body receipt = %s (%v)", gotBody["receipt"], err)
	}

	// An older server without the route answers 404; the report must simply
	// return (the run is not affected).
	status = http.StatusNotFound
	d.reportContextReceipt(context.Background(), "task-1", receipt, logger)
}

func TestContextReceiptFailedBriefWrite(t *testing.T) {
	workDir := t.TempDir()
	// A directory at the instruction-file path rejects the write on every
	// platform, including test runners allowed to overwrite read-only files.
	if err := os.Mkdir(filepath.Join(workDir, "CLAUDE.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	taskCtx := execenv.TaskContextForEnv{AgentInstructions: "required instruction"}
	brief, err := execenv.InjectRuntimeConfig(workDir, "claude", taskCtx)
	if err == nil {
		t.Fatal("expected injection to fail")
	}
	r := buildContextReceipt(contextReceiptInput{
		Provider: "claude", RuntimeVersion: "2.1.220", WorkDir: workDir,
		RuntimeBrief: brief, TaskCtx: taskCtx,
	})
	for _, kind := range []string{ReceiptKindRuntimeBrief, ReceiptKindAgentInstructions} {
		s := receiptSource(t, r, kind, "")
		if s.Load != ReceiptLoadNotDelivered {
			t.Errorf("%s after failed write: load = %s, want not_delivered", kind, s.Load)
		}
	}
}

func TestContextReceiptUsesMaterializedSkill(t *testing.T) {
	taskCtx := execenv.TaskContextForEnv{AgentSkills: []execenv.SkillContextForEnv{{
		Name: "Code Review", Content: "Be concise.",
	}}}
	env, err := execenv.Prepare(execenv.PrepareParams{
		WorkspacesRoot: t.TempDir(), WorkspaceID: "receipt-workspace",
		TaskID: "receipt-task", Provider: "kiro", Task: taskCtx,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = env.Cleanup(true) })
	taskCtx = execenv.ApplyPreparedSkillNames(taskCtx, env.PreparedSkillNames)
	r := buildContextReceipt(contextReceiptInput{
		Provider: "kiro", WorkDir: env.WorkDir, TaskCtx: taskCtx,
		Skills: env.SkillReceipts("kiro", taskCtx.AgentSkills),
	})
	source := receiptSource(t, r, ReceiptKindSkill, taskCtx.AgentSkills[0].Name)
	actual, err := os.ReadFile(filepath.Join(env.WorkDir, filepath.FromSlash(source.Path)))
	if err != nil {
		t.Fatal(err)
	}
	if source.Bytes != len(actual) {
		t.Errorf("receipt bytes = %d, materialized file bytes = %d", source.Bytes, len(actual))
	}
	if source.SHA256 == receiptHash(taskCtx.AgentSkills[0].Content) || len(source.SHA256) != 12 {
		t.Errorf("skill fingerprint = %q, want the materialized package fingerprint", source.SHA256)
	}
}
