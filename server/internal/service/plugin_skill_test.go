package service

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
	"github.com/multica-ai/multica/server/pkg/pluginruntime"
)

func pluginSkillArtifact(id, releaseID, path, content string) db.PluginArtifactFile {
	return db.PluginArtifactFile{
		ID:        util.MustParseUUID(id),
		ReleaseID: util.MustParseUUID(releaseID),
		Path:      path,
		Digest:    plugincontract.DigestBytes([]byte(content)),
		SizeBytes: int64(len(content)),
		Content:   content,
	}
}

func TestCompileAndMaterializePluginSkillCompanionFiles(t *testing.T) {
	const (
		releaseID = "11111111-1111-1111-1111-111111111111"
		entryID   = "22222222-2222-2222-2222-222222222222"
		agentID   = "33333333-3333-3333-3333-333333333333"
		rulesID   = "44444444-4444-4444-4444-444444444444"
	)
	entry := pluginSkillArtifact(entryID, releaseID, "skills/research/SKILL.md", "# Research")
	agent := pluginSkillArtifact(agentID, releaseID, "skills/research/agents/openai.yaml", "interface: {}")
	rules := pluginSkillArtifact(rulesID, releaseID, "skills/research/references/rules.md", "rules")
	other := pluginSkillArtifact("55555555-5555-5555-5555-555555555555", releaseID, "skills/other/SKILL.md", "other")
	license := pluginSkillArtifact("66666666-6666-6666-6666-666666666666", releaseID, "THIRD_PARTY_LICENSES/source.txt", "license")

	pins, bundleFiles, err := compilePluginSkillFiles(
		util.MustParseUUID(releaseID),
		"research",
		"skills/research/SKILL.md",
		[]db.PluginArtifactFile{rules, other, entry, license, agent},
	)
	if err != nil {
		t.Fatalf("compilePluginSkillFiles: %v", err)
	}
	if len(pins) != 2 || pins[0].Path != "agents/openai.yaml" || pins[1].Path != "references/rules.md" {
		t.Fatalf("compiled pins = %#v", pins)
	}
	if len(bundleFiles) != 2 || bundleFiles[0].Path != pins[0].Path || bundleFiles[1].Path != pins[1].Path {
		t.Fatalf("bundle files = %#v", bundleFiles)
	}

	compiled := pluginruntime.CompiledEntry{
		ReleaseID:        releaseID,
		ContributionID:   "77777777-7777-7777-7777-777777777777",
		ContributionKey:  "research",
		ArtifactFileID:   entryID,
		EntryPath:        entry.Path,
		EntryDigest:      entry.Digest,
		SkillFileCount:   len(pins),
		SkillFiles:       pins,
		Description:      "Research from primary sources",
		ContributionType: plugincontract.ContributionAgentSkillV1,
	}
	filesByID := map[string]db.PluginArtifactFile{
		entryID: entry,
		agentID: agent,
		rulesID: rules,
	}
	skill, err := materializePinnedPluginSkill(compiled, filesByID)
	if err != nil {
		t.Fatalf("materializePinnedPluginSkill: %v", err)
	}
	if skill.Content != entry.Content || len(skill.Files) != 2 || skill.Files[0].Path != "agents/openai.yaml" || skill.Files[1].Path != "references/rules.md" {
		t.Fatalf("materialized Skill = %#v", skill)
	}
}

func TestCompilePluginSkillFilesRejectsDigestDrift(t *testing.T) {
	const releaseID = "11111111-1111-1111-1111-111111111111"
	entry := pluginSkillArtifact("22222222-2222-2222-2222-222222222222", releaseID, "skills/research/SKILL.md", "# Research")
	companion := pluginSkillArtifact("33333333-3333-3333-3333-333333333333", releaseID, "skills/research/rules.md", "rules")
	companion.Content = "tampered"
	if _, _, err := compilePluginSkillFiles(util.MustParseUUID(releaseID), "research", entry.Path, []db.PluginArtifactFile{entry, companion}); err == nil {
		t.Fatal("digest drift was accepted")
	}
}

func TestMaterializePinnedPluginSkillRejectsPathEscape(t *testing.T) {
	entryFile := pluginSkillArtifact(
		"22222222-2222-2222-2222-222222222222",
		"11111111-1111-1111-1111-111111111111",
		"skills/research/SKILL.md",
		"# Research",
	)
	entry := pluginruntime.CompiledEntry{
		ReleaseID:       "11111111-1111-1111-1111-111111111111",
		ContributionKey: "research",
		ArtifactFileID:  "22222222-2222-2222-2222-222222222222",
		EntryPath:       "skills/research/SKILL.md",
		EntryDigest:     entryFile.Digest,
		SkillFileCount:  1,
		SkillFiles: []pluginruntime.SkillFile{{
			ArtifactFileID: "33333333-3333-3333-3333-333333333333",
			Path:           "../escape.md",
			Digest:         "sha256:invalid",
			SizeBytes:      1,
		}},
	}
	if _, err := materializePinnedPluginSkill(entry, map[string]db.PluginArtifactFile{entry.ArtifactFileID: entryFile}); err == nil {
		t.Fatal("path escape was accepted")
	}
}

func TestValidatePluginSkillRelativePathRejectsPrimaryContentDescendant(t *testing.T) {
	if err := validatePluginSkillRelativePath("SKILL.md/hidden.txt"); err == nil {
		t.Fatal("primary content descendant was accepted")
	}
	if err := validatePluginSkillRelativePath(".."); err == nil {
		t.Fatal("bare parent path was accepted")
	}
}
