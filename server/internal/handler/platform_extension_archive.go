package handler

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

const (
	platformExtensionArchiveManifestPath = "extension.json"
	codeAgentExtensionManifestPath       = "codeagent-extension.json"
	codeAgentExtensionAgentsPrefix       = "agents"
	codeAgentExtensionCommandsPrefix     = "commands"
	platformExtensionArchiveSkillPrefix  = "skills"
	platformExtensionArchiveTotalBytes   = 16 * 1024 * 1024
)

type codeAgentExtensionManifest struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

// codeAgentExtensionMarkdownFrontmatter is deliberately small: CodeAgent
// resources are Markdown documents, so their body remains the agent prompt or
// command content while their standard YAML frontmatter provides identity.
// Unknown keys remain forward-compatible and are not materialized by Multica.
type codeAgentExtensionMarkdownFrontmatter struct {
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	Leader      bool           `yaml:"leader"`
	Metadata    map[string]any `yaml:"metadata"`
}

// decodePlatformExtensionArchiveImport accepts the portable extension package
// layout: extension.json plus skills/<skill-key>/<declared file path>. Text
// files are stored directly; binary entries are base64 encoded for the
// existing text-backed skill-file store and tagged for daemon restoration.
func decodePlatformExtensionArchiveImport(data []byte, policy PlatformExtensionPolicy) (PlatformExtensionBundle, []byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return PlatformExtensionBundle{}, nil, platformExtensionCode("EXTENSION_ARCHIVE_INVALID", "upload is not a valid ZIP archive")
	}
	entries := make(map[string]*zip.File, len(reader.File))
	if len(reader.File) > 1+PlatformExtensionMaxAgents+PlatformExtensionMaxCommands+PlatformExtensionMaxSkills*(1+PlatformExtensionMaxSkillFiles) {
		return PlatformExtensionBundle{}, nil, platformExtensionCode("EXTENSION_ARCHIVE_INVALID", "archive contains too many entries")
	}
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		if file.FileInfo().Mode()&os.ModeSymlink != 0 {
			return PlatformExtensionBundle{}, nil, platformExtensionCode("EXTENSION_ARCHIVE_INVALID", "symbolic links are not allowed")
		}
		if !safePlatformExtensionArchivePath(file.Name) {
			return PlatformExtensionBundle{}, nil, platformExtensionCode("EXTENSION_ARCHIVE_INVALID", "unsafe archive entry "+file.Name)
		}
		if _, exists := entries[file.Name]; exists {
			return PlatformExtensionBundle{}, nil, platformExtensionCode("EXTENSION_ARCHIVE_INVALID", "duplicate archive entry "+file.Name)
		}
		entries[file.Name] = file
	}
	if _, hasCodeAgentLayout := entries[codeAgentExtensionManifestPath]; hasCodeAgentLayout {
		if _, hasLegacyManifest := entries[platformExtensionArchiveManifestPath]; hasLegacyManifest {
			return PlatformExtensionBundle{}, nil, platformExtensionCode("EXTENSION_ARCHIVE_INVALID", "archive cannot contain both extension.json and codeagent-extension.json")
		}
		return decodeCodeAgentExtensionArchiveImport(entries, policy)
	}

	manifestFile, ok := entries[platformExtensionArchiveManifestPath]
	if !ok {
		return PlatformExtensionBundle{}, nil, platformExtensionCode("EXTENSION_ARCHIVE_INVALID", "archive must contain extension.json")
	}
	manifestData, err := readPlatformExtensionArchiveFile(manifestFile, platformExtensionArchiveTotalBytes)
	if err != nil {
		return PlatformExtensionBundle{}, nil, platformExtensionCode("EXTENSION_ARCHIVE_INVALID", "read extension.json: "+err.Error())
	}
	source, err := DecodePlatformExtensionSource(manifestData)
	if err != nil {
		return PlatformExtensionBundle{}, nil, err
	}
	usedEntries, err := hydratePlatformExtensionArchiveSkillFiles(&source, entries, len(manifestData))
	if err != nil {
		return PlatformExtensionBundle{}, nil, err
	}
	for entryName := range entries {
		if _, used := usedEntries[entryName]; !used {
			return PlatformExtensionBundle{}, nil, platformExtensionCode("EXTENSION_ARCHIVE_INVALID", "undeclared archive entry "+entryName)
		}
	}
	bundle, err := CompilePlatformExtensionWithPolicy(source, policy)
	if err != nil {
		return PlatformExtensionBundle{}, nil, err
	}
	if err := validatePlatformExtensionImportE2ECommand(bundle); err != nil {
		return PlatformExtensionBundle{}, nil, err
	}
	manifest, err := CanonicalPlatformExtensionBundleJSON(bundle)
	if err != nil {
		return PlatformExtensionBundle{}, nil, platformExtensionCode("EXTENSION_INVALID", err.Error())
	}
	return bundle, manifest, nil
}

