package handler

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const (
	PlatformExtensionSourceSchemaVersion = "platform.extension/v1"
	PlatformExtensionBundleSchemaVersion = "multica.extension-bundle/v1"
	PlatformExtensionFlowCommandSuffix   = "-e2e"

	PlatformExtensionMaxAgents         = 32
	PlatformExtensionMaxSkills         = 32
	PlatformExtensionMaxCommands       = 128
	PlatformExtensionMaxSkillFiles     = 32
	PlatformExtensionMaxSkillFileBytes = 4 * 1024 * 1024
)

const platformExtensionSkillFileEncodingsConfigKey = "multica_file_encodings"

var platformExtensionWindowsDeviceName = regexp.MustCompile(`(?i)^(con|prn|aux|nul|com[1-9]|lpt[1-9])(?:\..*)?$`)

// PlatformExtensionContractError identifies a rejected platform extension
// contract without leaking parser-specific details into callers.
type PlatformExtensionContractError struct {
	Code    string
	Message string
}

// PlatformExtensionPolicy supplies trusted platform classification rules.
// Document-declared suffixes must match this policy before classification.
type PlatformExtensionPolicy struct {
	CommandSuffixes PlatformExtensionCommandSuffixes
}

// DefaultPlatformExtensionV1Policy returns the mock V1 policy used by the
// compatibility wrappers.
func DefaultPlatformExtensionV1Policy() PlatformExtensionPolicy {
	return PlatformExtensionPolicy{CommandSuffixes: PlatformExtensionCommandSuffixes{
		Flow: []string{".flow"},
		Tool: []string{".tool"},
	}}
}

func (e *PlatformExtensionContractError) Error() string {
	if e.Message == "" {
		return e.Code
	}
	return e.Code + ": " + e.Message
}

type PlatformExtensionSource struct {
	SchemaVersion   string                           `json:"schema_version"`
	Extension       PlatformExtension                `json:"extension"`
	Leader          string                           `json:"leader"`
	Agents          []PlatformExtensionAgent         `json:"agents"`
	Skills          []PlatformExtensionSkill         `json:"skills"`
	Commands        []PlatformExtensionCommand       `json:"commands"`
	CommandSuffixes PlatformExtensionCommandSuffixes `json:"command_suffixes"`
}

type PlatformExtension struct {
	Key          string `json:"key"`
	Name         string `json:"name"`
	Version      string `json:"version"`
	Description  string `json:"description"`
	Instructions string `json:"instructions"`
}

type PlatformExtensionAgent struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Prompt      string `json:"prompt"`
}

type PlatformExtensionSkill struct {
	Key         string                       `json:"key"`
	Name        string                       `json:"name"`
	Description string                       `json:"description"`
	Files       []PlatformExtensionSkillFile `json:"files"`
}

type PlatformExtensionSkillFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	// Encoding is omitted for normal UTF-8 text. Binary archive entries are
	// persisted as base64 so PostgreSQL text storage and the task wire format
	// remain portable; the daemon restores their original bytes at execution.
	Encoding string `json:"encoding,omitempty"`
}

// PlatformExtensionCommand holds only the platform's standard command
// fields. Classification remains a property of the enclosing extension.
type PlatformExtensionCommand struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Content     string          `json:"content"`
	Metadata    json.RawMessage `json:"metadata"`
}

type PlatformExtensionCommandSuffixes struct {
	Flow []string `json:"flow"`
	Tool []string `json:"tool"`
}

type PlatformExtensionBundle struct {
	SchemaVersion     string                           `json:"schema_version"`
	Extension         PlatformExtension                `json:"extension"`
	Digest            string                           `json:"digest,omitempty"`
	Leader            string                           `json:"leader"`
	Agents            []PlatformExtensionAgent         `json:"agents"`
	Skills            []PlatformExtensionSkill         `json:"skills"`
	FlowCommands      []PlatformExtensionCommand       `json:"flow_commands"`
	RuntimeCommands   []PlatformExtensionCommand       `json:"runtime_commands"`
	CommandSuffixes   PlatformExtensionCommandSuffixes `json:"command_suffixes"`
	SquadInstructions string                           `json:"squad_instructions"`
}

