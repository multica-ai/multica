package cerebro

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

// SkillMetadata holds structured fields extracted from a SKILL.md YAML frontmatter.
// Stored as JSONB in skill.metadata; synced on every create/update.
type SkillMetadata struct {
	Category       string       `json:"category,omitempty"`
	Domain         string       `json:"domain,omitempty"`
	Type           string       `json:"type,omitempty"`
	Scope          string       `json:"scope,omitempty"`
	Status         string       `json:"status,omitempty"`
	Tags           []string     `json:"tags,omitempty"`
	DataDomains    []DataDomain `json:"data_domains,omitempty"`
	TriggeredBy    []AutoLink   `json:"triggered_by,omitempty"`
	RequiresAccess []string     `json:"requires_access,omitempty"`
	AutoLearn      bool         `json:"auto_learn"`
}

// DataDomain describes a BigQuery project+datasets a skill accesses.
type DataDomain struct {
	Project  string   `json:"project"`
	Datasets []string `json:"datasets,omitempty"`
}

// AutoLink describes an automation that triggers a skill.
type AutoLink struct {
	Type     string `json:"type"`              // "autopilot" | "cron" | "webhook"
	ID       string `json:"id,omitempty"`      // autopilot UUID when known
	Name     string `json:"name"`
	Schedule string `json:"schedule,omitempty"`
}

// ParseSkillMetadata extracts structured metadata from SKILL.md YAML frontmatter.
// Returns an empty SkillMetadata (auto_learn: true default) when content has no
// frontmatter or frontmatter fields are absent.
func ParseSkillMetadata(content string) SkillMetadata {
	meta := SkillMetadata{AutoLearn: true}

	if !strings.HasPrefix(content, "---") {
		return meta
	}
	end := strings.Index(content[3:], "\n---")
	if end < 0 {
		return meta
	}
	frontmatter := content[3 : 3+end]

	// Simple line-by-line YAML parser for scalar fields.
	// For structured fields (tags, data_domains, triggered_by, requires_access)
	// we collect indented list lines after a key line.
	var (
		currentKey  string
		listLines   []string
		inListBlock bool
	)

	flush := func() {
		if currentKey == "" || !inListBlock {
			return
		}
		var items []string
		for _, l := range listLines {
			l = strings.TrimSpace(l)
			l = strings.TrimPrefix(l, "- ")
			l = strings.Trim(l, "\"'")
			if l != "" {
				items = append(items, l)
			}
		}
		switch currentKey {
		case "tags":
			meta.Tags = items
		case "requires_access":
			meta.RequiresAccess = items
		}
		currentKey = ""
		listLines = nil
		inListBlock = false
	}

	for _, line := range strings.Split(frontmatter, "\n") {
		// Indented list continuation.
		if inListBlock && (strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "\t")) {
			listLines = append(listLines, strings.TrimSpace(line))
			continue
		}
		flush()

		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		key, val, ok := strings.Cut(trimmed, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		val = strings.Trim(val, "\"'")

		switch key {
		case "category":
			meta.Category = val
		case "domain":
			meta.Domain = val
		case "type":
			meta.Type = val
		case "scope":
			meta.Scope = val
		case "status":
			meta.Status = val
		case "auto_learn":
			meta.AutoLearn = val != "false"
		case "tags", "requires_access":
			if val == "" {
				currentKey = key
				inListBlock = true
			}
		}
	}
	flush()

	return meta
}

// MarshalSkillMetadata returns the JSON bytes for the metadata JSONB column.
func MarshalSkillMetadata(meta SkillMetadata) ([]byte, error) {
	return json.Marshal(meta)
}

// SyncSkillMetadata parses frontmatter from content and writes the metadata
// column for the given skill ID. Called after every create/update.
func SyncSkillMetadata(ctx context.Context, q *cerebrodb.Queries, skillID pgtype.UUID, content string) error {
	meta := ParseSkillMetadata(content)
	raw, err := MarshalSkillMetadata(meta)
	if err != nil {
		return err
	}
	return q.UpdateSkillMetadata(ctx, cerebrodb.UpdateSkillMetadataParams{
		ID:       skillID,
		Metadata: raw,
	})
}