// decodeCodeAgentExtensionArchiveImport converts the portable CodeAgent
// directory layout into the canonical Multica Extension bundle. The package
// root owns the Extension identity while individual resource definitions stay
// in their corresponding agents/, commands/, and skills/ directories.
func decodeCodeAgentExtensionArchiveImport(entries map[string]*zip.File, policy PlatformExtensionPolicy) (PlatformExtensionBundle, []byte, error) {
	totalBytes := 0
	manifestData, err := readCodeAgentExtensionArchiveFile(entries[codeAgentExtensionManifestPath], &totalBytes)
	if err != nil {
		return PlatformExtensionBundle{}, nil, platformExtensionCode("EXTENSION_ARCHIVE_INVALID", "read codeagent-extension.json: "+err.Error())
	}
	var manifest codeAgentExtensionManifest
	if err := decodePlatformExtensionJSON(manifestData, &manifest); err != nil {
		return PlatformExtensionBundle{}, nil, err
	}
	extensionKey := codeAgentExtensionKey(manifest.Name)
	if extensionKey == "" {
		return PlatformExtensionBundle{}, nil, platformExtensionCode("REQUIRED_FIELD", "extension name")
	}

	agentsByKey := make(map[string]PlatformExtensionAgent)
	commandsByName := make(map[string]PlatformExtensionCommand)
	skillMetadata := make(map[string]codeAgentExtensionMarkdownFrontmatter)
	skillFiles := make(map[string][]PlatformExtensionSkillFile)
	leaders := make([]string, 0, 1)

	entryNames := make([]string, 0, len(entries))
	for entryName := range entries {
		entryNames = append(entryNames, entryName)
	}
	sort.Strings(entryNames)
	for _, entryName := range entryNames {
		if entryName == codeAgentExtensionManifestPath {
			continue
		}
		archiveFile := entries[entryName]
		switch {
		case strings.HasPrefix(entryName, codeAgentExtensionAgentsPrefix+"/"):
			key, ok := codeAgentExtensionMarkdownResourceKey(entryName, codeAgentExtensionAgentsPrefix)
			if !ok {
				return PlatformExtensionBundle{}, nil, platformExtensionCode("EXTENSION_ARCHIVE_INVALID", "invalid agent entry "+entryName)
			}
			data, err := readCodeAgentExtensionArchiveFile(archiveFile, &totalBytes)
			if err != nil {
				return PlatformExtensionBundle{}, nil, platformExtensionCode("EXTENSION_ARCHIVE_INVALID", "read "+entryName+": "+err.Error())
			}
			frontmatter, prompt, err := decodeCodeAgentExtensionMarkdown(data)
			if err != nil {
				return PlatformExtensionBundle{}, nil, platformExtensionCode("EXTENSION_ARCHIVE_INVALID", "read "+entryName+": "+err.Error())
			}
			agentsByKey[key] = PlatformExtensionAgent{Key: key, Name: frontmatter.Name, Description: frontmatter.Description, Prompt: prompt}
			if frontmatter.Leader {
				leaders = append(leaders, key)
			}
		case strings.HasPrefix(entryName, codeAgentExtensionCommandsPrefix+"/"):
			name, ok := codeAgentExtensionMarkdownResourceKey(entryName, codeAgentExtensionCommandsPrefix)
			if !ok {
				return PlatformExtensionBundle{}, nil, platformExtensionCode("EXTENSION_ARCHIVE_INVALID", "invalid command entry "+entryName)
			}
			data, err := readCodeAgentExtensionArchiveFile(archiveFile, &totalBytes)
			if err != nil {
				return PlatformExtensionBundle{}, nil, platformExtensionCode("EXTENSION_ARCHIVE_INVALID", "read "+entryName+": "+err.Error())
			}
			frontmatter, content, err := decodeCodeAgentExtensionMarkdown(data)
			if err != nil {
				return PlatformExtensionBundle{}, nil, platformExtensionCode("EXTENSION_ARCHIVE_INVALID", "read "+entryName+": "+err.Error())
			}
			metadata := json.RawMessage(`{}`)
			if len(frontmatter.Metadata) > 0 {
				encoded, err := json.Marshal(frontmatter.Metadata)
				if err != nil {
					return PlatformExtensionBundle{}, nil, platformExtensionCode("EXTENSION_ARCHIVE_INVALID", "encode metadata in "+entryName+": "+err.Error())
				}
				metadata = encoded
			}
			commandsByName[name] = PlatformExtensionCommand{Name: name, Description: frontmatter.Description, Content: content, Metadata: metadata}
		case strings.HasPrefix(entryName, platformExtensionArchiveSkillPrefix+"/"):
			skillKey, skillPath, ok := codeAgentExtensionSkillArchiveEntry(entryName)
			if !ok {
				return PlatformExtensionBundle{}, nil, platformExtensionCode("EXTENSION_ARCHIVE_INVALID", "invalid skill entry "+entryName)
			}
			data, err := readCodeAgentExtensionArchiveFile(archiveFile, &totalBytes)
			if err != nil {
				return PlatformExtensionBundle{}, nil, platformExtensionCode("EXTENSION_ARCHIVE_INVALID", "read "+entryName+": "+err.Error())
			}
			if skillPath == "skill.json" {
				return PlatformExtensionBundle{}, nil, platformExtensionCode("EXTENSION_ARCHIVE_INVALID", "skills use SKILL.md frontmatter, not "+entryName)
			}
			if skillPath == "SKILL.md" {
				frontmatter, _, err := decodeCodeAgentExtensionMarkdown(data)
				if err != nil {
					return PlatformExtensionBundle{}, nil, platformExtensionCode("EXTENSION_ARCHIVE_INVALID", "read "+entryName+": "+err.Error())
				}
				skillMetadata[skillKey] = frontmatter
			}
			file := PlatformExtensionSkillFile{Path: skillPath}
			if utf8.Valid(data) {
				file.Content = string(data)
			} else {
				file.Content = base64.StdEncoding.EncodeToString(data)
				file.Encoding = "base64"
			}
			skillFiles[skillKey] = append(skillFiles[skillKey], file)
		default:
			return PlatformExtensionBundle{}, nil, platformExtensionCode("EXTENSION_ARCHIVE_INVALID", "undeclared archive entry "+entryName)
		}
	}
	if len(leaders) != 1 {
		return PlatformExtensionBundle{}, nil, platformExtensionCode("LEADER_INVALID", "CodeAgent archive must declare exactly one leader")
	}

	source := PlatformExtensionSource{
		SchemaVersion: PlatformExtensionSourceSchemaVersion,
		Extension: PlatformExtension{
			Key:         extensionKey,
			Name:        manifest.Name,
			Version:     manifest.Version,
			Description: manifest.Description,
		},
		Leader:          leaders[0],
		CommandSuffixes: clonePlatformExtensionCommandSuffixes(policy.CommandSuffixes),
	}
	source.Agents = codeAgentExtensionAgents(agentsByKey)
	source.Commands = codeAgentExtensionCommands(commandsByName)
	source.Skills, err = codeAgentExtensionSkills(skillMetadata, skillFiles)
	if err != nil {
		return PlatformExtensionBundle{}, nil, err
	}
	bundle, err := CompilePlatformExtensionWithPolicy(source, policy)
	if err != nil {
		return PlatformExtensionBundle{}, nil, err
	}
	if err := validatePlatformExtensionImportE2ECommand(bundle); err != nil {
		return PlatformExtensionBundle{}, nil, err
	}
	canonicalManifest, err := CanonicalPlatformExtensionBundleJSON(bundle)
	if err != nil {
		return PlatformExtensionBundle{}, nil, platformExtensionCode("EXTENSION_INVALID", err.Error())
	}
	return bundle, canonicalManifest, nil
}