// DecodePlatformExtensionSource strictly decodes a source document.
func DecodePlatformExtensionSource(data []byte) (PlatformExtensionSource, error) {
	var source PlatformExtensionSource
	if err := decodePlatformExtensionJSON(data, &source); err != nil {
		return PlatformExtensionSource{}, err
	}
	return source, nil
}

// DecodePlatformExtensionBundle strictly decodes a compiled bundle document.
func DecodePlatformExtensionBundle(data []byte) (PlatformExtensionBundle, error) {
	var bundle PlatformExtensionBundle
	if err := decodePlatformExtensionJSON(data, &bundle); err != nil {
		return PlatformExtensionBundle{}, err
	}
	return bundle, nil
}

func decodePlatformExtensionJSON(data []byte, target any) error {
	if err := rejectPlatformExtensionDuplicateObjectKeys(data); err != nil {
		return platformExtensionCode("EXTENSION_INVALID", err.Error())
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return platformExtensionCode("EXTENSION_INVALID", err.Error())
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return platformExtensionCode("EXTENSION_INVALID", "extension must contain one JSON value")
		}
		return platformExtensionCode("EXTENSION_INVALID", err.Error())
	}
	return nil
}

func rejectPlatformExtensionDuplicateObjectKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := scanPlatformExtensionJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return fmt.Errorf("extension must contain one JSON value")
		}
		return err
	}
	return nil
}

func scanPlatformExtensionJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("object key is not a string")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate object key %q", key)
			}
			seen[key] = struct{}{}
			if err := scanPlatformExtensionJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	case '[':
		for decoder.More() {
			if err := scanPlatformExtensionJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delim)
	}
}

// CompilePlatformExtension validates a source and produces a deterministic,
// immutable bundle suitable for persistence.
func CompilePlatformExtension(source PlatformExtensionSource) (PlatformExtensionBundle, error) {
	return CompilePlatformExtensionWithPolicy(source, DefaultPlatformExtensionV1Policy())
}

// CompilePlatformExtensionWithPolicy compiles a Source against caller-supplied
// trusted classification rules.
func CompilePlatformExtensionWithPolicy(source PlatformExtensionSource, policy PlatformExtensionPolicy) (PlatformExtensionBundle, error) {
	if err := validatePlatformExtensionPolicy(policy); err != nil {
		return PlatformExtensionBundle{}, err
	}
	if err := validatePlatformExtensionCommandSuffixDeclaration(source.CommandSuffixes, policy.CommandSuffixes); err != nil {
		return PlatformExtensionBundle{}, err
	}
	if err := validatePlatformExtensionSource(source); err != nil {
		return PlatformExtensionBundle{}, err
	}
	normalizePlatformExtensionSkillFileEncodings(source.Skills)

	commands := make([]PlatformExtensionCommand, len(source.Commands))
	for i, command := range source.Commands {
		normalized, err := normalizePlatformExtensionCommand(command)
		if err != nil {
			return PlatformExtensionBundle{}, err
		}
		commands[i] = normalized
	}
	flowCommands, runtimeCommands, err := classifyPlatformExtensionCommands(commands, policy.CommandSuffixes)
	if err != nil {
		return PlatformExtensionBundle{}, err
	}

	bundle := PlatformExtensionBundle{
		SchemaVersion:     PlatformExtensionBundleSchemaVersion,
		Extension:         source.Extension,
		Leader:            source.Leader,
		Agents:            source.Agents,
		Skills:            source.Skills,
		FlowCommands:      flowCommands,
		RuntimeCommands:   runtimeCommands,
		CommandSuffixes:   clonePlatformExtensionCommandSuffixes(policy.CommandSuffixes),
		SquadInstructions: platformExtensionSquadInstructions(source, flowCommands),
	}
	digest, err := platformExtensionBundleDigest(bundle)
	if err != nil {
		return PlatformExtensionBundle{}, err
	}
	bundle.Digest = digest
	return bundle, nil
}

// ValidatePlatformExtensionBundle confirms its structure, digest, command
// classifications, and generated squad instructions before import.
func ValidatePlatformExtensionBundle(bundle PlatformExtensionBundle) error {
	return ValidatePlatformExtensionBundleWithPolicy(bundle, DefaultPlatformExtensionV1Policy())
}

