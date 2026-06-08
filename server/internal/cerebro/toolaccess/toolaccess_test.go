package toolaccess

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/runtimetools"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
)

func TestExposureDeniedWhenWorkspacePolicyDenies(t *testing.T) {
	svc := New(fakeRuntimeTools{
		tools:  []runtimetools.Tool{{Name: "web_fetch", Source: runtimetools.SourceCloud, Enabled: true}},
		grants: []runtimetools.ResolvedTool{{Name: "web_fetch"}},
	}, fakePolicy{eff: toolpolicy.Effective{Setting: toolpolicy.SettingDeny, DecidedBy: toolpolicy.LayerWorkspace, Reason: "Denied by workspace"}})

	got, err := svc.ListEffectiveTools(context.Background(), Query{
		WorkspaceID:     uuid(1),
		RuntimeID:       uuid(2),
		RuntimeMode:     "cloud",
		RuntimeProvider: "firtal-gateway",
		AgentID:         uuid(3),
		UserID:          uuid(4),
	})
	if err != nil {
		t.Fatalf("ListEffectiveTools: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("rows = %d, want 1", len(got))
	}
	if got[0].ExposureEffective.Effective {
		t.Fatalf("exposure effective = true, want false")
	}
	if got[0].Policy.Effective != AccessDeny {
		t.Fatalf("policy = %q, want deny", got[0].Policy.Effective)
	}
}

func TestExposureRequiresCompatibleProtocol(t *testing.T) {
	svc := New(fakeRuntimeTools{
		tools:  []runtimetools.Tool{{Name: "supabase.query", Source: runtimetools.SourceMCP, Enabled: true}},
		grants: []runtimetools.ResolvedTool{{Name: "supabase.query"}},
	}, fakePolicy{eff: toolpolicy.Effective{Setting: toolpolicy.SettingAllow, Reason: "Allowed by default"}})

	got, err := svc.ListEffectiveTools(context.Background(), Query{
		WorkspaceID:         uuid(1),
		RuntimeID:           uuid(2),
		RuntimeMode:         "cloud",
		RuntimeProvider:     "firtal-gateway",
		RuntimeCapabilities: []byte(`{"tool_protocols":["native_tool_loop"]}`),
		AgentID:             uuid(3),
		UserID:              uuid(4),
	})
	if err != nil {
		t.Fatalf("ListEffectiveTools: %v", err)
	}
	if got[0].Protocol.Effective != ProtocolUnsupported {
		t.Fatalf("protocol = %q, want unsupported", got[0].Protocol.Effective)
	}
	if got[0].ExposureEffective.Effective {
		t.Fatalf("exposure effective = true, want false")
	}
}

func TestPlatformToolCanUseLocalMCPProtocol(t *testing.T) {
	svc := New(fakeRuntimeTools{
		tools:  []runtimetools.Tool{{Name: "firtal_bq_query", Source: runtimetools.SourceCloud, Enabled: true}},
		grants: []runtimetools.ResolvedTool{{Name: "firtal_bq_query"}},
	}, fakePolicy{eff: toolpolicy.Effective{Setting: toolpolicy.SettingAllow, Reason: "Allowed by default"}})

	got, err := svc.ListEffectiveTools(context.Background(), Query{
		WorkspaceID:         uuid(1),
		RuntimeID:           uuid(2),
		RuntimeMode:         "local",
		RuntimeProvider:     "claude",
		RuntimeCapabilities: []byte(`{"tool_protocols":["mcp_stdio"],"supports_ask":false}`),
		AgentID:             uuid(3),
		UserID:              uuid(4),
	})
	if err != nil {
		t.Fatalf("ListEffectiveTools: %v", err)
	}
	if got[0].Protocol.Effective != ProtocolSupported {
		t.Fatalf("protocol = %q, want supported", got[0].Protocol.Effective)
	}
	if got[0].Protocol.SelectedProtocol != "mcp_stdio" {
		t.Fatalf("selected protocol = %q, want mcp_stdio", got[0].Protocol.SelectedProtocol)
	}
	if !got[0].ExposureEffective.Effective {
		t.Fatalf("exposure effective = false, want true: %s", got[0].ExposureEffective.Reason)
	}
}

func TestAskFallsBackToDenyWhenRuntimeCannotWait(t *testing.T) {
	svc := New(fakeRuntimeTools{
		tools:  []runtimetools.Tool{{Name: "firtal_bq_query", Source: runtimetools.SourceCloud, Enabled: true}},
		grants: []runtimetools.ResolvedTool{{Name: "firtal_bq_query"}},
	}, fakePolicy{eff: toolpolicy.Effective{Setting: toolpolicy.SettingAsk, Reason: "Ask required by policy"}})

	got, err := svc.ListEffectiveTools(context.Background(), Query{
		WorkspaceID:         uuid(1),
		RuntimeID:           uuid(2),
		RuntimeMode:         "local",
		RuntimeProvider:     "claude",
		RuntimeCapabilities: []byte(`{"tool_protocols":["mcp_stdio"],"supports_ask":false}`),
		AgentID:             uuid(3),
		UserID:              uuid(4),
	})
	if err != nil {
		t.Fatalf("ListEffectiveTools: %v", err)
	}
	if got[0].ExposureEffective.Effective {
		t.Fatalf("exposure effective = true, want false")
	}
	if got[0].ExposureEffective.Reason != "policy requires Ask, but runtime does not support approval waiting" {
		t.Fatalf("reason = %q", got[0].ExposureEffective.Reason)
	}
}

func TestExplicitChatOnlyCapabilityBeatsRuntimeModeFallback(t *testing.T) {
	svc := New(fakeRuntimeTools{
		tools:  []runtimetools.Tool{{Name: "firtal_bq_query", Source: runtimetools.SourceCloud, Enabled: true}},
		grants: []runtimetools.ResolvedTool{{Name: "firtal_bq_query"}},
	}, fakePolicy{eff: toolpolicy.Effective{Setting: toolpolicy.SettingAllow, Reason: "Allowed by default"}})

	got, err := svc.ListEffectiveTools(context.Background(), Query{
		WorkspaceID:         uuid(1),
		RuntimeID:           uuid(2),
		RuntimeMode:         "local",
		RuntimeProvider:     "claude",
		RuntimeCapabilities: []byte(`{"tool_protocols":["chat_only"],"supports_ask":false}`),
		AgentID:             uuid(3),
		UserID:              uuid(4),
	})
	if err != nil {
		t.Fatalf("ListEffectiveTools: %v", err)
	}
	if got[0].Protocol.Effective != ProtocolUnsupported {
		t.Fatalf("protocol = %q, want unsupported", got[0].Protocol.Effective)
	}
	if got[0].ExposureEffective.Effective {
		t.Fatalf("exposure effective = true, want false")
	}
}

func TestCredentialBackedToolIsNotExposedByReadOnlyPreview(t *testing.T) {
	svc := New(fakeRuntimeTools{
		tools:  []runtimetools.Tool{{Name: "credential_list", Source: runtimetools.SourceCloud, Enabled: true}},
		grants: []runtimetools.ResolvedTool{{Name: "credential_list"}},
	}, fakePolicy{eff: toolpolicy.Effective{Setting: toolpolicy.SettingAllow, Reason: "Allowed by default"}})

	got, err := svc.ListEffectiveTools(context.Background(), Query{
		WorkspaceID:     uuid(1),
		RuntimeID:       uuid(2),
		RuntimeMode:     "cloud",
		RuntimeProvider: "firtal-gateway",
		AgentID:         uuid(3),
		UserID:          uuid(4),
	})
	if err != nil {
		t.Fatalf("ListEffectiveTools: %v", err)
	}
	if got[0].Credential.Effective != CredentialRequired {
		t.Fatalf("credential = %q, want required", got[0].Credential.Effective)
	}
	if got[0].Descriptor.RecommendedDefaultPolicy != AccessDeny {
		t.Fatalf("recommended default = %q, want deny", got[0].Descriptor.RecommendedDefaultPolicy)
	}
	if got[0].ExposureEffective.Effective {
		t.Fatalf("exposure effective = true, want false")
	}
}

type fakeRuntimeTools struct {
	tools  []runtimetools.Tool
	grants []runtimetools.ResolvedTool
}

func (f fakeRuntimeTools) ListTools(context.Context, pgtype.UUID) ([]runtimetools.Tool, error) {
	return f.tools, nil
}

func (f fakeRuntimeTools) ResolveAccess(context.Context, pgtype.UUID, pgtype.UUID) ([]runtimetools.ResolvedTool, error) {
	return f.grants, nil
}

type fakePolicy struct {
	eff toolpolicy.Effective
}

func (f fakePolicy) Resolve(context.Context, toolpolicy.Query) (toolpolicy.Effective, error) {
	return f.eff, nil
}

func uuid(seed byte) pgtype.UUID {
	var id pgtype.UUID
	id.Valid = true
	id.Bytes[15] = seed
	return id
}
