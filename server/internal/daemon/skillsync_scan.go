package daemon

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const skillDefinitionFilename = "SKILL.md"

// ScannedSkill represents one local skill directory and its deterministic manifest.
type ScannedSkill struct {
	Name    string
	Path    string
	Content string
	Files   []ScannedSkillFile
	Hash    string
}

// ScannedSkillFile is a text supporting file included in a scanned skill manifest.
type ScannedSkillFile struct {
	Path    string
	Content string
}

// ScanLocalSkills scans one local skills root for child directories containing SKILL.md.
func ScanLocalSkills(root string) ([]ScannedSkill, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read skills root %q: %w", root, err)
	}

	skills := make([]ScannedSkill, 0)
	for _, entry := range entries {
		if !entry.IsDir() || shouldIgnoreSkillDir(entry.Name()) {
			continue
		}

		skillDir := filepath.Join(root, entry.Name())
		skill, ok, err := scanSkillDir(skillDir, entry.Name())
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		skills = append(skills, skill)
	}

	sort.Slice(skills, func(i, j int) bool {
		return skills[i].Name < skills[j].Name
	})

	return skills, nil
}

func scanSkillDir(skillDir, skillName string) (ScannedSkill, bool, error) {
	skillPath := filepath.Join(skillDir, skillDefinitionFilename)
	content, ok, err := readTextFile(skillPath)
	if err != nil {
		return ScannedSkill{}, false, fmt.Errorf("scan skill %q: %w", skillName, err)
	}
	if !ok {
		return ScannedSkill{}, false, nil
	}

	files, err := collectSkillFiles(skillDir)
	if err != nil {
		return ScannedSkill{}, false, fmt.Errorf("scan skill %q: %w", skillName, err)
	}

	return ScannedSkill{
		Name:    skillName,
		Path:    skillDir,
		Content: content,
		Files:   files,
		Hash:    hashSkillManifest(skillName, content, files),
	}, true, nil
}

func collectSkillFiles(skillDir string) ([]ScannedSkillFile, error) {
	files := make([]ScannedSkillFile, 0)

	err := filepath.WalkDir(skillDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == skillDir {
			return nil
		}

		name := d.Name()
		if d.IsDir() {
			if shouldIgnoreNestedDir(name) {
				return filepath.SkipDir
			}
			return nil
		}

		if shouldIgnoreFile(name) {
			return nil
		}

		relPath, err := filepath.Rel(skillDir, path)
		if err != nil {
			return fmt.Errorf("relative path for %q: %w", path, err)
		}

		relPath = filepath.ToSlash(relPath)
		if relPath == skillDefinitionFilename {
			return nil
		}

		content, ok, err := readTextFile(path)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}

		files = append(files, ScannedSkillFile{
			Path:    relPath,
			Content: content,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})

	return files, nil
}

func readTextFile(path string) (string, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("read file %q: %w", path, err)
	}

	if !isTextFile(data) {
		return "", false, nil
	}

	return string(data), true, nil
}

func isTextFile(data []byte) bool {
	if bytes.IndexByte(data, 0) >= 0 {
		return false
	}
	if !utf8.Valid(data) {
		return false
	}

	if len(data) == 0 {
		return true
	}

	var suspicious int
	for _, r := range string(data) {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			continue
		case unicode.IsPrint(r):
			continue
		default:
			suspicious++
		}
	}

	return suspicious*20 <= len([]rune(string(data)))
}

func shouldIgnoreSkillDir(name string) bool {
	return shouldIgnoreNestedDir(name)
}

func shouldIgnoreNestedDir(name string) bool {
	switch {
	case strings.HasPrefix(name, "."):
		return true
	case name == "__pycache__":
		return true
	case name == "node_modules", name == "dist", name == "build", name == ".next", name == "coverage":
		return true
	default:
		return false
	}
}

func shouldIgnoreFile(name string) bool {
	return name == ".DS_Store"
}

func hashSkillManifest(name, content string, files []ScannedSkillFile) string {
	h := sha256.New()
	writeHashField(h, "name", name)
	writeHashField(h, "content", content)

	for _, file := range files {
		writeHashField(h, "file", file.Path)
		writeHashField(h, "content", file.Content)
	}

	return hex.EncodeToString(h.Sum(nil))
}

func writeHashField(h hash.Hash, key, value string) {
	_, _ = h.Write([]byte(key))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(value))
	_, _ = h.Write([]byte{0})
}
