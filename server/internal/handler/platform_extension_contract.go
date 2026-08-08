package handler

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"
)

const (
	PlatformExtensionSourceSchemaVersion = "platform.extension/v1"
	PlatformExtensionBundleSchemaVersion = "multica.extension-bundle/v1"

	PlatformExtensionMaxAgents         = 32
	PlatformExtensionMaxSkills         = 32
	PlatformExtensionMaxCommands       = 128
	PlatformExtensionMaxSkillFiles     = 32
	PlatformExtensionMaxSkillFileBytes = 256 * 1024
)

var platformExtensionWindowsDeviceName = regexp.MustCompile(`(?i)^(con|prn|aux|nul|com[1-9]|lpt[1-9])(?:\..*)?$`)

// PlatformExtensionContractError identifies a rejected platform extension
// contract without leaking parser-specific details into callers.
type PlatformExtensionContractError struct {
	Code    string
	Message string
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

// CompilePlatformExtension validates a source and produces a deterministic,
// immutable bundle suitable for persistence.
func CompilePlatformExtension(source PlatformExtensionSource) (PlatformExtensionBundle, error) {
	if err := validatePlatformExtensionSource(source); err != nil {
		return PlatformExtensionBundle{}, err
	}

	commands := make([]PlatformExtensionCommand, len(source.Commands))
	for i, command := range source.Commands {
		normalized, err := normalizePlatformExtensionCommand(command)
		if err != nil {
			return PlatformExtensionBundle{}, err
		}
		commands[i] = normalized
	}
	flowCommands, runtimeCommands, err := classifyPlatformExtensionCommands(commands, source.CommandSuffixes)
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
		CommandSuffixes:   source.CommandSuffixes,
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
	if bundle.SchemaVersion != PlatformExtensionBundleSchemaVersion {
		return platformExtensionCode("BUNDLE_SCHEMA_VERSION_INVALID", bundle.SchemaVersion)
	}
	if err := validatePlatformExtensionBundleStructure(bundle); err != nil {
		return err
	}
	if strings.TrimSpace(bundle.Digest) == "" {
		return platformExtensionCode("BUNDLE_DIGEST_INVALID", "digest is required")
	}
	digest, err := platformExtensionBundleDigest(bundle)
	if err != nil {
		return err
	}
	if bundle.Digest != digest {
		return platformExtensionCode("BUNDLE_DIGEST_INVALID", "digest does not match bundle content")
	}
	return validatePlatformExtensionSquadInstructions(bundle)
}

// CanonicalPlatformExtensionBundleJSON returns stable indented JSON matching
// the platform-agent-cli bundle representation byte for byte.
func CanonicalPlatformExtensionBundleJSON(bundle PlatformExtensionBundle) ([]byte, error) {
	data, err := json.MarshalIndent(bundle, "", "  ")
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
	for _, file := range files {
		if len(file.Content) > PlatformExtensionMaxSkillFileBytes {
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
		if file.Path == "SKILL.md" {
			rootFiles++
		}
	}
	if rootFiles != 1 {
		return platformExtensionCode("SKILL_ROOT_INVALID", "each skill must contain exactly one root SKILL.md")
	}
	return nil
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
		case ".platform-agent", ".agent_context", ".git", "agents.md":
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
		if platformExtensionMatchesSuffix(command.Name, suffixes.Flow) {
			flowCommands = append(flowCommands, command)
			continue
		}
		runtimeCommands = append(runtimeCommands, command)
	}
	return flowCommands, runtimeCommands, nil
}

func validatePlatformExtensionBundleStructure(bundle PlatformExtensionBundle) error {
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
		if platformExtensionMatchesSuffix(command.Name, bundle.CommandSuffixes.Tool) {
			return platformExtensionCode("TOOL_COMMAND_UNSUPPORTED", command.Name)
		}
		if !platformExtensionMatchesSuffix(command.Name, bundle.CommandSuffixes.Flow) {
			return platformExtensionCode("FLOW_COMMAND_CLASSIFICATION_INVALID", command.Name)
		}
	}
	for _, command := range bundle.RuntimeCommands {
		if platformExtensionMatchesSuffix(command.Name, bundle.CommandSuffixes.Tool) {
			return platformExtensionCode("TOOL_COMMAND_UNSUPPORTED", command.Name)
		}
		if platformExtensionMatchesSuffix(command.Name, bundle.CommandSuffixes.Flow) {
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

func platformExtensionBundleDigest(bundle PlatformExtensionBundle) (string, error) {
	bundle.Digest = ""
	data, err := CanonicalPlatformExtensionBundleJSON(bundle)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
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
