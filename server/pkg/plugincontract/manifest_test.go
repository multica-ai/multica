package plugincontract

import (
	"encoding/json"
	"strings"
	"testing"
)

func validManifest() Manifest {
	return Manifest{
		APIVersion: APIVersionV1,
		Kind:       KindPlugin,
		Metadata: Metadata{
			Key:       "ai.multica.software-delivery",
			Name:      "Software Delivery Skill Pack",
			Version:   "1.0.0",
			Publisher: "multica",
		},
		Compatibility: Compatibility{
			HostAPI: ">=1.0.0 <2.0.0",
			RequiredDaemonFeatures: []string{
				DaemonFeatureExecutionManifestV1,
				DaemonFeatureAgentSkillV1,
			},
		},
		RequestedCapabilities: []string{CapabilityAgentSkillContribute},
		Contributes: ManifestContributions{
			AgentSkills: []AgentSkillContribution{
				{
					Key:         "review-readiness",
					Name:        "Review Readiness",
					Description: "Prepare an evidence-based review handoff.",
					Entry:       "skills/review-readiness/SKILL.md",
				},
			},
		},
	}
}

func manifestJSON(t *testing.T, manifest Manifest) []byte {
	t.Helper()
	content, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	return content
}

func TestParseManifestAcceptsReferenceContract(t *testing.T) {
	manifest, canonical, err := ParseManifest(manifestJSON(t, validManifest()))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if manifest.Metadata.Key != "ai.multica.software-delivery" {
		t.Fatalf("plugin key = %q", manifest.Metadata.Key)
	}
	if len(canonical) == 0 || canonical[len(canonical)-1] == '\n' {
		t.Fatalf("canonical manifest is not compact JSON: %q", canonical)
	}
}

func TestParseManifestRejectsUnknownExecutableContract(t *testing.T) {
	raw := strings.TrimSuffix(string(manifestJSON(t, validManifest())), "}") + `,"scripts":{"postinstall":"./install.sh"}}`
	if _, _, err := ParseManifest([]byte(raw)); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("ParseManifest unknown scripts error = %v", err)
	}
}

func TestManifestRejectsMissingHostCapabilities(t *testing.T) {
	manifest := validManifest()
	manifest.RequestedCapabilities = nil
	if err := manifest.Validate(); err == nil || !strings.Contains(err.Error(), CapabilityAgentSkillContribute) {
		t.Fatalf("Validate missing capability error = %v", err)
	}

	manifest = validManifest()
	manifest.Compatibility.RequiredDaemonFeatures = []string{DaemonFeatureAgentSkillV1}
	if err := manifest.Validate(); err == nil || !strings.Contains(err.Error(), DaemonFeatureExecutionManifestV1) {
		t.Fatalf("Validate missing daemon feature error = %v", err)
	}
}

func TestManifestRejectsReservedAndNonNamespacedKeys(t *testing.T) {
	manifest := validManifest()
	manifest.Metadata.Key = "review"
	if err := manifest.Validate(); err == nil || !strings.Contains(err.Error(), "reverse-DNS") {
		t.Fatalf("Validate non-namespaced plugin key error = %v", err)
	}

	manifest = validManifest()
	manifest.Contributes.AgentSkills[0].Key = "multica-policy"
	manifest.Contributes.AgentSkills[0].Entry = "skills/multica-policy/SKILL.md"
	if err := manifest.Validate(); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("Validate reserved contribution key error = %v", err)
	}
}

func TestManifestRejectsDuplicateContributionIdentity(t *testing.T) {
	manifest := validManifest()
	manifest.Contributes.AgentSkills = append(manifest.Contributes.AgentSkills, manifest.Contributes.AgentSkills[0])
	if err := manifest.Validate(); err == nil || !strings.Contains(err.Error(), "duplicate contribution key") {
		t.Fatalf("Validate duplicate contribution error = %v", err)
	}
}