func readCodeAgentExtensionArchiveFile(file *zip.File, totalBytes *int) ([]byte, error) {
	data, err := readPlatformExtensionArchiveFile(file, platformExtensionArchiveTotalBytes-*totalBytes)
	if err != nil {
		return nil, err
	}
	*totalBytes += len(data)
	return data, nil
}

func codeAgentExtensionMarkdownResourceKey(entryName, directory string) (string, bool) {
	prefix := directory + "/"
	if !strings.HasPrefix(entryName, prefix) {
		return "", false
	}
	remainder := strings.TrimPrefix(entryName, prefix)
	key := strings.TrimSuffix(remainder, ".md")
	if key == remainder || !safePlatformExtensionArchiveComponent(key) {
		return "", false
	}
	return key, true
}

func decodeCodeAgentExtensionMarkdown(data []byte) (codeAgentExtensionMarkdownFrontmatter, string, error) {
	if !utf8.Valid(data) {
		return codeAgentExtensionMarkdownFrontmatter{}, "", errors.New("Markdown must be UTF-8 text")
	}
	lines := strings.SplitAfter(string(data), "\n")
	if len(lines) < 3 || strings.TrimRight(lines[0], "\r\n") != "---" {
		return codeAgentExtensionMarkdownFrontmatter{}, "", errors.New("Markdown must start with YAML frontmatter")
	}
	for index := 1; index < len(lines); index++ {
		if strings.TrimRight(lines[index], "\r\n") != "---" {
			continue
		}
		var frontmatter codeAgentExtensionMarkdownFrontmatter
		if err := yaml.Unmarshal([]byte(strings.Join(lines[1:index], "")), &frontmatter); err != nil {
			return codeAgentExtensionMarkdownFrontmatter{}, "", fmt.Errorf("invalid YAML frontmatter: %w", err)
		}
		return frontmatter, strings.Join(lines[index+1:], ""), nil
	}
	return codeAgentExtensionMarkdownFrontmatter{}, "", errors.New("Markdown frontmatter is missing its closing delimiter")
}

