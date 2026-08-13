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
