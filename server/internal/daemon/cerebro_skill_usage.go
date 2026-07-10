package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/multica-ai/multica/server/internal/cerebro/traceupload"
)

var (
	skillPathPattern = regexp.MustCompile(`(?:^|[/\\])skills[/\\]([^/\\\s"']+)[/\\]SKILL\.md`)
	skillGetPattern  = regexp.MustCompile(`\bmultica\s+skill\s+get\s+([0-9a-fA-F-]{36})\b`)
)

func (d *Daemon) reportTaskSkillUsage(ctx context.Context, task Task, provider string, result TaskResult) {
	if provider != "claude" && provider != "codex" {
		return
	}
	path, err := traceupload.LocateJSONL(traceupload.LocateParams{
		Runtime: provider, SessionID: result.SessionID, EnvRoot: result.EnvRoot, WorkDir: result.WorkDir,
	})
	if err != nil {
		return
	}
	skills, err := extractSkillUsageFromTranscript(path)
	if err != nil || len(skills) == 0 {
		return
	}
	if err := d.client.ReportTaskSkillUsage(ctx, task.ID, skills); err != nil {
		d.logger.Warn("report task skill usage failed", "task", shortID(task.ID), "error", err)
	}
}

func extractSkillUsageFromTranscript(path string) ([]TaskSkillUsageEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	byKey := map[string]TaskSkillUsageEntry{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var event any
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			continue
		}
		for _, command := range commandStrings(event, "") {
			for _, match := range skillPathPattern.FindAllStringSubmatch(command, -1) {
				name := strings.TrimSpace(match[1])
				key := "name:" + name
				entry := byKey[key]
				entry.Name, entry.Count = name, entry.Count+1
				byKey[key] = entry
			}
			for _, match := range skillGetPattern.FindAllStringSubmatch(command, -1) {
				id := strings.ToLower(match[1])
				key := "id:" + id
				entry := byKey[key]
				entry.ID, entry.Name, entry.Count = id, id, entry.Count+1
				byKey[key] = entry
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	result := make([]TaskSkillUsageEntry, 0, len(byKey))
	for _, entry := range byKey {
		result = append(result, entry)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ID == "" && result[j].ID != "" {
			return true
		}
		if result[i].ID != "" && result[j].ID == "" {
			return false
		}
		return result[i].Name < result[j].Name
	})
	return result, nil
}

func commandStrings(value any, key string) []string {
	switch typed := value.(type) {
	case map[string]any:
		result := []string{}
		for childKey, child := range typed {
			result = append(result, commandStrings(child, childKey)...)
		}
		return result
	case []any:
		result := []string{}
		for _, child := range typed {
			result = append(result, commandStrings(child, key)...)
		}
		return result
	case string:
		if key == "cmd" || key == "command" {
			return []string{typed}
		}
	}
	return nil
}
