package remotemcp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Tool is one remote MCP tool pinned by an administrator. SchemaDigest freezes
// the exact input schema that was approved so the daemon broker can reject a
// server that later swaps a tool's contract underneath a running task.
type Tool struct {
	Name         string          `json:"name"`
	Description  string          `json:"description,omitempty"`
	InputSchema  json.RawMessage `json:"input_schema"`
	SchemaDigest string          `json:"schema_digest"`
	Risk         string          `json:"risk,omitempty"`
}

// Connection is claim-time, task-scoped connection metadata. Credentials are
// intentionally absent from this wire type and are resolved just-in-time by the
// daemon broker.
type Connection struct {
	InstallationID       string          `json:"installation_id"`
	ContributionID       string          `json:"contribution_id"`
	ContributionKey      string          `json:"contribution_key"`
	ConfigID             string          `json:"config_id"`
	ConfigRevision       int64           `json:"config_revision"`
	Endpoint             string          `json:"endpoint"`
	PublicConfig         json.RawMessage `json:"public_config,omitempty"`
	Transport            string          `json:"transport"`
	ProtocolVersions     []string        `json:"protocol_versions"`
	EndpointAllowedHosts []string        `json:"endpoint_allowed_hosts,omitempty"`
	CredentialHeader     string          `json:"credential_header,omitempty"`
	ApprovedTools        []Tool          `json:"approved_tools"`
	ToolSchemaDigest     string          `json:"tool_schema_digest"`
	FailurePolicy        string          `json:"failure_policy"`
}

// PluginContributionPrefix marks a connection contributed by an installed
// Plugin rather than by a workspace's own Remote MCP configuration.
//
// The two kinds share one Connection shape and one broker, but their
// credentials live in different places and are served by different routes. The
// daemon holds only the contribution id at dial time, so the id is what has to
// say which kind it is — inferring it from the string's shape would make a
// cloud-issued id that happened to contain a colon resolve against the plugin
// route.
const PluginContributionPrefix = "plugin:"

// DigestBytes is the content digest used to pin approved tool schemas.
func DigestBytes(content []byte) string {
	digest := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(digest[:])
}