// ValidatePlatformExtensionBundleWithPolicy validates a Bundle against
// caller-supplied trusted rules. CanonicalPlatformExtensionBundleJSON returns
// normalized bytes suitable for persistence.
func ValidatePlatformExtensionBundleWithPolicy(bundle PlatformExtensionBundle, policy PlatformExtensionPolicy) error {
	if err := validatePlatformExtensionPolicy(policy); err != nil {
		return err
	}
	if bundle.SchemaVersion != PlatformExtensionBundleSchemaVersion {
		return platformExtensionCode("BUNDLE_SCHEMA_VERSION_INVALID", bundle.SchemaVersion)
	}
	if err := validatePlatformExtensionCommandSuffixDeclaration(bundle.CommandSuffixes, policy.CommandSuffixes); err != nil {
		return err
	}
	canonical, err := canonicalizePlatformExtensionBundleMetadata(bundle)
	if err != nil {
		return err
	}
	if err := validatePlatformExtensionBundleStructure(canonical, policy.CommandSuffixes); err != nil {
		return err
	}
	if strings.TrimSpace(canonical.Digest) == "" {
		return platformExtensionCode("BUNDLE_DIGEST_INVALID", "digest is required")
	}
	digest, err := platformExtensionBundleDigest(canonical)
	if err != nil {
		return err
	}
	if canonical.Digest != digest {
		return platformExtensionCode("BUNDLE_DIGEST_INVALID", "digest does not match bundle content")
	}
	return validatePlatformExtensionSquadInstructions(canonical)
}

// CanonicalPlatformExtensionBundleJSON returns stable indented JSON matching
// the platform-agent-cli bundle representation byte for byte.
func CanonicalPlatformExtensionBundleJSON(bundle PlatformExtensionBundle) ([]byte, error) {
	canonical, err := canonicalizePlatformExtensionBundleMetadata(bundle)
	if err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(canonical, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal extension bundle: %w", err)
	}
	return append(data, '\n'), nil
}

func validatePlatformExtensionSource(source PlatformExtensionSource) error {
	if source.SchemaVersion != PlatformExtensionSourceSchemaVersion {
		return platformExtensionCode("SOURCE_SCHEMA_VERSION_INVALID", source.SchemaVersion)
	}
	if err := requirePlatformExtensionField("extension key", source.Extension.Key); err != nil {
		return err
	}
	if err := requirePlatformExtensionField("extension name", source.Extension.Name); err != nil {
		return err
	}
	if err := requirePlatformExtensionField("extension version", source.Extension.Version); err != nil {
		return err
	}
	if len(source.Agents) == 0 {
		return platformExtensionCode("AGENT_REQUIRED", "extension must include an agent")
	}
	if len(source.Agents) > PlatformExtensionMaxAgents {
		return platformExtensionCode("AGENT_LIMIT_EXCEEDED", fmt.Sprintf("maximum is %d", PlatformExtensionMaxAgents))
	}
	if len(source.Skills) > PlatformExtensionMaxSkills {
		return platformExtensionCode("SKILL_LIMIT_EXCEEDED", fmt.Sprintf("maximum is %d", PlatformExtensionMaxSkills))
	}
	if len(source.Commands) > PlatformExtensionMaxCommands {
		return platformExtensionCode("COMMAND_LIMIT_EXCEEDED", fmt.Sprintf("maximum is %d", PlatformExtensionMaxCommands))
	}

	agentKeys := make(map[string]struct{}, len(source.Agents))
	agentNames := make(map[string]struct{}, len(source.Agents))
	for _, agent := range source.Agents {
		if err := requirePlatformExtensionField("agent key", agent.Key); err != nil {
			return err
		}
		if err := requirePlatformExtensionField("agent name", agent.Name); err != nil {
			return err
		}
		if platformExtensionDuplicate(agentKeys, agent.Key) || platformExtensionDuplicate(agentNames, agent.Name) {
			return platformExtensionCode("DUPLICATE_NAME", "agent key or name")
		}
	}
	if _, ok := agentKeys[source.Leader]; !ok {
		return platformExtensionCode("LEADER_INVALID", source.Leader)
	}

	skillKeys := make(map[string]struct{}, len(source.Skills))
	skillNames := make(map[string]struct{}, len(source.Skills))
	for _, skill := range source.Skills {
		if err := requirePlatformExtensionField("skill key", skill.Key); err != nil {
			return err
		}
		if err := requirePlatformExtensionField("skill name", skill.Name); err != nil {
			return err
		}
		if platformExtensionDuplicate(skillKeys, skill.Key) || platformExtensionDuplicate(skillNames, skill.Name) {
			return platformExtensionCode("DUPLICATE_NAME", "skill key or name")
		}
		if err := validatePlatformExtensionSkillFiles(skill.Files); err != nil {
			return err
		}
	}

	commandNames := make(map[string]struct{}, len(source.Commands))
	for _, command := range source.Commands {
		if err := requirePlatformExtensionField("command name", command.Name); err != nil {
			return err
		}
		if platformExtensionDuplicate(commandNames, command.Name) {
			return platformExtensionCode("DUPLICATE_NAME", "command name")
		}
	}
	return nil
}

