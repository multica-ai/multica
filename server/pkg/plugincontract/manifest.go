// Package plugincontract defines the versioned public contract between Plugin
// publishers and the Multica host. It must remain independent from private
// handlers, services, and database implementations.
package plugincontract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
)

const (
	APIVersionV1 = "multica.plugin/v1"
	KindPlugin   = "Plugin"

	ContributionAgentSkillV1       = "agent.skill.v1"
	CapabilityAgentSkillContribute = "agent.skill.contribute"
	ContributionRemoteMCPV1        = "tool.remote-mcp.v1"
	CapabilityRemoteMCPConnect     = "tool.remote-mcp.connect"
	RemoteMCPAnyToolIntent         = "*"

	DaemonFeatureExecutionManifestV1 = "execution-manifest-v1"
	DaemonFeatureAgentSkillV1        = "agent-skill-v1"
	DaemonFeatureRemoteMCPV1         = "remote-mcp-v1"

	ManifestFilename = "multica.plugin.json"
	MaxManifestSize  = 1 << 20
)

var (
	pluginKeySegmentPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
	contributionKeyPattern  = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
	publisherPattern        = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$`)
	semverPattern           = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
)

type Manifest struct {
	APIVersion            string                `json:"api_version"`
	Kind                  string                `json:"kind"`
	Metadata              Metadata              `json:"metadata"`
	Compatibility         Compatibility         `json:"compatibility"`
	RequestedCapabilities []string              `json:"requested_capabilities"`
	Contributes           ManifestContributions `json:"contributes"`
}

func remoteMCPHostAllowed(host string, policies []string) bool {
	for _, policy := range policies {
		policy = strings.ToLower(strings.TrimSuffix(policy, "."))
		if host == policy {
			return true
		}
		if strings.HasPrefix(policy, "*.") {
			suffix := strings.TrimPrefix(policy, "*")
			if strings.HasSuffix(host, suffix) && host != strings.TrimPrefix(suffix, ".") {
				return true
			}
		}
	}
	return false
}

type Metadata struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Version     string `json:"version"`
	Publisher   string `json:"publisher"`
}

type Compatibility struct {
	HostAPI                string   `json:"host_api"`
	RequiredDaemonFeatures []string `json:"required_daemon_features"`
}

type ManifestContributions struct {
	AgentSkills []AgentSkillContribution `json:"agent_skills,omitempty"`
	RemoteMCP   []RemoteMCPContribution  `json:"remote_mcp,omitempty"`
}

type AgentSkillContribution struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// Entry is the canonical SKILL.md. Every other regular UTF-8 file below
	// skills/<key>/ is carried as an immutable companion file in the same
	// agent.skill.v1 bundle.
	Entry string `json:"entry"`
}

// RemoteMCPContribution is a declarative request to connect an installation to
// a remote MCP server. The package never contains an endpoint or credential:
// those are workspace-owned, revisioned installation configuration. ToolIntent
// is the maximum publisher-declared tool surface an administrator may approve.
type RemoteMCPContribution struct {
	Key                 string                  `json:"key"`
	Name                string                  `json:"name"`
	Description         string                  `json:"description"`
	Transport           string                  `json:"transport"`
	ProtocolVersions    []string                `json:"protocol_versions"`
	EndpointPolicy      RemoteMCPEndpointPolicy `json:"endpoint_policy"`
	Authentication      RemoteMCPAuthentication `json:"authentication,omitempty"`
	ToolIntent          []RemoteMCPToolIntent   `json:"tool_intent"`
	ConfigurationSchema json.RawMessage         `json:"configuration_schema,omitempty"`
}

type RemoteMCPEndpointPolicy struct {
	// DefaultEndpoint is the publisher-suggested hosted endpoint used by the
	// one-click Connect flow. It is configuration, never a grant: the host must
	// also be covered by AllowedHosts and the server still applies its public
	// HTTPS/SSRF checks before every discovery or execution request.
	DefaultEndpoint string   `json:"default_endpoint,omitempty"`
	AllowedHosts    []string `json:"allowed_hosts,omitempty"`
}

type RemoteMCPAuthentication struct {
	Preferred string   `json:"preferred,omitempty"`
	Supported []string `json:"supported,omitempty"`
}

type RemoteMCPToolIntent struct {
	// Name may be "*" for hosted MCP servers whose exact tool set is only
	// available after authentication. The administrator must still explicitly
	// approve discovered tool names, and execution pins each approved schema;
	// newly discovered tools never become available automatically. Wildcard
	// intent is conservatively classified as write until exact tools are known.
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Risk        string `json:"risk"`
}

// ParseManifest decodes a strict V1 manifest and returns the canonical bytes
// that releases hash and sign. Unknown fields fail instead of being ignored so
// a typo cannot silently weaken the publisher's intended contract.
func ParseManifest(raw []byte) (Manifest, []byte, error) {
	if len(raw) == 0 {
		return Manifest{}, nil, fmt.Errorf("plugin manifest is empty")
	}
	if len(raw) > MaxManifestSize {
		return Manifest{}, nil, fmt.Errorf("plugin manifest exceeds %d bytes", MaxManifestSize)
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()

	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, nil, fmt.Errorf("decode plugin manifest: %w", err)
	}
	if err := rejectTrailingJSON(decoder); err != nil {
		return Manifest{}, nil, err
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, nil, err
	}

	canonical, err := json.Marshal(manifest)
	if err != nil {
		return Manifest{}, nil, fmt.Errorf("canonicalize plugin manifest: %w", err)
	}
	return manifest, canonical, nil
}

func rejectTrailingJSON(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("plugin manifest contains trailing JSON")
		}
		return fmt.Errorf("decode trailing plugin manifest data: %w", err)
	}
	return nil
}

func (m Manifest) Validate() error {
	if m.APIVersion != APIVersionV1 {
		return fmt.Errorf("api_version must be %q", APIVersionV1)
	}
	if m.Kind != KindPlugin {
		return fmt.Errorf("kind must be %q", KindPlugin)
	}
	if err := validatePluginKey(m.Metadata.Key); err != nil {
		return err
	}
	if err := validateDisplayText("metadata.name", m.Metadata.Name, 160); err != nil {
		return err
	}
	if len(m.Metadata.Description) > 2000 {
		return fmt.Errorf("metadata.description exceeds 2000 bytes")
	}
	if !semverPattern.MatchString(m.Metadata.Version) {
		return fmt.Errorf("metadata.version must be semantic versioning, got %q", m.Metadata.Version)
	}
	if !publisherPattern.MatchString(m.Metadata.Publisher) {
		return fmt.Errorf("metadata.publisher is invalid")
	}
	if strings.TrimSpace(m.Compatibility.HostAPI) == "" || len(m.Compatibility.HostAPI) > 256 || strings.ContainsAny(m.Compatibility.HostAPI, "\r\n") {
		return fmt.Errorf("compatibility.host_api must be a non-empty single-line range")
	}

	capabilities, err := uniqueStrings("requested_capabilities", m.RequestedCapabilities)
	if err != nil {
		return err
	}
	wantedCapabilities := map[string]bool{}
	if len(m.Contributes.AgentSkills) > 0 {
		wantedCapabilities[CapabilityAgentSkillContribute] = true
	}
	if len(m.Contributes.RemoteMCP) > 0 {
		wantedCapabilities[CapabilityRemoteMCPConnect] = true
	}
	if len(wantedCapabilities) == 0 {
		return fmt.Errorf("contributes must contain at least one agent_skills or remote_mcp contribution")
	}
	if len(capabilities) != len(wantedCapabilities) {
		return fmt.Errorf("requested_capabilities must exactly match declared capabilities %q", declaredCapabilityNames(wantedCapabilities))
	}
	for capability := range capabilities {
		if !wantedCapabilities[capability] {
			return fmt.Errorf("requested_capabilities contains unsupported capability %q", capability)
		}
	}

	features, err := uniqueStrings("compatibility.required_daemon_features", m.Compatibility.RequiredDaemonFeatures)
	if err != nil {
		return err
	}
	requiredFeatures := []string{DaemonFeatureExecutionManifestV1}
	if len(m.Contributes.AgentSkills) > 0 {
		requiredFeatures = append(requiredFeatures, DaemonFeatureAgentSkillV1)
	}
	if len(m.Contributes.RemoteMCP) > 0 {
		requiredFeatures = append(requiredFeatures, DaemonFeatureRemoteMCPV1)
	}
	for _, required := range requiredFeatures {
		if !features[required] {
			return fmt.Errorf("compatibility.required_daemon_features must include %q", required)
		}
	}

	keys := make(map[string]bool, len(m.Contributes.AgentSkills)+len(m.Contributes.RemoteMCP))
	names := make(map[string]bool, len(m.Contributes.AgentSkills)+len(m.Contributes.RemoteMCP))
	for index, skill := range m.Contributes.AgentSkills {
		field := fmt.Sprintf("contributes.agent_skills[%d]", index)
		if !contributionKeyPattern.MatchString(skill.Key) {
			return fmt.Errorf("%s.key is invalid", field)
		}
		if strings.HasPrefix(skill.Key, "multica-") {
			return fmt.Errorf("%s.key uses the reserved multica- namespace", field)
		}
		if keys[skill.Key] {
			return fmt.Errorf("duplicate contribution key %q", skill.Key)
		}
		keys[skill.Key] = true
		if err := validateDisplayText(field+".name", skill.Name, 160); err != nil {
			return err
		}
		nameKey := strings.ToLower(skill.Name)
		if names[nameKey] {
			return fmt.Errorf("duplicate contribution name %q", skill.Name)
		}
		names[nameKey] = true
		if err := validateDisplayText(field+".description", skill.Description, 2000); err != nil {
			return err
		}
		wantEntry := "skills/" + skill.Key + "/SKILL.md"
		if skill.Entry != wantEntry {
			return fmt.Errorf("%s.entry must be %q", field, wantEntry)
		}
	}
	for index, remote := range m.Contributes.RemoteMCP {
		field := fmt.Sprintf("contributes.remote_mcp[%d]", index)
		if err := validateContributionIdentity(field, remote.Key, remote.Name, remote.Description, keys, names); err != nil {
			return err
		}
		if remote.Transport != "streamable-http" {
			return fmt.Errorf("%s.transport must be %q", field, "streamable-http")
		}
		versions, err := uniqueStrings(field+".protocol_versions", remote.ProtocolVersions)
		if err != nil {
			return err
		}
		if len(versions) == 0 {
			return fmt.Errorf("%s.protocol_versions must not be empty", field)
		}
		for version := range versions {
			if version != "2025-03-26" && version != "2024-11-05" {
				return fmt.Errorf("%s.protocol_versions contains unsupported version %q", field, version)
			}
		}
		allowedHosts, err := uniqueStrings(field+".endpoint_policy.allowed_hosts", remote.EndpointPolicy.AllowedHosts)
		if err != nil {
			return err
		}
		for host := range allowedHosts {
			if strings.ContainsAny(host, "/:@?#") || strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") || strings.ToLower(host) != host {
				return fmt.Errorf("%s.endpoint_policy.allowed_hosts contains invalid host %q", field, host)
			}
		}
		if remote.EndpointPolicy.DefaultEndpoint != "" {
			endpoint, err := url.Parse(remote.EndpointPolicy.DefaultEndpoint)
			if err != nil || endpoint.Scheme != "https" || endpoint.Hostname() == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
				return fmt.Errorf("%s.endpoint_policy.default_endpoint must be a plain HTTPS URL", field)
			}
			host := strings.ToLower(strings.TrimSuffix(endpoint.Hostname(), "."))
			if !remoteMCPHostAllowed(host, remote.EndpointPolicy.AllowedHosts) {
				return fmt.Errorf("%s.endpoint_policy.default_endpoint host must be covered by allowed_hosts", field)
			}
		}
		authModes, err := uniqueStrings(field+".authentication.supported", remote.Authentication.Supported)
		if err != nil {
			return err
		}
		for mode := range authModes {
			if mode != "none" && mode != "oauth" && mode != "bearer" && mode != "header" {
				return fmt.Errorf("%s.authentication.supported contains unsupported mode %q", field, mode)
			}
		}
		if remote.Authentication.Preferred != "" && !authModes[remote.Authentication.Preferred] {
			return fmt.Errorf("%s.authentication.preferred must appear in supported", field)
		}
		if len(remote.ToolIntent) == 0 {
			return fmt.Errorf("%s.tool_intent must not be empty", field)
		}
		toolNames := make(map[string]bool, len(remote.ToolIntent))
		for toolIndex, tool := range remote.ToolIntent {
			toolField := fmt.Sprintf("%s.tool_intent[%d]", field, toolIndex)
			if tool.Name != RemoteMCPAnyToolIntent && !contributionKeyPattern.MatchString(tool.Name) && !regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.:-]{0,127}$`).MatchString(tool.Name) {
				return fmt.Errorf("%s.name is invalid", toolField)
			}
			if toolNames[tool.Name] {
				return fmt.Errorf("%s contains duplicate tool name %q", field+".tool_intent", tool.Name)
			}
			toolNames[tool.Name] = true
			if tool.Description != "" {
				if err := validateDisplayText(toolField+".description", tool.Description, 2000); err != nil {
					return err
				}
			}
			if tool.Risk != "read" && tool.Risk != "write" {
				return fmt.Errorf("%s.risk must be read or write", toolField)
			}
			if tool.Name == RemoteMCPAnyToolIntent && tool.Risk != "write" {
				return fmt.Errorf("%s.risk must be write for wildcard tool intent", toolField)
			}
		}
		if len(remote.ConfigurationSchema) > 0 {
			var schema map[string]any
			if err := json.Unmarshal(remote.ConfigurationSchema, &schema); err != nil {
				return fmt.Errorf("%s.configuration_schema must be a JSON object: %w", field, err)
			}
			if schemaType, _ := schema["type"].(string); schemaType != "object" {
				return fmt.Errorf("%s.configuration_schema.type must be object", field)
			}
		}
	}

	return nil
}

