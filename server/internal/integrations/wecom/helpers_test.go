package wecom

// helpers_test.go — pure, no-network guards for the small building blocks:
// the install-config JSON round trip, the binding-token hash, and the
// issue-created confirmation text.

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestEncodeInstallConfigRoundTrip(t *testing.T) {
	t.Parallel()
	in := Installation{BotID: "bot-xyz", SecretEncrypted: []byte("sealed")}
	cfg, err := encodeInstallConfig(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, err := installationFromRow(db.ChannelInstallation{Config: cfg, Status: "active"})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.BotID != "bot-xyz" {
		t.Errorf("BotID round trip = %q, want bot-xyz", out.BotID)
	}
	if string(out.SecretEncrypted) != "sealed" {
		t.Errorf("SecretEncrypted round trip = %q, want sealed", out.SecretEncrypted)
	}
	if out.Status != InstallationActive {
		t.Errorf("Status = %q, want active", out.Status)
	}
}

func TestEncodeInstallConfigRequiresBotID(t *testing.T) {
	t.Parallel()
	if _, err := encodeInstallConfig(Installation{}); err == nil {
		t.Error("encodeInstallConfig with no bot_id should error")
	}
}

func TestHashBindingTokenStableAndDistinct(t *testing.T) {
	t.Parallel()
	h1 := hashBindingToken("token-a")
	if h1 != hashBindingToken("token-a") {
		t.Error("hash is not stable for the same input")
	}
	if h1 == hashBindingToken("token-b") {
		t.Error("different tokens hashed to the same value")
	}
	if len(h1) != 64 { // sha256 hex
		t.Errorf("hash length = %d, want 64 hex chars", len(h1))
	}
	// The raw token must not be recoverable from / equal to its hash.
	if strings.Contains(h1, "token-a") {
		t.Error("hash leaks the raw token")
	}
}

func TestRandomBindingTokenIsUnique(t *testing.T) {
	t.Parallel()
	a, err := randomBindingToken(24)
	if err != nil {
		t.Fatalf("randomBindingToken: %v", err)
	}
	b, _ := randomBindingToken(24)
	if a == "" || a == b {
		t.Errorf("expected two distinct non-empty tokens, got %q and %q", a, b)
	}
}

func TestValidateInstallationParams(t *testing.T) {
	t.Parallel()
	valid := InstallationParams{
		WorkspaceID:     mustTestUUID(t),
		AgentID:         mustTestUUID(t),
		InstallerUserID: mustTestUUID(t),
		BotID:           "bot-1",
		Secret:          "s3cr3t",
	}
	if err := validateInstallationParams(valid); err != nil {
		t.Fatalf("valid params rejected: %v", err)
	}
	// Each required field, cleared in turn, must be rejected.
	clearers := map[string]func(p *InstallationParams){
		"workspace_id": func(p *InstallationParams) { p.WorkspaceID = pgtype.UUID{} },
		"agent_id":     func(p *InstallationParams) { p.AgentID = pgtype.UUID{} },
		"installer":    func(p *InstallationParams) { p.InstallerUserID = pgtype.UUID{} },
		"bot_id":       func(p *InstallationParams) { p.BotID = "" },
		"secret":       func(p *InstallationParams) { p.Secret = "" },
	}
	for name, clear := range clearers {
		p := valid
		clear(&p)
		if err := validateInstallationParams(p); err == nil {
			t.Errorf("missing %s should be rejected", name)
		}
	}
}

func TestIssueCreatedText(t *testing.T) {
	t.Parallel()
	withTitle := issueCreatedText(engine.Result{IssueIdentifier: "MUL-42", IssueTitle: "Fix the thing"})
	if !strings.Contains(withTitle, "MUL-42") || !strings.Contains(withTitle, "Fix the thing") {
		t.Errorf("issueCreatedText = %q, want identifier + title", withTitle)
	}
	noTitle := issueCreatedText(engine.Result{IssueIdentifier: "MUL-43"})
	if !strings.Contains(noTitle, "MUL-43") || strings.Contains(noTitle, "—") {
		t.Errorf("issueCreatedText without title = %q, want just the identifier", noTitle)
	}
	noID := issueCreatedText(engine.Result{IssueNumber: 7})
	if !strings.Contains(noID, "#7") {
		t.Errorf("issueCreatedText with no identifier = %q, want #7 fallback", noID)
	}
}
