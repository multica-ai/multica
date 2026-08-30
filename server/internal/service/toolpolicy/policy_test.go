package toolpolicy

import (
	"context"
	"errors"
	"testing"
)

func TestReplaceRequiresHumanOwnerAdminAndExpectedRevision(t *testing.T) {
	rules := []Rule{{
		TransportKind: "managed_mcp",
		ServerKey:     "linear",
		ToolName:      "list_issues",
		SchemaDigest:  "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Effect:        "allow",
	}}

	for _, tc := range []struct {
		name  string
		actor Actor
		want  error
	}{
		{name: "owner", actor: Actor{Kind: ActorHuman, WorkspaceRole: "owner"}},
		{name: "admin", actor: Actor{Kind: ActorHuman, WorkspaceRole: "admin"}},
		{name: "member", actor: Actor{Kind: ActorHuman, WorkspaceRole: "member"}, want: ErrForbidden},
		{name: "agent", actor: Actor{Kind: ActorAgent, WorkspaceRole: "owner"}, want: ErrForbidden},
		{name: "task", actor: Actor{Kind: ActorTask, WorkspaceRole: "owner"}, want: ErrForbidden},
		{name: "daemon", actor: Actor{Kind: ActorDaemon, WorkspaceRole: "owner"}, want: ErrForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := NewService(&memoryRepository{revision: 3})
			_, err := service.Replace(context.Background(), Replacement{
				Actor:            tc.actor,
				ExpectedRevision: 3,
				Rules:            rules,
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("Replace error = %v, want %v", err, tc.want)
			}
		})
	}

	service := NewService(&memoryRepository{revision: 3})
	_, err := service.Replace(context.Background(), Replacement{
		Actor:            Actor{Kind: ActorHuman, WorkspaceRole: "owner"},
		ExpectedRevision: 2,
		Rules:            rules,
	})
	if !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale Replace error = %v, want %v", err, ErrRevisionConflict)
	}
}

func TestGetAllowsOnlyScopedAgentActor(t *testing.T) {
	service := NewService(&memoryRepository{revision: 1})
	request := ReadRequest{
		AgentID: "agent-a",
		Actor:   Actor{Kind: ActorTask, AgentID: "agent-a"},
	}
	if _, err := service.Get(context.Background(), request); err != nil {
		t.Fatalf("owned-agent Get error = %v", err)
	}

	request.Actor.AgentID = "agent-b"
	if _, err := service.Get(context.Background(), request); !errors.Is(err, ErrForbidden) {
		t.Fatalf("cross-agent Get error = %v, want %v", err, ErrForbidden)
	}

	request.Actor = Actor{Kind: ActorDaemon, AgentID: "agent-a"}
	if _, err := service.Get(context.Background(), request); !errors.Is(err, ErrForbidden) {
		t.Fatalf("daemon Get error = %v, want %v", err, ErrForbidden)
	}
}

func TestReplaceCanonicalizesExactRulesAndDigest(t *testing.T) {
	repo := &captureRepository{}
	service := NewService(repo)
	rules := []Rule{
		{TransportKind: " managed_mcp ", ServerKey: " zeta ", ToolName: " second ", SchemaDigest: " SHA256:BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB ", Effect: " REQUIRE_APPROVAL "},
		{TransportKind: "managed_mcp", ServerKey: "alpha", ToolName: "first", SchemaDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Effect: "allow"},
	}
	request := Replacement{Actor: Actor{Kind: ActorHuman, WorkspaceRole: "owner"}, Rules: rules}
	if _, err := service.Replace(context.Background(), request); err != nil {
		t.Fatalf("Replace error = %v", err)
	}
	first := repo.last
	if first.Rules[0].ServerKey != "alpha" || first.Rules[1].ServerKey != "zeta" {
		t.Fatalf("canonical rule order = %+v", first.Rules)
	}

	repo.last = CanonicalReplacement{}
	request.Rules[0], request.Rules[1] = request.Rules[1], request.Rules[0]
	if _, err := service.Replace(context.Background(), request); err != nil {
		t.Fatalf("reordered Replace error = %v", err)
	}
	if repo.last.PolicyDigest != first.PolicyDigest || string(repo.last.RuleIdentities) != string(first.RuleIdentities) {
		t.Fatalf("canonical output changed with input order: first=%s/%s second=%s/%s", first.PolicyDigest, first.RuleIdentities, repo.last.PolicyDigest, repo.last.RuleIdentities)
	}
}

func TestReplaceUsesTheFullExactIdentityIncludingSchemaDigest(t *testing.T) {
	service := NewService(&captureRepository{})
	base := Rule{TransportKind: "managed_mcp", ServerKey: "linear", ToolName: "list_issues", SchemaDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Effect: "allow"}
	distinctDigest := base
	distinctDigest.SchemaDigest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if _, err := service.Replace(context.Background(), Replacement{
		Actor: Actor{Kind: ActorHuman, WorkspaceRole: "owner"},
		Rules: []Rule{base, distinctDigest},
	}); err != nil {
		t.Fatalf("Replace rejected distinct exact identities: %v", err)
	}

	for _, rules := range [][]Rule{
		{base, base},
		{{TransportKind: "managed_mcp", ServerKey: "*", ToolName: "list_issues", SchemaDigest: base.SchemaDigest, Effect: "allow"}},
	} {
		_, err := service.Replace(context.Background(), Replacement{Actor: Actor{Kind: ActorHuman, WorkspaceRole: "owner"}, Rules: rules})
		if !errors.Is(err, ErrInvalidPolicy) {
			t.Fatalf("Replace error = %v, want %v for %+v", err, ErrInvalidPolicy, rules)
		}
	}
}

type memoryRepository struct {
	revision int64
}

type captureRepository struct {
	last CanonicalReplacement
}

func (r *captureRepository) Get(context.Context, ReadRequest) (EffectivePolicy, error) {
	return EffectivePolicy{}, nil
}

func (r *captureRepository) Replace(_ context.Context, replacement CanonicalReplacement) (EffectivePolicy, error) {
	r.last = replacement
	return EffectivePolicy{Configured: true, Revision: replacement.NextRevision, Rules: replacement.Rules}, nil
}

func (r *memoryRepository) Get(_ context.Context, _ ReadRequest) (EffectivePolicy, error) {
	return EffectivePolicy{Configured: r.revision > 0, Revision: r.revision, Rules: []Rule{}}, nil
}

func (r *memoryRepository) Replace(_ context.Context, replacement CanonicalReplacement) (EffectivePolicy, error) {
	if replacement.ExpectedRevision != r.revision {
		return EffectivePolicy{}, ErrRevisionConflict
	}
	r.revision++
	return EffectivePolicy{Configured: true, Revision: r.revision, Rules: replacement.Rules}, nil
}
