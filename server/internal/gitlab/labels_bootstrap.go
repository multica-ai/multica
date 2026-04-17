package gitlab

import (
	"context"
	"fmt"

	gitlabapi "github.com/multica-ai/multica/server/pkg/gitlab"
)

type canonicalLabel struct {
	Name  string
	Color string
}

var canonicalScopedLabels = []canonicalLabel{
	{"status::backlog", "#9b9b9b"},
	{"status::todo", "#cccccc"},
	{"status::in_progress", "#3b82f6"},
	{"status::in_review", "#8b5cf6"},
	{"status::done", "#10b981"},
	{"status::blocked", "#ef4444"},
	{"status::cancelled", "#6b7280"},
	{"priority::urgent", "#dc2626"},
	{"priority::high", "#f97316"},
	{"priority::medium", "#eab308"},
	{"priority::low", "#84cc16"},
	{"priority::none", "#9ca3af"},
}

// CanonicalScopedLabelNames returns just the names — exposed for tests.
func CanonicalScopedLabelNames() []string {
	out := make([]string, len(canonicalScopedLabels))
	for i, l := range canonicalScopedLabels {
		out[i] = l.Name
	}
	return out
}

// BootstrapScopedLabels ensures every canonical Multica scoped label exists
// in the project. Existing labels (including ones with different colors)
// are left untouched.
func BootstrapScopedLabels(ctx context.Context, c *gitlabapi.Client, token string, projectID int64) error {
	existing, err := c.ListLabels(ctx, token, projectID)
	if err != nil {
		return fmt.Errorf("bootstrap: list labels: %w", err)
	}
	have := make(map[string]bool, len(existing))
	for _, l := range existing {
		have[l.Name] = true
	}
	for _, l := range canonicalScopedLabels {
		if have[l.Name] {
			continue
		}
		if _, err := c.CreateLabel(ctx, token, projectID, gitlabapi.CreateLabelInput{
			Name:        l.Name,
			Color:       l.Color,
			Description: "Managed by Multica",
		}); err != nil {
			return fmt.Errorf("bootstrap: create label %q: %w", l.Name, err)
		}
	}
	return nil
}
