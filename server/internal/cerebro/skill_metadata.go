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
	Type     string `json:"type"`         // "autopilot" | "cron" | "webhook"
	ID       string `json:"id,omitempty"` // autopilot UUID when known
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
	// auto_learn is a runtime toggle (see SetSkillAutoLearn / the `multica skill
	// auto-learn` command). When the SKILL.md frontmatter does not pin it
	// explicitly, preserve whatever value was last stored so a toggle survives
	// later, unrelated content edits instead of snapping back to the default.
	if !ContentHasExplicitAutoLearn(content) {
		if prev, err := q.GetSkillMetadata(ctx, skillID); err == nil && len(prev) > 0 {
			var prevMeta SkillMetadata
			if json.Unmarshal(prev, &prevMeta) == nil {
				meta.AutoLearn = prevMeta.AutoLearn
			}
		}
	}
	raw, err := MarshalSkillMetadata(meta)
	if err != nil {
		return err
	}
	return q.UpdateSkillMetadata(ctx, cerebrodb.UpdateSkillMetadataParams{
		ID:       skillID,
		Metadata: raw,
	})
}

// ContentHasExplicitAutoLearn reports whether the SKILL.md frontmatter pins an
// auto_learn value explicitly (as opposed to relying on the default).
func ContentHasExplicitAutoLearn(content string) bool {
	if !strings.HasPrefix(content, "---") {
		return false
	}
	end := strings.Index(content[3:], "\n---")
	if end < 0 {
		return false
	}
	for _, line := range strings.Split(content[3:3+end], "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, _, ok := strings.Cut(trimmed, ":")
		if ok && strings.TrimSpace(key) == "auto_learn" {
			return true
		}
	}
	return false
}

// SetSkillAutoLearn flips the auto_learn switch on a skill, preserving every
// other metadata field. It re-derives the structured fields from the skill's
// current content (so they are never lost) and overrides only auto_learn, then
// persists the metadata JSONB. Returns the updated metadata.
func SetSkillAutoLearn(ctx context.Context, q *cerebrodb.Queries, skillID pgtype.UUID, content string, enabled bool) (SkillMetadata, error) {
	meta := ParseSkillMetadata(content)
	meta.AutoLearn = enabled
	raw, err := MarshalSkillMetadata(meta)
	if err != nil {
		return meta, err
	}
	if err := q.UpdateSkillMetadata(ctx, cerebrodb.UpdateSkillMetadataParams{
		ID:       skillID,
		Metadata: raw,
	}); err != nil {
		return meta, err
	}
	return meta, nil
}

// IsAutoLearnEnabled reports whether the stored metadata has auto_learn on.
// Absent/empty metadata defaults to enabled so skills that have never been
// synced keep learning until they are explicitly switched off.
func IsAutoLearnEnabled(raw []byte) bool {
	if len(raw) == 0 {
		return true
	}
	var meta SkillMetadata
	if json.Unmarshal(raw, &meta) != nil {
		return true
	}
	return meta.AutoLearn
}
