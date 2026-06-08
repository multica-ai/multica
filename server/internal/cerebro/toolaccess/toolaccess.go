package toolaccess

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/runtimetools"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
)

const (
	AccessAllow   = "allow"
	AccessAsk     = "ask"
	AccessDeny    = "deny"
	AccessUnknown = "unknown"

	CredentialNotRequired = "not_required"
	CredentialRequired    = "required"

	ProtocolSupported   = "supported"
	ProtocolUnsupported = "unsupported"
)

type RuntimeTools interface {
	ListTools(ctx context.Context, runtimeID pgtype.UUID) ([]runtimetools.Tool, error)
	ResolveAccess(ctx context.Context, agentID, userID pgtype.UUID) ([]runtimetools.ResolvedTool, error)
}

type PolicyResolver interface {
	Resolve(ctx context.Context, in toolpolicy.Query) (toolpolicy.Effective, error)
}

type Service struct {
	runtimeTools RuntimeTools
	policy       PolicyResolver
}

func New(runtimeTools RuntimeTools, policy PolicyResolver) *Service {
	return &Service{runtimeTools: runtimeTools, policy: policy}
}

type Query struct {
	WorkspaceID         pgtype.UUID
	RuntimeID           pgtype.UUID
	RuntimeMode         string
	RuntimeProvider     string
	RuntimeCapabilities []byte
	AgentID             pgtype.UUID
	UserID              pgtype.UUID
}

type EffectiveTool struct {
	Descriptor        Descriptor        `json:"descriptor"`
	Inventory         InventoryState    `json:"inventory"`
	Policy            PolicyState       `json:"policy"`
	RuntimeGrant      GrantState        `json:"runtime_grant"`
	Protocol          ProtocolState     `json:"protocol"`
	Credential        CredentialState   `json:"credential"`
	ExposureEffective ExposureEffective `json:"exposure_effective"`
	Layers            map[string]string `json:"layers,omitempty"`
	Debug             map[string]string `json:"debug,omitempty"`
}

type Descriptor struct {
	ToolKey                  string   `json:"tool_key"`
	DisplayName              string   `json:"display_name"`
	Description              string   `json:"description,omitempty"`
	Source                   string   `json:"source"`
	RiskClass                string   `json:"risk_class"`
	Protocols                []string `json:"protocols"`
	RecommendedDefaultPolicy string   `json:"recommended_default_policy"`
}

type InventoryState struct {
	RuntimeID     string `json:"runtime_id"`
	ToolName      string `json:"tool_name"`
	Source        string `json:"source"`
	MCPServerName string `json:"mcp_server_name,omitempty"`
	Enabled       bool   `json:"enabled"`
}

type PolicyState struct {
	Effective string `json:"effective"`
	Reason    string `json:"reason"`
	DecidedBy string `json:"decided_by,omitempty"`
	CappedBy  string `json:"capped_by,omitempty"`
}

type GrantState struct {
	Effective string `json:"effective"`
	Reason    string `json:"reason"`
}

type ProtocolState struct {
	Effective          string   `json:"effective"`
	RequiredProtocols  []string `json:"required_protocols"`
	RuntimeProtocols   []string `json:"runtime_protocols"`
	SelectedProtocol   string   `json:"selected_protocol,omitempty"`
	SupportsAsk        bool     `json:"supports_ask"`
	UnsupportedMessage string   `json:"unsupported_message,omitempty"`
}

type CredentialState struct {
	Effective string `json:"effective"`
	Reason    string `json:"reason"`
}

type ExposureEffective struct {
	Effective bool   `json:"effective"`
	Reason    string `json:"reason"`
}

