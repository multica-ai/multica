// Package accessdiagnostics builds one read-only operator contract for
// provider probes, MCP discovery, live Permissions decisions, and the frozen
// Task Mandate. It never participates in authorization.
package accessdiagnostics

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

type State string

const (
	StateSuccess     State = "success"
	StateInfo        State = "info"
	StatePartial     State = "partial"
	StateStale       State = "stale"
	StateEmpty       State = "empty"
	StateDenied      State = "denied"
	StateUnavailable State = "unavailable"
	StateError       State = "error"
)

type Code string

const (
	CodeProviderProbe       Code = "provider_probe"
	CodeMCPDiscovery        Code = "mcp_discovery"
	CodeConnectionDiscovery Code = "connection_discovery"
	CodeTaskObservationOnly Code = "task_observation_only"
	CodeTaskPartial         Code = "task_partial"
	CodeTaskEmpty           Code = "task_empty"
	CodeTaskExpired         Code = "task_expired"
	CodeTaskDraft           Code = "task_not_finalized"
	CodeTaskMissing         Code = "task_mandate_missing"
	CodeObservedDenial      Code = "observed_denial"
)

// Diagnostic is the additive wire shape shared by REST, CLI, MCP and the app.
// SourcePolicy names the authority that produced the signal; RecoveryAction
// says what an operator can safely do next.
type Diagnostic struct {
	Code               Code   `json:"code"`
	State              State  `json:"state"`
	Title              string `json:"title"`
	Message            string `json:"message"`
	AffectedCapability string `json:"affected_capability,omitempty"`
	SourcePolicy       string `json:"source_policy"`
	RecoveryAction     string `json:"recovery_action"`
	Version            string `json:"version,omitempty"`
	Count              int    `json:"count,omitempty"`
	ObservedAt         string `json:"observed_at,omitempty"`
}

type RuntimeToolEvidence struct {
	Name          string
	Source        string
	MCPServerName string
	LastScannedAt time.Time
}

type RuntimeInput struct {
	RuntimeID          string
	Provider           string
	Status             string
	Capabilities       map[string]any
	ProviderObservedAt time.Time
	Tools              []RuntimeToolEvidence
	Now                time.Time
	StaleAfter         time.Duration
}

