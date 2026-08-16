package service

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
	"github.com/multica-ai/multica/server/pkg/pluginruntime"
	"github.com/multica-ai/multica/server/pkg/skillbundle"
)

// compilePluginSkillFiles turns every artifact file owned by a Skill root into
// an immutable execution-manifest pin. The manifest still declares only the
// canonical SKILL.md entry; companion files are discovered by convention so a
// community Skill directory can be packaged without translating its layout.
func compilePluginSkillFiles(releaseID pgtype.UUID, contributionKey, entryPath string, files []db.PluginArtifactFile) ([]pluginruntime.SkillFile, []skillbundle.File, error) {
	root, err := pluginSkillRoot(contributionKey, entryPath)
	if err != nil {
		return nil, nil, err
	}
	foundEntry := false
	seen := make(map[string]struct{})
	pins := make([]pluginruntime.SkillFile, 0)
	bundleFiles := make([]skillbundle.File, 0)
	for _, file := range files {
		if file.ReleaseID != releaseID {
			return nil, nil, fmt.Errorf("plugin Skill artifact file belongs to another release")
		}
		if file.Path == entryPath {
			foundEntry = true
			continue
		}
		if !strings.HasPrefix(file.Path, root) {
			continue
		}
		relative := strings.TrimPrefix(file.Path, root)
		if err := validatePluginSkillRelativePath(relative); err != nil {
			return nil, nil, err
		}
		if _, exists := seen[relative]; exists {
			return nil, nil, fmt.Errorf("plugin Skill contains duplicate companion path %q", relative)
		}
		seen[relative] = struct{}{}
		if file.Digest != plugincontract.DigestBytes([]byte(file.Content)) || file.SizeBytes != int64(len(file.Content)) {
			return nil, nil, fmt.Errorf("plugin Skill companion file %q failed digest validation", relative)
		}
		pins = append(pins, pluginruntime.SkillFile{
			ArtifactFileID: util.UUIDToString(file.ID),
			Path:           relative,
			Digest:         file.Digest,
			SizeBytes:      file.SizeBytes,
		})
		bundleFiles = append(bundleFiles, skillbundle.File{Path: relative, Content: file.Content})
	}
	if !foundEntry {
		return nil, nil, fmt.Errorf("plugin Skill entry %q is missing from release artifacts", entryPath)
	}
	sort.Slice(pins, func(i, j int) bool { return pins[i].Path < pins[j].Path })
	sort.Slice(bundleFiles, func(i, j int) bool { return bundleFiles[i].Path < bundleFiles[j].Path })
	return pins, bundleFiles, nil
}

func materializePinnedPluginSkill(entry pluginruntime.CompiledEntry, filesByID map[string]db.PluginArtifactFile) (AgentSkillData, error) {
	root, err := pluginSkillRoot(entry.ContributionKey, entry.EntryPath)
	if err != nil {
		return AgentSkillData{}, fmt.Errorf("execution manifest contains invalid plugin Skill: %w", err)
	}
	releaseID, err := util.ParseUUID(entry.ReleaseID)
	if err != nil {
		return AgentSkillData{}, fmt.Errorf("execution manifest contains invalid release id")
	}
	entryFile, ok := filesByID[entry.ArtifactFileID]
	if !ok || entryFile.ReleaseID != releaseID || entryFile.Path != entry.EntryPath || entryFile.Digest != entry.EntryDigest || plugincontract.DigestBytes([]byte(entryFile.Content)) != entry.EntryDigest {
		return AgentSkillData{}, fmt.Errorf("pinned plugin Skill entry failed digest validation")
	}
	if entry.SkillFileCount != len(entry.SkillFiles) {
		return AgentSkillData{}, fmt.Errorf("pinned plugin Skill companion file count is invalid")
	}

	skill := AgentSkillData{
		ID:          "plugin:" + entry.ContributionID,
		Source:      skillbundle.SourcePlugin,
		Name:        entry.ContributionKey,
		Description: entry.Description,
		Content:     entryFile.Content,
		Files:       make([]AgentSkillFileData, 0, len(entry.SkillFiles)),
	}
	seenPaths := make(map[string]struct{}, len(entry.SkillFiles))
	seenIDs := make(map[string]struct{}, len(entry.SkillFiles))
	for _, pinned := range entry.SkillFiles {
		if err := validatePluginSkillRelativePath(pinned.Path); err != nil {
			return AgentSkillData{}, fmt.Errorf("execution manifest contains invalid plugin Skill companion file: %w", err)
		}
		if pinned.ArtifactFileID == "" || pinned.Digest == "" || pinned.SizeBytes < 0 {
			return AgentSkillData{}, fmt.Errorf("execution manifest contains incomplete plugin Skill companion file")
		}
		if _, exists := seenPaths[pinned.Path]; exists {
			return AgentSkillData{}, fmt.Errorf("execution manifest contains duplicate plugin Skill companion path %q", pinned.Path)
		}
		if _, exists := seenIDs[pinned.ArtifactFileID]; exists {
			return AgentSkillData{}, fmt.Errorf("execution manifest contains duplicate plugin Skill artifact file id")
		}
		seenPaths[pinned.Path] = struct{}{}
		seenIDs[pinned.ArtifactFileID] = struct{}{}

		file, ok := filesByID[pinned.ArtifactFileID]
		if !ok || file.ReleaseID != releaseID || file.Path != root+pinned.Path || file.Digest != pinned.Digest || file.SizeBytes != pinned.SizeBytes || plugincontract.DigestBytes([]byte(file.Content)) != pinned.Digest || int64(len(file.Content)) != pinned.SizeBytes {
			return AgentSkillData{}, fmt.Errorf("pinned plugin Skill companion file %q failed digest validation", pinned.Path)
		}
		skill.Files = append(skill.Files, AgentSkillFileData{
			Path:      pinned.Path,
			Content:   file.Content,
			SHA256:    pinned.Digest,
			SizeBytes: pinned.SizeBytes,
		})
	}
	return skill, nil
}

func pluginSkillRoot(contributionKey, entryPath string) (string, error) {
	expected := "skills/" + contributionKey + "/SKILL.md"
	if entryPath != expected {
		return "", fmt.Errorf("plugin Skill entry must be %q", expected)
	}
	return strings.TrimSuffix(expected, "SKILL.md"), nil
}

func validatePluginSkillRelativePath(relative string) error {
	firstSegment := relative
	if separator := strings.IndexByte(relative, '/'); separator >= 0 {
		firstSegment = relative[:separator]
	}
	if relative == "" || relative == "." || relative == ".." || strings.EqualFold(firstSegment, "SKILL.md") || strings.Contains(relative, "\\") || path.IsAbs(relative) || path.Clean(relative) != relative || strings.HasPrefix(relative, "../") {
		return fmt.Errorf("plugin Skill companion path %q is invalid", relative)
	}
	return nil
}