func (s *Service) ListEffectiveTools(ctx context.Context, q Query) ([]EffectiveTool, error) {
	if s == nil || s.runtimeTools == nil || s.policy == nil {
		return nil, fmt.Errorf("toolaccess: service not configured")
	}
	tools, err := s.runtimeTools.ListTools(ctx, q.RuntimeID)
	if err != nil {
		return nil, err
	}

	grants := map[string]struct{}{}
	grantKnown := q.AgentID.Valid && q.UserID.Valid
	if grantKnown {
		resolved, err := s.runtimeTools.ResolveAccess(ctx, q.AgentID, q.UserID)
		if err != nil {
			return nil, err
		}
		for _, t := range resolved {
			grants[t.Name] = struct{}{}
		}
	}

	runtimeProtocols := RuntimeProtocols(q.RuntimeMode, q.RuntimeProvider, q.RuntimeCapabilities)
	out := make([]EffectiveTool, 0, len(tools))
	for _, t := range tools {
		desc := DescriptorForTool(t)
		policy, err := s.policy.Resolve(ctx, toolpolicy.Query{
			WorkspaceID: q.WorkspaceID,
			ToolKey:     desc.ToolKey,
			RuntimeID:   q.RuntimeID,
			AgentID:     q.AgentID,
			UserID:      q.UserID,
			Base:        toolpolicy.SettingAllow,
		})
		if err != nil {
			return nil, fmt.Errorf("resolve policy for %s: %w", desc.ToolKey, err)
		}

		grant := grantState(t, grantKnown, grants)
		protocol := protocolState(desc.Protocols, runtimeProtocols)
		credential := CredentialForDescriptor(desc)
		exposure := exposureEffective(t.Enabled, policy, grant, protocol, credential)

		out = append(out, EffectiveTool{
			Descriptor:        desc,
			Inventory:         inventoryState(t),
			Policy:            policyState(policy),
			RuntimeGrant:      grant,
			Protocol:          protocol,
			Credential:        credential,
			ExposureEffective: exposure,
			Layers:            layerMap(policy),
		})
	}
	return out, nil
}

func DescriptorForTool(t runtimetools.Tool) Descriptor {
	protocols := []string{"native_tool_loop", "mcp_stdio", "mcp_http_sse", "managed_http_tool_loop"}
	source := "platform"
	if t.Source == runtimetools.SourceMCP {
		source = "mcp"
		protocols = []string{"mcp_stdio", "mcp_http_sse"}
	}
	risk := RiskClass(t.Name, t.Source)
	return Descriptor{
		ToolKey:                  t.Name,
		DisplayName:              t.Name,
		Description:              t.Description,
		Source:                   source,
		RiskClass:                risk,
		Protocols:                protocols,
		RecommendedDefaultPolicy: RecommendedDefaultPolicy(risk),
	}
}

func RiskClass(toolName, source string) string {
	name := strings.ToLower(toolName)
	switch {
	case strings.Contains(name, "credential") || strings.Contains(name, "secret"):
		return "credential"
	case strings.Contains(name, "delete") || strings.Contains(name, "revoke") || strings.Contains(name, "rotate"):
		return "destructive"
	case strings.Contains(name, "write") || strings.Contains(name, "create") || strings.Contains(name, "update") || strings.Contains(name, "add_"):
		return "write"
	case strings.Contains(name, "web_fetch") || strings.Contains(name, "fetch") || strings.Contains(name, "registry"):
		return "network"
	case source == runtimetools.SourceMCP:
		return "runtime_native"
	default:
		return "read"
	}
}

func RecommendedDefaultPolicy(risk string) string {
	switch risk {
	case "credential", "destructive":
		return AccessDeny
	case "write", "network", "runtime_native":
		return AccessAsk
	default:
		return AccessAllow
	}
}

type runtimeCapabilities struct {
	ToolProtocols []string `json:"tool_protocols"`
	SupportsAsk   *bool    `json:"supports_ask"`
}

func RuntimeProtocols(runtimeMode, provider string, raw []byte) ProtocolState {
	protocols := []string{}
	supportsAsk := false
	if len(raw) > 0 {
		var caps runtimeCapabilities
		if err := json.Unmarshal(raw, &caps); err == nil {
			protocols = append(protocols, caps.ToolProtocols...)
			if caps.SupportsAsk != nil {
				supportsAsk = *caps.SupportsAsk
			}
		}
	}
	if len(protocols) == 0 {
		if runtimeMode == "cloud" || provider == "firtal-gateway" {
			protocols = append(protocols, "native_tool_loop")
			supportsAsk = true
		}
		if runtimeMode == "local" {
			protocols = append(protocols, "mcp_stdio")
		}
	}
	protocols = uniqueNonEmpty(protocols)
	if len(protocols) == 0 {
		protocols = []string{"chat_only"}
	}
	return ProtocolState{RuntimeProtocols: protocols, SupportsAsk: supportsAsk}
}