type RuntimeReport struct {
	RuntimeID   string       `json:"runtime_id"`
	Provider    string       `json:"provider"`
	Status      string       `json:"status"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

func BuildRuntimeDiagnostics(in RuntimeInput) RuntimeReport {
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	staleAfter := in.StaleAfter
	if staleAfter <= 0 {
		staleAfter = 30 * time.Minute
	}
	provider := strings.TrimSpace(in.Provider)
	discoveryMethod, _ := in.Capabilities["discovery_method"].(string)
	providerTools := stringSlice(in.Capabilities["tools"])
	providerDiagnostic := Diagnostic{
		Code:               CodeProviderProbe,
		Title:              "Provider capability probe",
		AffectedCapability: "provider:" + provider,
		SourcePolicy:       "Provider probe",
		Version:            Version(providerTools),
		Count:              len(providerTools),
	}
	if !in.ProviderObservedAt.IsZero() {
		providerDiagnostic.ObservedAt = in.ProviderObservedAt.UTC().Format(time.RFC3339Nano)
	}
	switch {
	case !strings.EqualFold(in.Status, "online"):
		providerDiagnostic.State = StateUnavailable
		providerDiagnostic.Message = "The runtime is offline, so its provider capability probe cannot be trusted as current."
		providerDiagnostic.RecoveryAction = "Bring the Runtime online, then run Scan now."
	case in.ProviderObservedAt.IsZero():
		providerDiagnostic.State = StateUnavailable
		providerDiagnostic.Message = "The provider inventory has no observation timestamp, so it cannot be trusted as current."
		providerDiagnostic.RecoveryAction = "Bring the Runtime online and run Scan now to record a current provider observation."
	case now.Sub(in.ProviderObservedAt) > staleAfter:
		providerDiagnostic.State = StateStale
		providerDiagnostic.Message = fmt.Sprintf("The provider capability observation is older than %s.", staleAfter.Round(time.Minute))
		providerDiagnostic.RecoveryAction = "Run Scan now and investigate why the Runtime has not refreshed its provider inventory."
	case discoveryMethod == "probed" && len(providerTools) > 0:
		providerDiagnostic.State = StateSuccess
		providerDiagnostic.Message = fmt.Sprintf("The provider probe measured %d callable capabilities.", len(providerTools))
		providerDiagnostic.RecoveryAction = "Run Scan now after changing the provider or its installed tools."
	case discoveryMethod == "static" && len(providerTools) > 0:
		providerDiagnostic.State = StatePartial
		providerDiagnostic.Message = fmt.Sprintf("The Runtime is using a curated inventory of %d capabilities instead of a live provider probe.", len(providerTools))
		providerDiagnostic.RecoveryAction = "Use Scan now when this provider gains a live probe; treat new provider tools as unverified until then."
	default:
		providerDiagnostic.State = StateUnavailable
		providerDiagnostic.Message = "No measured provider capability inventory is available for this Runtime."
		providerDiagnostic.RecoveryAction = "Check the provider installation and Runtime logs, then run Scan now."
	}

	configuredServers := cleanNames(stringSlice(in.Capabilities["mcp_servers"]))
	mcpNames := make([]string, 0, len(in.Tools))
	type serverEvidence struct {
		count            int
		newest           time.Time
		missingTimestamp bool
	}
	serverEvidenceByName := make(map[string]serverEvidence, len(configuredServers))
	for _, tool := range in.Tools {
		if tool.MCPServerName == "" && !strings.EqualFold(tool.Source, "mcp") {
			continue
		}
		serverName := strings.TrimSpace(tool.MCPServerName)
		mcpNames = append(mcpNames, serverName+":"+strings.TrimSpace(tool.Name))
		evidence := serverEvidenceByName[serverName]
		evidence.count++
		if tool.LastScannedAt.IsZero() {
			evidence.missingTimestamp = true
		} else if tool.LastScannedAt.After(evidence.newest) {
			evidence.newest = tool.LastScannedAt
		}
		serverEvidenceByName[serverName] = evidence
	}
	missingServers := make([]string, 0)
	missingTimestamps := make([]string, 0)
	staleServers := make([]string, 0)
	var oldestConfiguredObservation time.Time
	for _, serverName := range configuredServers {
		evidence, found := serverEvidenceByName[serverName]
		if !found || evidence.count == 0 {
			missingServers = append(missingServers, serverName)
			continue
		}
		if evidence.missingTimestamp || evidence.newest.IsZero() {
			missingTimestamps = append(missingTimestamps, serverName)
			continue
		}
		if now.Sub(evidence.newest) > staleAfter {
			staleServers = append(staleServers, serverName)
		}
		if oldestConfiguredObservation.IsZero() || evidence.newest.Before(oldestConfiguredObservation) {
			oldestConfiguredObservation = evidence.newest
		}
	}
	mcpDiagnostic := Diagnostic{
		Code:               CodeMCPDiscovery,
		Title:              "MCP discovery",
		AffectedCapability: "mcp:*",
		SourcePolicy:       "MCP tools/list",
		Version:            Version(mcpNames),
		Count:              len(mcpNames),
	}
	if !oldestConfiguredObservation.IsZero() {
		mcpDiagnostic.ObservedAt = oldestConfiguredObservation.UTC().Format(time.RFC3339Nano)
	}
	switch {
	case len(configuredServers) == 0 && len(mcpNames) == 0:
		mcpDiagnostic.State = StateEmpty
		mcpDiagnostic.Message = "No MCP servers are configured for this Runtime."
		mcpDiagnostic.RecoveryAction = "Add an enabled Connection only when this Runtime should receive MCP capabilities."
	case len(configuredServers) == 0:
		mcpDiagnostic.State = StatePartial
		mcpDiagnostic.Message = "MCP capability evidence exists, but no matching MCP server is configured for this Runtime."
		mcpDiagnostic.RecoveryAction = "Run Scan now and remove legacy discovery rows or restore the matching Connection configuration."
	case len(mcpNames) == 0:
		mcpDiagnostic.State = StateUnavailable
		mcpDiagnostic.Message = "MCP servers are configured, but tools/list has not produced a capability inventory."
		mcpDiagnostic.RecoveryAction = "Check Connection availability and credentials, then run Scan now."
	case len(missingServers) > 0 || len(missingTimestamps) > 0:
		mcpDiagnostic.State = StatePartial
		mcpDiagnostic.Message = fmt.Sprintf("MCP discovery is incomplete: %d configured servers have no inventory and %d have inventory without a scan timestamp.", len(missingServers), len(missingTimestamps))
		mcpDiagnostic.RecoveryAction = "Check every configured Connection, then run Scan now until each server reports timestamped tools/list evidence."
	case len(staleServers) > 0:
		mcpDiagnostic.State = StateStale
		mcpDiagnostic.Message = fmt.Sprintf("%d configured MCP servers have discovery evidence older than %s.", len(staleServers), staleAfter.Round(time.Minute))
		mcpDiagnostic.RecoveryAction = "Run Scan now and investigate every Connection that does not refresh."
	default:
		mcpDiagnostic.State = StateSuccess
		mcpDiagnostic.Message = fmt.Sprintf("MCP tools/list discovered %d callable capabilities.", len(mcpNames))
		mcpDiagnostic.RecoveryAction = "Run Scan now after changing an MCP server or Connection."
	}

	return RuntimeReport{
		RuntimeID:   in.RuntimeID,
		Provider:    provider,
		Status:      in.Status,
		Diagnostics: []Diagnostic{providerDiagnostic, mcpDiagnostic},
	}
}

type ConnectionInput struct {
	ConnectionType string
	Reachable      bool
	ToolNames      []string
	EndpointNames  []string
	Error          string
	Now            time.Time
}

func BuildConnectionDiagnostics(in ConnectionInput) []Diagnostic {
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	names := in.ToolNames
	title := "MCP discovery"
	source := "MCP tools/list"
	affected := "mcp:*"
	if in.ConnectionType != "mcp_http" {
		names = in.EndpointNames
		title = "API discovery"
		source = "OpenAPI discovery"
		affected = "connection:endpoints"
	}
	diagnostic := Diagnostic{
		Code:               CodeConnectionDiscovery,
		Title:              title,
		AffectedCapability: affected,
		SourcePolicy:       source,
		Version:            Version(names),
		Count:              len(names),
		ObservedAt:         now.UTC().Format(time.RFC3339Nano),
	}
	switch {
	case !in.Reachable:
		diagnostic.State = StateUnavailable
		diagnostic.Message = "The Connection could not be reached, so no capability inventory was accepted."
		diagnostic.RecoveryAction = "Check the URL, credentials, network route and whether the Connection is enabled, then test again."
	case strings.TrimSpace(in.Error) != "":
		diagnostic.State = StateError
		diagnostic.Message = strings.TrimSpace(in.Error)
		diagnostic.RecoveryAction = "Resolve the reported discovery error, then test again before relying on this inventory."
	case len(names) == 0:
		diagnostic.State = StateEmpty
		diagnostic.Message = "The Connection was reachable but returned no discoverable capabilities."
		diagnostic.RecoveryAction = "Confirm the identity can list capabilities and that the server publishes tools/list or an OpenAPI contract."
	default:
		diagnostic.State = StateSuccess
		diagnostic.Message = fmt.Sprintf("Discovery returned %d capabilities.", len(names))
		diagnostic.RecoveryAction = "Retest after changing the Connection identity or remote contract."
	}
	return []Diagnostic{diagnostic}
}

type DecisionEvidence struct {
	ObservedToolName      string
	CanonicalCapabilityID string
	Decision              string
	PolicyDecision        string
	LegacyPath            string
	Reason                string
	CreatedAt             time.Time
}

type TaskInput struct {
	EnforcementEnabled bool
	Status             string
	LifecycleState     string
	OfferedCount       int
	AuthorizedCount    int
	VerdictAllowed     bool
	VerdictCode        string
	VerdictMessage     string
	RecoveryAction     string
	Ledger             []DecisionEvidence
}

func BuildTaskDiagnostics(in TaskInput) []Diagnostic {
	out := make([]Diagnostic, 0, len(in.Ledger)+4)
	if !in.EnforcementEnabled {
		out = append(out, Diagnostic{
			Code: CodeTaskObservationOnly, State: StateInfo, Title: "Observation only",
			Message:            "Task Mandate enforcement is off. The snapshot is recorded for comparison but cannot reject calls.",
			AffectedCapability: "task:*", SourcePolicy: "Task Mandate",
			RecoveryAction: "Keep the rollout off until observation shows clean provider, discovery and policy parity.",
		})
	}
	if in.Status == "expired" {
		out = append(out, Diagnostic{
			Code: CodeTaskExpired, State: StateStale, Title: "Task access expired",
			Message:            "This historical Task Mandate is outside its active window.",
			AffectedCapability: "task:*", SourcePolicy: "Task Mandate",
			RecoveryAction: "Start a new task to receive a current Task Mandate.",
		})
	}
	if in.LifecycleState != "" && in.LifecycleState != "finalized" && in.LifecycleState != "legacy" {
		out = append(out, Diagnostic{
			Code: CodeTaskDraft, State: StateUnavailable, Title: "Task access not finalized",
			Message:            "The claim did not finalize an immutable capability ceiling.",
			AffectedCapability: "task:*", SourcePolicy: "Task Mandate",
			RecoveryAction: "Retry the task claim and investigate claim finalization before enabling enforcement.",
		})
	}
	if in.OfferedCount > in.AuthorizedCount {
		removed := in.OfferedCount - in.AuthorizedCount
		out = append(out, Diagnostic{
			Code: CodeTaskPartial, State: StatePartial, Title: "Partial task access",
			Message:            fmt.Sprintf("%d offered capabilities were removed before the Task Mandate was finalized.", removed),
			AffectedCapability: fmt.Sprintf("%d capabilities", removed), SourcePolicy: "Task Mandate",
			RecoveryAction: "Review Settings → Permissions and provider discovery, then start a new task after changing access.",
		})
	}
	if in.AuthorizedCount == 0 {
		out = append(out, Diagnostic{
			Code: CodeTaskEmpty, State: StateEmpty, Title: "No task capabilities",
			Message:            "No capabilities were frozen for this run.",
			AffectedCapability: "task:*", SourcePolicy: "Task Mandate",
			RecoveryAction: "Check the Runtime provider probe, MCP discovery and Settings → Permissions before retrying the task.",
		})
	}
	if in.EnforcementEnabled && !in.VerdictAllowed && in.VerdictCode != "" {
		out = append(out, Diagnostic{
			Code: Code(in.VerdictCode), State: StateDenied, Title: "Task Mandate denied access",
			Message: in.VerdictMessage, AffectedCapability: "task:*", SourcePolicy: "Task Mandate",
			RecoveryAction: in.RecoveryAction,
		})
	}
	for _, evidence := range in.Ledger {
		if evidence.Decision != "deny" {
			continue
		}
		lowerReason := strings.ToLower(strings.TrimSpace(evidence.Reason))
		source := "Settings → Permissions"
		recovery := "Review the capability in Settings → Permissions and start a new task after changing access."
		switch {
		case strings.Contains(lowerReason, "task mandate"):
			source = "Task Mandate"
			if strings.Contains(lowerReason, "task_generation_stale") {
				recovery = "Retry the task claim so it can finalize against the current claim generation."
			} else {
				recovery = "Review the frozen Task Mandate and start a new task after changing newly allowed access."
			}
		case strings.Contains(lowerReason, "absent from the live runtime surface"), strings.Contains(lowerReason, "did not resolve to a canonical capability"):
			source = "Runtime availability"
			recovery = "Refresh provider and MCP discovery, then retry after the capability appears on the live Runtime surface."
		case evidence.PolicyDecision == "ask":
			recovery = "Approve the pending request or change the capability decision, then retry the call."
		case evidence.PolicyDecision == "error":
			source = "Policy Decision Service"
			recovery = "Retry the policy lookup and investigate the Policy Decision Service before enabling enforcement."
		}
		capability := strings.TrimSpace(evidence.CanonicalCapabilityID)
		if capability == "" {
			capability = strings.TrimSpace(evidence.ObservedToolName)
		}
		diagnostic := Diagnostic{
			Code: CodeObservedDenial, State: StateDenied, Title: "Observed capability denial",
			Message: strings.TrimSpace(evidence.Reason), AffectedCapability: capability,
			SourcePolicy: source, RecoveryAction: recovery,
		}
		if !evidence.CreatedAt.IsZero() {
			diagnostic.ObservedAt = evidence.CreatedAt.UTC().Format(time.RFC3339Nano)
		}
		out = append(out, diagnostic)
	}
	return out
}

// Version returns a stable content version for a capability inventory.
func Version(names []string) string {
	clean := make([]string, 0, len(names))
	for _, name := range names {
		if name = strings.TrimSpace(name); name != "" {
			clean = append(clean, name)
		}
	}
	if len(clean) == 0 {
		return ""
	}
	sort.Strings(clean)
	hash := sha256.Sum256([]byte(strings.Join(clean, "\n")))
	return "sha256:" + hex.EncodeToString(hash[:8])
}

func stringSlice(value any) []string {
	switch values := value.(type) {
	case []string:
		return append([]string(nil), values...)
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			if text, ok := value.(string); ok {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func cleanNames(names []string) []string {
	out := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}