func codeAgentExtensionSkillArchiveEntry(entryName string) (string, string, bool) {
	remainder := strings.TrimPrefix(entryName, platformExtensionArchiveSkillPrefix+"/")
	skillKey, skillPath, found := strings.Cut(remainder, "/")
	if !found || !safePlatformExtensionArchiveComponent(skillKey) || !safePlatformExtensionSkillPath(skillPath) {
		return "", "", false
	}
	return skillKey, skillPath, true
}

func codeAgentExtensionAgents(agentsByKey map[string]PlatformExtensionAgent) []PlatformExtensionAgent {
	keys := make([]string, 0, len(agentsByKey))
	for key := range agentsByKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	agents := make([]PlatformExtensionAgent, 0, len(keys))
	for _, key := range keys {
		agents = append(agents, agentsByKey[key])
	}
	return agents
}

func codeAgentExtensionCommands(commandsByName map[string]PlatformExtensionCommand) []PlatformExtensionCommand {
	names := make([]string, 0, len(commandsByName))
	for name := range commandsByName {
		names = append(names, name)
	}
	sort.Strings(names)
	commands := make([]PlatformExtensionCommand, 0, len(names))
	for _, name := range names {
		commands = append(commands, commandsByName[name])
	}
	return commands
}

func codeAgentExtensionSkills(metadataByKey map[string]codeAgentExtensionMarkdownFrontmatter, filesByKey map[string][]PlatformExtensionSkillFile) ([]PlatformExtensionSkill, error) {
	keys := make([]string, 0, len(metadataByKey))
	for key := range metadataByKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	skills := make([]PlatformExtensionSkill, 0, len(keys))
	for _, key := range keys {
		files := filesByKey[key]
		if len(files) == 0 {
			return nil, platformExtensionCode("SKILL_ROOT_INVALID", "skill "+key+" has no files")
		}
		sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
		metadata := metadataByKey[key]
		skills = append(skills, PlatformExtensionSkill{Key: key, Name: metadata.Name, Description: metadata.Description, Files: files})
	}
	for key := range filesByKey {
		if _, ok := metadataByKey[key]; !ok {
			return nil, platformExtensionCode("EXTENSION_ARCHIVE_FILE_MISSING", "skills/"+key+"/SKILL.md")
		}
	}
	return skills, nil
}

