// Package plugincontract defines the versioned public contract between Plugin
// publishers and the Multica host. It must remain independent from private
// handlers, services, and database implementations.
package plugincontract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
)

const (
	APIVersionV1 = "multica.plugin/v1"
	KindPlugin   = "Plugin"

	ContributionAgentSkillV1       = "agent.skill.v1"
	CapabilityAgentSkillContribute = "agent.skill.contribute"

	DaemonFeatureExecutionManifestV1 = "execution-manifest-v1"
	DaemonFeatureAgentSkillV1        = "agent-skill-v1"

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
	AgentSkills []AgentSkillContribution `json:"agent_skills"`
}

type AgentSkillContribution struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Entry       string `json:"entry"`
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
	if len(capabilities) != 1 || !capabilities[CapabilityAgentSkillContribute] {
		return fmt.Errorf("requested_capabilities must contain only %q in V1", CapabilityAgentSkillContribute)
	}

	features, err := uniqueStrings("compatibility.required_daemon_features", m.Compatibility.RequiredDaemonFeatures)
	if err != nil {
		return err
	}
	for _, required := range []string{DaemonFeatureExecutionManifestV1, DaemonFeatureAgentSkillV1} {
		if !features[required] {
			return fmt.Errorf("compatibility.required_daemon_features must include %q", required)
		}
	}

	if len(m.Contributes.AgentSkills) == 0 {
		return fmt.Errorf("contributes.agent_skills must contain at least one contribution")
	}
	keys := make(map[string]bool, len(m.Contributes.AgentSkills))
	names := make(map[string]bool, len(m.Contributes.AgentSkills))
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

	return nil
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
