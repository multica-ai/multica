package issueproperties

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type propertyCreator interface {
	CreateIssueProperty(context.Context, db.CreateIssuePropertyParams) (db.IssueProperty, error)
}

var defaultProperties = []struct {
	name        string
	description string
}{
	{name: "Business value (DKK)", description: "Expected business value in Danish kroner."},
	{name: "Effort (DKK)", description: "Expected delivery effort in Danish kroner."},
}

// SeedDefaults creates the commercial fields every new Cerebro workspace needs.
func SeedDefaults(ctx context.Context, queries propertyCreator, workspaceID pgtype.UUID) error {
	for _, property := range defaultProperties {
		if _, err := queries.CreateIssueProperty(ctx, db.CreateIssuePropertyParams{
			WorkspaceID: workspaceID,
			Name:        property.name,
			Type:        "number",
			Description: property.description,
			Icon:        "",
			Config:      []byte("{}"),
		}); err != nil {
			return fmt.Errorf("create default property %q: %w", property.name, err)
		}
	}
	return nil
}