func declaredCapabilityNames(capabilities map[string]bool) []string {
	names := make([]string, 0, len(capabilities))
	for _, capability := range []string{CapabilityAgentSkillContribute, CapabilityRemoteMCPConnect} {
		if capabilities[capability] {
			names = append(names, capability)
		}
	}
	return names
}

func validateContributionIdentity(field, key, name, description string, keys, names map[string]bool) error {
	if !contributionKeyPattern.MatchString(key) {
		return fmt.Errorf("%s.key is invalid", field)
	}
	if strings.HasPrefix(key, "multica-") {
		return fmt.Errorf("%s.key uses the reserved multica- namespace", field)
	}
	if keys[key] {
		return fmt.Errorf("duplicate contribution key %q", key)
	}
	keys[key] = true
	if err := validateDisplayText(field+".name", name, 160); err != nil {
		return err
	}
	nameKey := strings.ToLower(name)
	if names[nameKey] {
		return fmt.Errorf("duplicate contribution name %q", name)
	}
	names[nameKey] = true
	return validateDisplayText(field+".description", description, 2000)
}

func validatePluginKey(key string) error {
	if len(key) > 255 {
		return fmt.Errorf("metadata.key exceeds 255 bytes")
	}
	segments := strings.Split(key, ".")
	if len(segments) < 2 {
		return fmt.Errorf("metadata.key must use a reverse-DNS namespace")
	}
	for _, segment := range segments {
		if !pluginKeySegmentPattern.MatchString(segment) {
			return fmt.Errorf("metadata.key contains invalid segment %q", segment)
		}
	}
	return nil
}

func validateDisplayText(field, value string, maxBytes int) error {
	if value == "" || value != strings.TrimSpace(value) {
		return fmt.Errorf("%s must be non-empty without surrounding whitespace", field)
	}
	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("%s must be single-line", field)
	}
	if len(value) > maxBytes {
		return fmt.Errorf("%s exceeds %d bytes", field, maxBytes)
	}
	return nil
}

func uniqueStrings(field string, values []string) (map[string]bool, error) {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if value == "" || value != strings.TrimSpace(value) || strings.ContainsAny(value, "\r\n") {
			return nil, fmt.Errorf("%s contains an invalid value", field)
		}
		if seen[value] {
			return nil, fmt.Errorf("%s contains duplicate value %q", field, value)
		}
		seen[value] = true
	}
	return seen, nil
}