func codeAgentExtensionKey(name string) string {
	var key strings.Builder
	separator := false
	for _, character := range strings.ToLower(strings.TrimSpace(name)) {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			key.WriteRune(character)
			separator = false
			continue
		}
		if key.Len() > 0 && !separator {
			key.WriteByte('-')
			separator = true
		}
	}
	return strings.Trim(key.String(), "-")
}

func hydratePlatformExtensionArchiveSkillFiles(source *PlatformExtensionSource, entries map[string]*zip.File, totalBytes int) (map[string]struct{}, error) {
	usedEntries := map[string]struct{}{platformExtensionArchiveManifestPath: {}}
	for skillIndex := range source.Skills {
		skill := &source.Skills[skillIndex]
		if !safePlatformExtensionArchiveComponent(skill.Key) {
			return nil, platformExtensionCode("EXTENSION_ARCHIVE_INVALID", "unsafe skill key "+skill.Key)
		}
		for fileIndex := range skill.Files {
			file := &skill.Files[fileIndex]
			if !safePlatformExtensionSkillPath(file.Path) {
				return nil, platformExtensionCode("UNSAFE_SKILL_PATH", file.Path)
			}
			entryName := path.Join(platformExtensionArchiveSkillPrefix, skill.Key, file.Path)
			archiveFile, ok := entries[entryName]
			if !ok {
				return nil, platformExtensionCode("EXTENSION_ARCHIVE_FILE_MISSING", entryName)
			}
			remaining := platformExtensionArchiveTotalBytes - totalBytes
			content, err := readPlatformExtensionArchiveFile(archiveFile, remaining)
			if err != nil {
				return nil, platformExtensionCode("EXTENSION_ARCHIVE_INVALID", "read "+entryName+": "+err.Error())
			}
			totalBytes += len(content)
			if totalBytes > platformExtensionArchiveTotalBytes {
				return nil, platformExtensionCode("EXTENSION_ARCHIVE_INVALID", "archive expands beyond the size limit")
			}
			switch strings.ToLower(strings.TrimSpace(file.Encoding)) {
			case "", "text":
				if !utf8.Valid(content) {
					return nil, platformExtensionCode("SKILL_FILE_ENCODING_INVALID", file.Path)
				}
				file.Content = string(content)
				file.Encoding = ""
			case "base64", "binary":
				file.Content = base64.StdEncoding.EncodeToString(content)
				file.Encoding = "base64"
			default:
				return nil, platformExtensionCode("SKILL_FILE_ENCODING_INVALID", file.Path)
			}
			usedEntries[entryName] = struct{}{}
		}
	}
	return usedEntries, nil
}

func readPlatformExtensionArchiveFile(file *zip.File, limit int) ([]byte, error) {
	if limit < 0 || file.UncompressedSize64 > uint64(limit) {
		return nil, fmt.Errorf("file exceeds the size limit")
	}
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, fmt.Errorf("file exceeds the size limit")
	}
	return data, nil
}

func safePlatformExtensionArchivePath(value string) bool {
	if value == platformExtensionArchiveManifestPath {
		return true
	}
	if strings.ContainsRune(value, 0) || strings.Contains(value, "\\") || strings.HasPrefix(value, "/") {
		return false
	}
	return path.Clean(value) == value && !strings.HasPrefix(value, "../") && value != ".."
}

func safePlatformExtensionArchiveComponent(value string) bool {
	return value != "" && safePlatformExtensionArchivePath(value) && !strings.Contains(value, "/")
}