func protocolState(required []string, runtime ProtocolState) ProtocolState {
	state := ProtocolState{
		RequiredProtocols: required,
		RuntimeProtocols:  runtime.RuntimeProtocols,
		SupportsAsk:       runtime.SupportsAsk,
		Effective:         ProtocolUnsupported,
	}
	for _, req := range required {
		for _, have := range runtime.RuntimeProtocols {
			if req == have {
				state.Effective = ProtocolSupported
				state.SelectedProtocol = req
				return state
			}
		}
	}
	state.UnsupportedMessage = "runtime does not report a compatible tool protocol"
	return state
}

func CredentialForDescriptor(desc Descriptor) CredentialState {
	if desc.RiskClass == "credential" {
		return CredentialState{
			Effective: CredentialRequired,
			Reason:    "credential-backed tools require the credential policy gate at execution time",
		}
	}
	return CredentialState{Effective: CredentialNotRequired, Reason: "tool descriptor does not require a credential"}
}

func grantState(t runtimetools.Tool, known bool, grants map[string]struct{}) GrantState {
	if !known {
		return GrantState{Effective: AccessUnknown, Reason: "agent and member context were not both supplied"}
	}
	if _, ok := grants[t.Name]; ok {
		return GrantState{Effective: AccessAllow, Reason: "allowed by runtime tool grants for this agent/member context"}
	}
	return GrantState{Effective: AccessDeny, Reason: "not allowed by runtime tool grants for this agent/member context"}
}

func exposureEffective(enabled bool, policy toolpolicy.Effective, grant GrantState, protocol ProtocolState, credential CredentialState) ExposureEffective {
	if !enabled {
		return ExposureEffective{Effective: false, Reason: "disabled in runtime inventory"}
	}
	if policy.Setting == toolpolicy.SettingDeny {
		return ExposureEffective{Effective: false, Reason: policy.Reason}
	}
	if grant.Effective == AccessDeny {
		return ExposureEffective{Effective: false, Reason: grant.Reason}
	}
	if protocol.Effective != ProtocolSupported {
		return ExposureEffective{Effective: false, Reason: protocol.UnsupportedMessage}
	}
	if credential.Effective == CredentialRequired {
		return ExposureEffective{Effective: false, Reason: credential.Reason}
	}
	if policy.Setting == toolpolicy.SettingAsk && !protocol.SupportsAsk {
		return ExposureEffective{Effective: false, Reason: "policy requires Ask, but runtime does not support approval waiting"}
	}
	if grant.Effective == AccessUnknown {
		return ExposureEffective{Effective: false, Reason: grant.Reason}
	}
	if policy.Setting == toolpolicy.SettingAsk {
		return ExposureEffective{Effective: true, Reason: policy.Reason}
	}
	return ExposureEffective{Effective: true, Reason: "allowed by policy, runtime grants, protocol, and credential checks"}
}

func inventoryState(t runtimetools.Tool) InventoryState {
	return InventoryState{
		RuntimeID:     uuidString(t.RuntimeID),
		ToolName:      t.Name,
		Source:        t.Source,
		MCPServerName: t.MCPServerName,
		Enabled:       t.Enabled,
	}
}

func policyState(e toolpolicy.Effective) PolicyState {
	return PolicyState{
		Effective: string(e.Setting),
		Reason:    e.Reason,
		DecidedBy: string(e.DecidedBy),
		CappedBy:  string(e.CappedBy),
	}
}

func layerMap(e toolpolicy.Effective) map[string]string {
	out := map[string]string{}
	if e.DecidedBy != "" {
		out["decided_by"] = string(e.DecidedBy)
	}
	if e.CappedBy != "" {
		out["capped_by"] = string(e.CappedBy)
	}
	return out
}

func uniqueNonEmpty(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func uuidString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	const hex = "0123456789abcdef"
	out := make([]byte, 36)
	const dashes = "00000000-0000-0000-0000-000000000000"
	copy(out, dashes)
	idx := 0
	for i, b := range u.Bytes {
		if i == 4 || i == 6 || i == 8 || i == 10 {
			idx++
		}
		out[idx] = hex[b>>4]
		idx++
		out[idx] = hex[b&0x0f]
		idx++
	}
	return string(out)
}
