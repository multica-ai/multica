package daemon

import (
	"context"
	"fmt"
)

type TaskSkillUsageEntry struct {
	ID    string `json:"id,omitempty"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func (c *Client) ReportTaskSkillUsage(ctx context.Context, taskID string, skills []TaskSkillUsageEntry) error {
	if len(skills) == 0 {
		return nil
	}
	return c.postJSON(ctx, fmt.Sprintf("/api/daemon/tasks/%s/skill-usage", taskID), map[string]any{
		"skills": skills,
	}, nil)
}