func validatePlatformExtensionSkillFiles(files []PlatformExtensionSkillFile) error {
	if len(files) > PlatformExtensionMaxSkillFiles {
		return platformExtensionCode("SKILL_FILE_LIMIT_EXCEEDED", fmt.Sprintf("maximum is %d", PlatformExtensionMaxSkillFiles))
	}
	rootFiles := 0
	paths := make(map[string]struct{}, len(files))
	portablePaths := make(map[string]struct{}, len(files))
	for _, file := range files {
		fileBytes, err := platformExtensionSkillFileBytes(file)
		if err != nil {
			return err
		}
		if len(fileBytes) > PlatformExtensionMaxSkillFileBytes {
			return platformExtensionCode("SKILL_FILE_SIZE_EXCEEDED", fmt.Sprintf("maximum is %d bytes", PlatformExtensionMaxSkillFileBytes))
		}
		if platformExtensionDuplicate(paths, file.Path) {
			if file.Path == "SKILL.md" {
				return platformExtensionCode("SKILL_ROOT_INVALID", "each skill must contain exactly one root SKILL.md")
			}
			return platformExtensionCode("DUPLICATE_SKILL_FILE_PATH", file.Path)
		}
		if reservedPlatformExtensionSkillPath(file.Path) {
			return platformExtensionCode("RESERVED_SKILL_PATH", file.Path)
		}
		if !safePlatformExtensionSkillPath(file.Path) {
			return platformExtensionCode("UNSAFE_SKILL_PATH", file.Path)
		}
		if platformExtensionDuplicate(portablePaths, portablePlatformExtensionSkillPathKey(file.Path)) {
			return platformExtensionCode("DUPLICATE_SKILL_FILE_PATH", file.Path)
		}
		if file.Path == "SKILL.md" {
			if strings.EqualFold(strings.TrimSpace(file.Encoding), "base64") || strings.EqualFold(strings.TrimSpace(file.Encoding), "binary") {
				return platformExtensionCode("SKILL_ROOT_INVALID", "SKILL.md must be UTF-8 text")
			}
			rootFiles++
		}
	}
	if rootFiles != 1 {
		return platformExtensionCode("SKILL_ROOT_INVALID", "each skill must contain exactly one root SKILL.md")
	}
	return nil
}

func platformExtensionSkillFileBytes(file PlatformExtensionSkillFile) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(file.Encoding)) {
	case "", "text":
		return []byte(file.Content), nil
	case "base64", "binary":
		decoded, err := base64.StdEncoding.DecodeString(file.Content)
		if err != nil {
			return nil, platformExtensionCode("SKILL_FILE_ENCODING_INVALID", file.Path)
		}
		return decoded, nil
	default:
		return nil, platformExtensionCode("SKILL_FILE_ENCODING_INVALID", file.Path)
	}
}

func portablePlatformExtensionSkillPathKey(value string) string {
	components := strings.Split(value, "/")
	for i, component := range components {
		component = norm.NFC.String(component)
		component = cases.Fold().String(component)
		component = norm.NFC.String(component)
		components[i] = strings.TrimRight(component, " .")
	}
	return strings.Join(components, "/")
}

