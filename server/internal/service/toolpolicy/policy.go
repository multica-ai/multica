package toolpolicy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var (
	ErrForbidden        = errors.New("tool policy operation forbidden")
	ErrRevisionConflict = errors.New("tool policy revision conflict")
	ErrInvalidPolicy    = errors.New("invalid tool policy")
	ErrNotFound         = errors.New("tool policy agent not found")
	ErrStoredPolicy     = errors.New("stored tool policy failed validation")
)

type ActorKind string

const (
	ActorHuman  ActorKind = "human"
	ActorAgent  ActorKind = "agent"
	ActorTask   ActorKind = "task"
	ActorDaemon ActorKind = "daemon"
)

type Actor struct {
	Kind          ActorKind
	UserID        string
	AgentID       string
	WorkspaceRole string
}

type Rule struct {
	TransportKind string `json:"transport_kind"`
	ServerKey     string `json:"server_key"`
	ToolName      string `json:"tool_name"`
	SchemaDigest  string `json:"schema_digest"`
	Effect        string `json:"effect"`
}

type Replacement struct {
	WorkspaceID      string
	AgentID          string
	Actor            Actor
	ExpectedRevision int64
	Rules            []Rule
}

type ReadRequest struct {
	WorkspaceID string
	AgentID     string
	Actor       Actor
}

type CanonicalReplacement struct {
	WorkspaceID      string
	AgentID          string
	Actor            Actor
	ExpectedRevision int64
	NextRevision     int64
	Rules            []Rule
	RuleIdentities   []byte
	PolicyDigest     string
}

type EffectivePolicy struct {
	Configured    bool   `json:"configured"`
	Revision      int64  `json:"revision,omitempty"`
	Status        string `json:"status,omitempty"`
	PolicyDigest  string `json:"policy_digest,omitempty"`
	DefaultEffect string `json:"default_effect,omitempty"`
	Rules         []Rule `json:"rules"`
}

type Repository interface {
	Get(context.Context, ReadRequest) (EffectivePolicy, error)
	Replace(context.Context, CanonicalReplacement) (EffectivePolicy, error)
}

type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) Get(ctx context.Context, request ReadRequest) (EffectivePolicy, error) {
	switch request.Actor.Kind {
	case ActorHuman:
	case ActorAgent, ActorTask:
		if request.Actor.AgentID == "" || request.Actor.AgentID != request.AgentID {
			return EffectivePolicy{}, ErrForbidden
		}
	default:
		return EffectivePolicy{}, ErrForbidden
	}
	return s.repository.Get(ctx, request)
}

func (s *Service) Replace(ctx context.Context, replacement Replacement) (EffectivePolicy, error) {
	if replacement.Actor.Kind != ActorHuman || !isOperatorRole(replacement.Actor.WorkspaceRole) {
		return EffectivePolicy{}, ErrForbidden
	}
	if replacement.ExpectedRevision < 0 {
		return EffectivePolicy{}, fmt.Errorf("%w: expected_revision must not be negative", ErrInvalidPolicy)
	}
	rules, identities, digest, err := canonicalize(replacement.Rules)
	if err != nil {
		return EffectivePolicy{}, err
	}
	return s.repository.Replace(ctx, CanonicalReplacement{
		WorkspaceID:      replacement.WorkspaceID,
		AgentID:          replacement.AgentID,
		Actor:            replacement.Actor,
		ExpectedRevision: replacement.ExpectedRevision,
		NextRevision:     replacement.ExpectedRevision + 1,
		Rules:            rules,
		RuleIdentities:   identities,
		PolicyDigest:     digest,
	})
}

func isOperatorRole(role string) bool {
	return role == "owner" || role == "admin"
}

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
var identityPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,254}$`)

func canonicalize(input []Rule) ([]Rule, []byte, string, error) {
	rules := make([]Rule, len(input))
	seen := make(map[string]struct{}, len(input))
	for i, raw := range input {
		rule := Rule{
			TransportKind: strings.ToLower(strings.TrimSpace(raw.TransportKind)),
			ServerKey:     strings.TrimSpace(raw.ServerKey),
			ToolName:      strings.TrimSpace(raw.ToolName),
			SchemaDigest:  strings.ToLower(strings.TrimSpace(raw.SchemaDigest)),
			Effect:        strings.ToLower(strings.TrimSpace(raw.Effect)),
		}
		if err := validateRule(rule); err != nil {
			return nil, nil, "", err
		}
		identity := strings.Join([]string{rule.TransportKind, rule.ServerKey, rule.ToolName, rule.SchemaDigest}, "\x00")
		if _, exists := seen[identity]; exists {
			return nil, nil, "", fmt.Errorf("%w: duplicate exact tool identity", ErrInvalidPolicy)
		}
		seen[identity] = struct{}{}
		rules[i] = rule
	}

	sort.Slice(rules, func(i, j int) bool {
		left := rules[i]
		right := rules[j]
		if left.TransportKind != right.TransportKind {
			return left.TransportKind < right.TransportKind
		}
		if left.ServerKey != right.ServerKey {
			return left.ServerKey < right.ServerKey
		}
		if left.ToolName != right.ToolName {
			return left.ToolName < right.ToolName
		}
		return left.SchemaDigest < right.SchemaDigest
	})

	identities, err := json.Marshal(rules)
	if err != nil {
		return nil, nil, "", fmt.Errorf("%w: canonical rule encoding", ErrInvalidPolicy)
	}
	canonicalPolicy, err := json.Marshal(struct {
		DefaultEffect string          `json:"default_effect"`
		Rules         json.RawMessage `json:"rules"`
	}{DefaultEffect: "deny", Rules: identities})
	if err != nil {
		return nil, nil, "", fmt.Errorf("%w: canonical policy encoding", ErrInvalidPolicy)
	}
	sum := sha256.Sum256(canonicalPolicy)
	return rules, identities, "sha256:" + hex.EncodeToString(sum[:]), nil
}

func validateRule(rule Rule) error {
	if rule.TransportKind != "managed_mcp" && rule.TransportKind != "managed_native" {
		return fmt.Errorf("%w: unsupported transport_kind", ErrInvalidPolicy)
	}
	if !identityPattern.MatchString(rule.ServerKey) || !identityPattern.MatchString(rule.ToolName) {
		return fmt.Errorf("%w: tool identity must be exact metadata", ErrInvalidPolicy)
	}
	if containsSensitiveMarker(rule.ServerKey) || containsSensitiveMarker(rule.ToolName) {
		return fmt.Errorf("%w: sensitive marker in tool identity", ErrInvalidPolicy)
	}
	if !digestPattern.MatchString(rule.SchemaDigest) {
		return fmt.Errorf("%w: invalid schema_digest", ErrInvalidPolicy)
	}
	if rule.Effect != "allow" && rule.Effect != "require_approval" {
		return fmt.Errorf("%w: invalid effect", ErrInvalidPolicy)
	}
	return nil
}

func containsSensitiveMarker(value string) bool {
	lower := strings.ToLower(value)
	for _, marker := range []string{"canary", "bearer ", "password=", "token=", "api_key=", "-----begin", "sk-", "ghp_"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