func validRemoteMCPManifest() Manifest {
	manifest := validManifest()
	manifest.Metadata.Key = "dev.local.fixture-mcp"
	manifest.Metadata.Name = "Fixture MCP"
	manifest.Contributes.AgentSkills = nil
	manifest.Contributes.RemoteMCP = []RemoteMCPContribution{{
		Key:              "fixture-tools",
		Name:             "Fixture Tools",
		Description:      "Deterministic fixture tools.",
		Transport:        "streamable-http",
		ProtocolVersions: []string{"2025-03-26", "2024-11-05"},
		EndpointPolicy: RemoteMCPEndpointPolicy{
			DefaultEndpoint: "https://mcp.example.test/mcp", AllowedHosts: []string{"mcp.example.test"},
		},
		Authentication: RemoteMCPAuthentication{Preferred: "oauth", Supported: []string{"oauth"}},
		ToolIntent: []RemoteMCPToolIntent{
			{Name: "fixture.read", Description: "Read fixture state.", Risk: "read"},
			{Name: "fixture.write", Description: "Mutate fixture state.", Risk: "write"},
		},
		ConfigurationSchema: json.RawMessage(`{"type":"object","additionalProperties":false}`),
	}}
	manifest.RequestedCapabilities = []string{CapabilityRemoteMCPConnect}
	manifest.Compatibility.RequiredDaemonFeatures = []string{
		DaemonFeatureExecutionManifestV1,
		DaemonFeatureRemoteMCPV1,
	}
	return manifest
}

func TestManifestAcceptsRemoteMCPOnlyAndMixedContributions(t *testing.T) {
	remoteOnly := validRemoteMCPManifest()
	if err := remoteOnly.Validate(); err != nil {
		t.Fatalf("Validate remote-only manifest: %v", err)
	}

	mixed := remoteOnly
	mixed.Contributes.AgentSkills = validManifest().Contributes.AgentSkills
	mixed.RequestedCapabilities = []string{CapabilityAgentSkillContribute, CapabilityRemoteMCPConnect}
	mixed.Compatibility.RequiredDaemonFeatures = append(mixed.Compatibility.RequiredDaemonFeatures, DaemonFeatureAgentSkillV1)
	if err := mixed.Validate(); err != nil {
		t.Fatalf("Validate mixed manifest: %v", err)
	}
}

func TestManifestAcceptsReviewedWildcardRemoteMCPToolIntent(t *testing.T) {
	manifest := validRemoteMCPManifest()
	manifest.Contributes.RemoteMCP[0].ToolIntent = []RemoteMCPToolIntent{{
		Name: RemoteMCPAnyToolIntent, Description: "Discover tools for explicit administrator review.", Risk: "write",
	}}
	if err := manifest.Validate(); err != nil {
		t.Fatalf("Validate wildcard Remote MCP tool intent: %v", err)
	}
}

func TestManifestRemoteMCPFailsClosed(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Manifest)
		want string
	}{
		{name: "stdio transport", edit: func(m *Manifest) { m.Contributes.RemoteMCP[0].Transport = "stdio" }, want: "streamable-http"},
		{name: "unsupported protocol", edit: func(m *Manifest) { m.Contributes.RemoteMCP[0].ProtocolVersions = []string{"2026-01-01"} }, want: "unsupported version"},
		{name: "secret-looking endpoint", edit: func(m *Manifest) {
			m.Contributes.RemoteMCP[0].EndpointPolicy.AllowedHosts = []string{"https://token@mcp.example"}
		}, want: "invalid host"},
		{name: "default endpoint outside allowlist", edit: func(m *Manifest) {
			m.Contributes.RemoteMCP[0].EndpointPolicy.DefaultEndpoint = "https://other.example/mcp"
		}, want: "covered by allowed_hosts"},
		{name: "preferred auth not supported", edit: func(m *Manifest) {
			m.Contributes.RemoteMCP[0].Authentication.Preferred = "bearer"
		}, want: "must appear in supported"},
		{name: "invalid risk", edit: func(m *Manifest) { m.Contributes.RemoteMCP[0].ToolIntent[0].Risk = "admin" }, want: "read or write"},
		{name: "wildcard understates risk", edit: func(m *Manifest) {
			m.Contributes.RemoteMCP[0].ToolIntent = []RemoteMCPToolIntent{{Name: RemoteMCPAnyToolIntent, Risk: "read"}}
		}, want: "must be write for wildcard"},
		{name: "missing capability", edit: func(m *Manifest) { m.RequestedCapabilities = nil }, want: "requested_capabilities"},
		{name: "missing daemon feature", edit: func(m *Manifest) { m.Compatibility.RequiredDaemonFeatures = []string{DaemonFeatureExecutionManifestV1} }, want: DaemonFeatureRemoteMCPV1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest := validRemoteMCPManifest()
			test.edit(&manifest)
			if err := manifest.Validate(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate error = %v, want containing %q", err, test.want)
			}
		})
	}
}
