package recall

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func BuildIndex(ctx context.Context, options IndexOptions) (Index, error) {
	if strings.TrimSpace(options.VaultRoot) == "" {
		return Index{}, fmt.Errorf("vault root is required")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	notesRoot := filepath.Join(options.VaultRoot, "notes")
	entries := make([]Entry, 0)
	err := filepath.WalkDir(notesRoot, func(filePath string, item fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if item.IsDir() {
			return nil
		}
		relativePath, err := filepath.Rel(options.VaultRoot, filePath)
		if err != nil {
			return err
		}
		relativePath = filepath.ToSlash(relativePath)
		candidate := Entry{Path: relativePath, FolderClass: "notes"}
		if !isAllowedEntry(candidate) {
			return nil
		}
		info, err := item.Info()
		if err != nil {
			return err
		}
		if info.Size() == 0 {
			return nil
		}
		entry, err := indexEntry(filePath, relativePath, info)
		if err != nil {
			return err
		}
		entries = append(entries, entry)
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return Index{}, fmt.Errorf("walk notes: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	index := Index{
		IndexVersion: CurrentIndexVersion,
		GeneratedAt:  options.Now().UTC().Format(time.RFC3339),
		VaultCommit:  readVaultCommit(options.VaultRoot),
		EntryCount:   len(entries),
		Entries:      entries,
	}
	if err := writeIndexAtomically(options.VaultRoot, index); err != nil {
		return Index{}, err
	}
	return index, nil
}

func indexEntry(filePath, relativePath string, info fs.FileInfo) (Entry, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return Entry{}, err
	}
	content := string(data)
	frontmatter := parseFrontmatter(content)
	title := firstHeading(content)
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
	}
	summary := strings.TrimSpace(frontmatter["description"])
	if summary == "" {
		summary = firstBodyLine(content)
	}
	if len([]rune(summary)) > 240 {
		summary = string([]rune(summary)[:240])
	}
	return Entry{
		Path: relativePath, Title: title, Tags: parseTopics(content),
		MTime: info.ModTime().UTC().Format(time.RFC3339), Summary: summary,
		FolderClass: "notes", SizeBytes: info.Size(),
	}, nil
}

func parseFrontmatter(content string) map[string]string {
	fields := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(content))
	if !scanner.Scan() || strings.TrimSpace(scanner.Text()) != "---" {
		return fields
	}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "---" {
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if ok {
			fields[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"'`)
		}
	}
	return fields
}

func firstHeading(content string) string {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return ""
}

func firstBodyLine(content string) string {
	inFrontmatter := false
	for index, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if index == 0 && trimmed == "---" {
			inFrontmatter = true
			continue
		}
		if inFrontmatter {
			if trimmed == "---" {
				inFrontmatter = false
			}
			continue
		}
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") && trimmed != "---" && trimmed != "Topics:" {
			return trimmed
		}
	}
	return ""
}

func parseTopics(content string) []string {
	topics := make([]string, 0)
	inTopics := false
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "Topics:" {
			inTopics = true
			continue
		}
		if !inTopics {
			continue
		}
		if trimmed == "" || !strings.HasPrefix(trimmed, "-") {
			if trimmed != "" {
				break
			}
			continue
		}
		start := strings.Index(trimmed, "[[")
		end := strings.Index(trimmed, "]]")
		if start >= 0 && end > start+2 {
			topics = append(topics, strings.TrimSpace(trimmed[start+2:end]))
		}
	}
	sort.Strings(topics)
	return topics
}

func writeIndexAtomically(vaultRoot string, index Index) error {
	opsDir := filepath.Join(vaultRoot, "ops")
	if err := os.MkdirAll(opsDir, 0o755); err != nil {
		return fmt.Errorf("create ops directory: %w", err)
	}
	temporary, err := os.CreateTemp(opsDir, ".recall-index-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary index: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	encoder := json.NewEncoder(temporary)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(index); err != nil {
		temporary.Close()
		return fmt.Errorf("encode index: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync index: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close index: %w", err)
	}
	if err := os.Rename(temporaryPath, filepath.Join(opsDir, "recall-index.json")); err != nil {
		return fmt.Errorf("replace index: %w", err)
	}
	return nil
}

func readVaultCommit(vaultRoot string) string {
	gitPath := filepath.Join(vaultRoot, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return ""
	}
	gitDir := gitPath
	if !info.IsDir() {
		data, err := os.ReadFile(gitPath)
		if err != nil {
			return ""
		}
		value := strings.TrimSpace(string(data))
		if !strings.HasPrefix(value, "gitdir: ") {
			return ""
		}
		gitDir = strings.TrimSpace(strings.TrimPrefix(value, "gitdir: "))
		if !filepath.IsAbs(gitDir) {
			gitDir = filepath.Join(vaultRoot, gitDir)
		}
	}
	head, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return ""
	}
	value := strings.TrimSpace(string(head))
	if !strings.HasPrefix(value, "ref: ") {
		return value
	}
	reference := strings.TrimSpace(strings.TrimPrefix(value, "ref: "))
	data, err := os.ReadFile(filepath.Join(gitDir, filepath.FromSlash(reference)))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
