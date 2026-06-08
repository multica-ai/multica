package cerebro

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

// SyncAutomationLinks parses triggered_by from the skill's metadata JSONB and
// upserts rows into skill_automation_link. Replaces all existing links for the
// skill on each call so stale entries are removed when a skill removes a trigger.
func SyncAutomationLinks(ctx context.Context, q *cerebrodb.Queries, skillID pgtype.UUID, metadataRaw []byte) error {
	if len(metadataRaw) == 0 {
		return nil
	}

	var meta SkillMetadata
	if err := json.Unmarshal(metadataRaw, &meta); err != nil {
		return nil // malformed metadata — skip silently
	}

	if err := q.DeleteSkillAutomationLinks(ctx, skillID); err != nil {
		return err
	}

	for _, link := range meta.TriggeredBy {
		if link.Name == "" {
			continue
		}
		params := cerebrodb.UpsertSkillAutomationLinkParams{
			SkillID:        skillID,
			AutomationType: link.Type,
			AutomationName: link.Name,
			Schedule:       pgtype.Text{String: link.Schedule, Valid: link.Schedule != ""},
		}
		if link.ID != "" {
			if uuid, err := parseSkillUUID(link.ID); err == nil {
				params.AutomationID = uuid
			}
		}
		if err := q.UpsertSkillAutomationLink(ctx, params); err != nil {
			return err
		}
	}
	return nil
}

// SkillImpact holds the full impact picture for a skill.
type SkillImpact struct {
	SkillID        string           `json:"skill_id"`
	SkillName      string           `json:"skill_name"`
	AutomationLinks []AutoLinkView  `json:"automation_links"`
	AutomationCount int             `json:"automation_count"`
}

// AutoLinkView is the API-facing shape of a skill_automation_link row.
type AutoLinkView struct {
	Type     string `json:"type"`
	ID       string `json:"id,omitempty"`
	Name     string `json:"name"`
	Schedule string `json:"schedule,omitempty"`
}

// GetSkillImpact returns the impact analysis for a skill.
func GetSkillImpact(ctx context.Context, q *cerebrodb.Queries, skillID pgtype.UUID) (SkillImpact, error) {
	rows, err := q.ListSkillAutomationLinks(ctx, skillID)
	if err != nil {
		return SkillImpact{}, err
	}

	links := make([]AutoLinkView, 0, len(rows))
	for _, r := range rows {
		v := AutoLinkView{
			Type: r.AutomationType,
			Name: r.AutomationName,
		}
		if r.AutomationID.Valid {
			v.ID = uuidToStr(r.AutomationID)
		}
		if r.Schedule.Valid {
			v.Schedule = r.Schedule.String
		}
		links = append(links, v)
	}

	return SkillImpact{
		SkillID:         uuidToStr(skillID),
		AutomationLinks: links,
		AutomationCount: len(links),
	}, nil
}

// helpers to avoid importing handler-layer utils
func uuidToStr(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	b := id.Bytes
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func parseSkillUUID(s string) (pgtype.UUID, error) {
	var u pgtype.UUID
	err := u.Scan(s)
	return u, err
}