func safePlatformExtensionSkillPath(value string) bool {
	if value == "" || strings.ContainsRune(value, 0) || strings.Contains(value, "\\") || strings.HasPrefix(value, "/") || strings.HasPrefix(value, "~") || strings.Contains(value, ":") {
		return false
	}
	clean := path.Clean(value)
	return clean == value && clean != "." && !strings.HasPrefix(clean, "../") && clean != ".."
}

func reservedPlatformExtensionSkillPath(value string) bool {
	if strings.ContainsRune(value, 0) {
		return true
	}
	for _, component := range strings.Split(value, "/") {
		trimmed := strings.TrimRight(component, " .")
		if trimmed == "" {
			continue
		}
		switch strings.ToLower(trimmed) {
		case ".platform-agent", ".agent_context", ".git", "agents.md", ".multica-sidecar-owner":
			return true
		}
		if platformExtensionWindowsDeviceName.MatchString(trimmed) {
			return true
		}
	}
	return false
}

func platformExtensionDuplicate(seen map[string]struct{}, value string) bool {
	if _, ok := seen[value]; ok {
		return true
	}
	seen[value] = struct{}{}
	return false
}

func platformExtensionMatchesSuffix(name string, suffixes []string) bool {
	for _, suffix := range suffixes {
		if suffix != "" && strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

func classifyPlatformExtensionCommands(commands []PlatformExtensionCommand, suffixes PlatformExtensionCommandSuffixes) ([]PlatformExtensionCommand, []PlatformExtensionCommand, error) {
	flowCommands := make([]PlatformExtensionCommand, 0, len(commands))
	runtimeCommands := make([]PlatformExtensionCommand, 0, len(commands))
	for _, command := range commands {
		if platformExtensionMatchesSuffix(command.Name, suffixes.Tool) {
			return nil, nil, platformExtensionCode("TOOL_COMMAND_UNSUPPORTED", command.Name)
		}
		if platformExtensionMatchesFlowCommand(command.Name, suffixes.Flow) {
			flowCommands = append(flowCommands, command)
			continue
		}
		runtimeCommands = append(runtimeCommands, command)
	}
	return flowCommands, runtimeCommands, nil
}

func platformExtensionMatchesFlowCommand(name string, configuredSuffixes []string) bool {
	return strings.HasSuffix(name, PlatformExtensionFlowCommandSuffix) || platformExtensionMatchesSuffix(name, configuredSuffixes)
}

// validatePlatformExtensionImportE2ECommand is intentionally narrower than
// the legacy compiler classification. Older documents may still decode with
// their declared .flow suffixes, but an importable Extension package has one
// unambiguous command that owns the versioned Squad instructions.
func validatePlatformExtensionImportE2ECommand(bundle PlatformExtensionBundle) error {
	matching := 0
	for _, command := range bundle.FlowCommands {
		if strings.HasSuffix(command.Name, PlatformExtensionFlowCommandSuffix) {
			matching++
		}
	}
	if matching != 1 {
		return platformExtensionCode("E2E_COMMAND_INVALID", "extension must contain exactly one Command ending in -e2e")
	}
	return nil
}

func platformExtensionDefaultSquadBaseName(bundle PlatformExtensionBundle) (string, error) {
	for _, command := range bundle.FlowCommands {
		if strings.HasSuffix(command.Name, PlatformExtensionFlowCommandSuffix) {
			baseName := strings.TrimSpace(strings.TrimSuffix(command.Name, PlatformExtensionFlowCommandSuffix))
			if baseName == "" {
				return "", platformExtensionCode("E2E_COMMAND_INVALID", "the -e2e Command must have a name prefix")
			}
			return baseName, nil
		}
	}
	return "", platformExtensionCode("E2E_COMMAND_INVALID", "extension must contain exactly one Command ending in -e2e")
}

func validatePlatformExtensionBundleStructure(bundle PlatformExtensionBundle, trustedSuffixes PlatformExtensionCommandSuffixes) error {
	commands := make([]PlatformExtensionCommand, 0, len(bundle.FlowCommands)+len(bundle.RuntimeCommands))
	commands = append(commands, bundle.FlowCommands...)
	commands = append(commands, bundle.RuntimeCommands...)
	if err := validatePlatformExtensionSource(PlatformExtensionSource{
		SchemaVersion:   PlatformExtensionSourceSchemaVersion,
		Extension:       bundle.Extension,
		Leader:          bundle.Leader,
		Agents:          bundle.Agents,
		Skills:          bundle.Skills,
		Commands:        commands,
		CommandSuffixes: bundle.CommandSuffixes,
	}); err != nil {
		return err
	}
	for _, command := range bundle.FlowCommands {
		if platformExtensionMatchesSuffix(command.Name, trustedSuffixes.Tool) {
			return platformExtensionCode("TOOL_COMMAND_UNSUPPORTED", command.Name)
		}
		if !platformExtensionMatchesFlowCommand(command.Name, trustedSuffixes.Flow) {
			return platformExtensionCode("FLOW_COMMAND_CLASSIFICATION_INVALID", command.Name)
		}
	}
	for _, command := range bundle.RuntimeCommands {
		if platformExtensionMatchesSuffix(command.Name, trustedSuffixes.Tool) {
			return platformExtensionCode("TOOL_COMMAND_UNSUPPORTED", command.Name)
		}
		if platformExtensionMatchesFlowCommand(command.Name, trustedSuffixes.Flow) {
			return platformExtensionCode("RUNTIME_COMMAND_CLASSIFICATION_INVALID", command.Name)
		}
	}
	return nil
}

func validatePlatformExtensionSquadInstructions(bundle PlatformExtensionBundle) error {
	expected := platformExtensionSquadInstructions(PlatformExtensionSource{
		Extension: bundle.Extension,
		Leader:    bundle.Leader,
		Agents:    bundle.Agents,
	}, bundle.FlowCommands)
	if bundle.SquadInstructions != expected {
		return platformExtensionCode("SQUAD_INSTRUCTIONS_INVALID", "squad instructions do not match bundle content")
	}
	return nil
}

func requirePlatformExtensionField(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return platformExtensionCode("REQUIRED_FIELD", field)
	}
	return nil
}

func normalizePlatformExtensionCommand(command PlatformExtensionCommand) (PlatformExtensionCommand, error) {
	if len(command.Metadata) == 0 {
		return command, nil
	}
	if err := rejectPlatformExtensionDuplicateObjectKeys(command.Metadata); err != nil {
		return PlatformExtensionCommand{}, platformExtensionCode("COMMAND_METADATA_INVALID", err.Error())
	}
	decoder := json.NewDecoder(bytes.NewReader(command.Metadata))
	decoder.UseNumber()
	var metadata any
	if err := decoder.Decode(&metadata); err != nil {
		return PlatformExtensionCommand{}, platformExtensionCode("COMMAND_METADATA_INVALID", err.Error())
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return PlatformExtensionCommand{}, platformExtensionCode("COMMAND_METADATA_INVALID", "metadata must contain one JSON value")
	}
	canonical, err := json.Marshal(metadata)
	if err != nil {
		return PlatformExtensionCommand{}, fmt.Errorf("marshal command metadata: %w", err)
	}
	command.Metadata = canonical
	return command, nil
}

func canonicalizePlatformExtensionBundleMetadata(bundle PlatformExtensionBundle) (PlatformExtensionBundle, error) {
	var err error
	bundle.FlowCommands, err = normalizePlatformExtensionCommands(bundle.FlowCommands)
	if err != nil {
		return PlatformExtensionBundle{}, err
	}
	bundle.RuntimeCommands, err = normalizePlatformExtensionCommands(bundle.RuntimeCommands)
	if err != nil {
		return PlatformExtensionBundle{}, err
	}
	normalizePlatformExtensionSkillFileEncodings(bundle.Skills)
	return bundle, nil
}

func normalizePlatformExtensionSkillFileEncodings(skills []PlatformExtensionSkill) {
	for skillIndex := range skills {
		for fileIndex := range skills[skillIndex].Files {
			file := &skills[skillIndex].Files[fileIndex]
			switch strings.ToLower(strings.TrimSpace(file.Encoding)) {
			case "", "text":
				file.Encoding = ""
			case "base64", "binary":
				file.Encoding = "base64"
			}
		}
	}
}

func normalizePlatformExtensionCommands(commands []PlatformExtensionCommand) ([]PlatformExtensionCommand, error) {
	normalized := make([]PlatformExtensionCommand, len(commands))
	for i, command := range commands {
		var err error
		normalized[i], err = normalizePlatformExtensionCommand(command)
		if err != nil {
			return nil, err
		}
	}
	return normalized, nil
}

func platformExtensionBundleDigest(bundle PlatformExtensionBundle) (string, error) {
	bundle.Digest = ""
	data, err := CanonicalPlatformExtensionBundleJSON(bundle)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func validatePlatformExtensionCommandSuffixDeclaration(got, trusted PlatformExtensionCommandSuffixes) error {
	if !slices.Equal(got.Flow, trusted.Flow) || !slices.Equal(got.Tool, trusted.Tool) {
		return platformExtensionCode("COMMAND_SUFFIX_POLICY_MISMATCH", "command_suffixes do not match trusted policy")
	}
	return nil
}

func validatePlatformExtensionPolicy(policy PlatformExtensionPolicy) error {
	if len(policy.CommandSuffixes.Tool) == 0 {
		return platformExtensionCode("COMMAND_SUFFIX_POLICY_INVALID", "trusted policy must include a tool suffix")
	}
	seen := make(map[string]struct{}, len(policy.CommandSuffixes.Flow)+len(policy.CommandSuffixes.Tool))
	for _, suffixes := range [][]string{policy.CommandSuffixes.Flow, policy.CommandSuffixes.Tool} {
		for _, suffix := range suffixes {
			if strings.TrimSpace(suffix) == "" {
				return platformExtensionCode("COMMAND_SUFFIX_POLICY_INVALID", "trusted suffixes must be non-empty")
			}
			if _, exists := seen[suffix]; exists {
				return platformExtensionCode("COMMAND_SUFFIX_POLICY_INVALID", "trusted suffixes must be unique across classifications")
			}
			seen[suffix] = struct{}{}
		}
	}
	for _, flowSuffix := range policy.CommandSuffixes.Flow {
		for _, toolSuffix := range policy.CommandSuffixes.Tool {
			if strings.HasSuffix(flowSuffix, toolSuffix) || strings.HasSuffix(toolSuffix, flowSuffix) {
				return platformExtensionCode("COMMAND_SUFFIX_POLICY_INVALID", "flow and tool suffixes must not overlap")
			}
		}
	}
	return nil
}

func clonePlatformExtensionCommandSuffixes(value PlatformExtensionCommandSuffixes) PlatformExtensionCommandSuffixes {
	return PlatformExtensionCommandSuffixes{
		Flow: append([]string(nil), value.Flow...),
		Tool: append([]string(nil), value.Tool...),
	}
}

func platformExtensionSquadInstructions(source PlatformExtensionSource, flowCommands []PlatformExtensionCommand) string {
	leader := source.Agents[0]
	for _, agent := range source.Agents {
		if agent.Key == source.Leader {
			leader = agent
			break
		}
	}

	var out strings.Builder
	fmt.Fprintf(&out, "Extension: %s\nDescription: %s\nTeam goal: %s\n\n", source.Extension.Name, source.Extension.Description, source.Extension.Instructions)
	fmt.Fprintf(&out, "Leader: %s — %s\n", leader.Name, leader.Description)
	if len(source.Agents) > 1 {
		out.WriteString("Members:\n")
		for _, agent := range source.Agents {
			if agent.Key != source.Leader {
				fmt.Fprintf(&out, "- %s — %s\n", agent.Name, agent.Description)
			}
		}
	}
	if len(flowCommands) > 0 {
		out.WriteString("\nFlow commands:\n")
		for _, command := range flowCommands {
			fmt.Fprintf(&out, "- %s\n  Description: %s\n  Content: %s\n", command.Name, command.Description, command.Content)
		}
	}
	out.WriteString("\nDelegation: The leader selects members at runtime; no fixed DAG is precompiled.\n")
	out.WriteString("Completion: The leader is responsible for the final result.\n")
	return out.String()
}

func platformExtensionCode(code, message string) error {
	return &PlatformExtensionContractError{Code: code, Message: message}
}
