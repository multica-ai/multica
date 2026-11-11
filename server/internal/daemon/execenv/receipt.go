package execenv

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	skillpkg "github.com/multica-ai/multica/server/internal/skill"
)

// Read-only views over what Prepare / InjectRuntimeConfig put on disk, for the
// per-run context receipt (MUL-9013). None of these create, write or record
// anything: they only describe where a source went so the receipt can name it.

// RuntimeConfigRelPath returns the workdir-relative path of the provider's
// runtime config file (CLAUDE.md, AGENTS.md, QWEN.md, ...), or "" when the
// provider has no file target and the brief can only travel inline.
func RuntimeConfigRelPath(workDir, provider string) string {
	path := runtimeConfigPath(workDir, provider)
	if path == "" {
		return ""
	}
	if rel, err := filepath.Rel(workDir, path); err == nil {
		return filepath.ToSlash(rel)
	}
	return filepath.Base(path)
}

// ExistingRuntimeConfigContent returns the part of the provider's runtime
// config file that Multica does not manage: what the repository (or the user)
// already had in CLAUDE.md / AGENTS.md before the brief was appended. Empty
// when the file is missing, holds only the managed block, or cannot be read.
func ExistingRuntimeConfigContent(workDir, provider string) string {
	path := runtimeConfigPath(workDir, provider)
	if path == "" {
		return ""
	}
	// A missing or unreadable file both mean "nothing pre-existing we can
	// report"; the receipt is diagnostic and must not fail the run.
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	content := string(data)
	start, end, ok := locateMarkerBlock(content)
	if !ok {
		return content
	}
	pre := strings.TrimSuffix(content[:start], runtimeManagedSeparator)
	return pre + content[end:]
}

// SkillDeliveryLocation names where this provider's skills are materialized,
// for display only. Workdir-relative for runtimes that discover skills from
// the project tree; a per-task home for the two that read them from there.
func SkillDeliveryLocation(workDir, provider string) string {
	switch provider {
	case "codex":
		return "$CODEX_HOME/skills"
	case "hermes":
		return "$HERMES_HOME/skills"
	case "qwenpaw":
		// QwenPaw receives this private directory through --workspace.
		return "<QwenPaw workspace>/skills"
	}
	dir := skillsDirPath(workDir, provider)
	if rel, err := filepath.Rel(workDir, dir); err == nil {
		return filepath.ToSlash(rel)
	}
	return dir
}

// SkillReceipt describes a complete materialized skill package. Available is
// false if any declared file could not be read; no partial fingerprint is used.
type SkillReceipt struct {
	Name      string
	Path      string
	Bytes     int
	SHA256    string
	Available bool
}

// SkillReceipts snapshots the actual prepared files, including frontmatter
// rewriting and collision-free names. It reads only files declared by the
// skill, not neighboring user-installed skills or runtime-generated files.
func (e *Environment) SkillReceipts(provider string, skills []SkillContextForEnv) []SkillReceipt {
	dir := skillsDirPath(e.WorkDir, provider)
	switch provider {
	case "codex":
		dir = filepath.Join(e.CodexHome, "skills")
	case "hermes":
		dir = filepath.Join(e.HermesHome, "skills")
	case "qwenpaw":
		dir = filepath.Join(e.QwenpawWorkspace, "skills")
	}
	receipts := make([]SkillReceipt, 0, len(skills))
	for _, skill := range skills {
		r := SkillReceipt{
			Name: skill.Name,
			Path: SkillDeliveryLocation(e.WorkDir, provider) + "/" + skill.Name + "/SKILL.md",
		}
		paths := map[string]struct{}{"SKILL.md": {}}
		for _, f := range skill.Files {
			if skillpkg.IsReservedContentPath(f.Path) {
				continue
			}
			paths[filepath.ToSlash(filepath.Clean(f.Path))] = struct{}{}
		}
		ordered := make([]string, 0, len(paths))
		for path := range paths {
			ordered = append(ordered, path)
		}
		sort.Strings(ordered)
		hash := sha256.New()
		available := true
		for _, path := range ordered {
			data, err := os.ReadFile(filepath.Join(dir, skill.Name, filepath.FromSlash(path)))
			if err != nil {
				available = false
				break
			}
			// Frame sorted relative paths and file lengths so changes to any
			// file (or filename) affect the package hash unambiguously.
			_, _ = io.WriteString(hash, path)
			_, _ = hash.Write([]byte{0})
			var length [8]byte
			binary.BigEndian.PutUint64(length[:], uint64(len(data)))
			_, _ = hash.Write(length[:])
			_, _ = hash.Write(data)
			r.Bytes += len(data)
		}
		if available {
			r.Available = true
			r.SHA256 = hex.EncodeToString(hash.Sum(nil))[:12]
		} else {
			r.Bytes = 0
		}
		receipts = append(receipts, r)
	}
	return receipts
}
