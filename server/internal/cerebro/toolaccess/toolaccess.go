package toolaccess

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/capabilityregistry"
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

type CapabilityLister interface {
	List(ctx context.Context, workspaceID pgtype.UUID, subject *capabilityregistry.Subject, keys []string) ([]capabilityregistry.View, error)
}

type PolicyResolver interface {
	Resolve(ctx context.Context, in toolpolicy.Query) (toolpolicy.Effective, error)
}

type Service struct {
	capabilities CapabilityLister
	policy       PolicyResolver
}

func New(capabilities CapabilityLister, policy PolicyResolver) *Service {
	return &Service{capabilities: capabilities, policy: policy}
}

type Query struct {
	WorkspaceID         pgtype.UUID
	RuntimeID           pgtype.UUID
	RuntimeMode         string
	RuntimeProvider     string
	RuntimeCapabilities []byte
	AgentID             pgtype.UUID
	UserID              pgtype.UUID
	// OnBehalfOfID is the delegated member (task initiator) the work is performed
	// for, resolved as the tighten-only on_behalf_of policy layer distinct from
	// UserID (the agent owner) (FIR-2441). Zero when there is no delegation.
	OnBehalfOfID pgtype.UUID
}

type EffectiveTool struct {
	Descriptor        Descriptor        `json:"descriptor"`
	Inventory         InventoryState    `json:"inventory"`
	Policy            PolicyState       `json:"policy"`
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
	if s == nil || s.capabilities == nil || s.policy == nil {
		return nil, fmt.Errorf("toolaccess: service not configured")
	}
	subject := capabilityregistry.Subject{Type: "runtime", ID: uuidString(q.RuntimeID)}
	tools, err := s.capabilities.List(ctx, q.WorkspaceID, &subject, nil)
	if err != nil {
		return nil, err
	}

	runtimeProtocols := RuntimeProtocols(q.RuntimeMode, q.RuntimeProvider, q.RuntimeCapabilities)
	out := make([]EffectiveTool, 0, len(tools))
	for _, t := range tools {
		desc := DescriptorForCapability(t)
		policy, err := s.policy.Resolve(ctx, toolpolicy.Query{
			WorkspaceID:  q.WorkspaceID,
			ToolKey:      desc.ToolKey,
			RuntimeID:    q.RuntimeID,
			AgentID:      q.AgentID,
			UserID:       q.UserID,
			OnBehalfOfID: q.OnBehalfOfID,
			Base:         toolpolicy.SettingAllow,
		})
		if err != nil {
			return nil, fmt.Errorf("resolve policy for %s: %w", desc.ToolKey, err)
		}

		protocol := protocolState(desc.Protocols, runtimeProtocols)
		credential := CredentialForDescriptor(desc)
		enabled := policy.Setting != toolpolicy.SettingDeny
		exposure := exposureEffective(enabled, policy, protocol, credential)

		out = append(out, EffectiveTool{
			Descriptor:        desc,
			Inventory:         inventoryState(t, q.RuntimeID, enabled),
			Policy:            policyState(policy),
			Protocol:          protocol,
			Credential:        credential,
			ExposureEffective: exposure,
			Layers:            layerMap(policy),
		})
	}
	return out, nil
}

func DescriptorForCapability(t capabilityregistry.View) Descriptor {
	protocols := []string{"native_tool_loop", "mcp_stdio", "mcp_http_sse", "managed_http_tool_loop"}
	source := "platform"
	if t.Source == "scan" {
		source = "mcp"
		protocols = []string{"mcp_stdio", "mcp_http_sse"}
	}
	risk := RiskClass(t.Key, source)
	return Descriptor{
		ToolKey:                  t.Key,
		DisplayName:              t.Title,
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
	case source == "mcp":
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

func exposureEffective(enabled bool, policy toolpolicy.Effective, protocol ProtocolState, credential CredentialState) ExposureEffective {
	if !enabled {
		return ExposureEffective{Effective: false, Reason: "disabled in runtime inventory"}
	}
	if policy.Setting == toolpolicy.SettingDeny {
		return ExposureEffective{Effective: false, Reason: policy.Reason}
	}
	if protocol.Effective != ProtocolSupported {
		return ExposureEffective{Effective: false, Reason: protocol.UnsupportedMessage}
	}
	if credential.Effective == CredentialRequired {
		return ExposureEffective{Effective: false, Reason: credential.Reason}
	}
	if policy.Setting == toolpolicy.SettingAsk {
		return ExposureEffective{Effective: true, Reason: policy.Reason}
	}
	return ExposureEffective{Effective: true, Reason: "allowed by policy, protocol, and credential checks"}
}

func inventoryState(t capabilityregistry.View, runtimeID pgtype.UUID, enabled bool) InventoryState {
	serverName := ""
	if value, ok := t.Metadata["server_name"].(string); ok {
		serverName = value
	}
	return InventoryState{
		RuntimeID:     uuidString(runtimeID),
		ToolName:      t.Key,
		Source:        t.Source,
		MCPServerName: serverName,
		Enabled:       enabled,
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
