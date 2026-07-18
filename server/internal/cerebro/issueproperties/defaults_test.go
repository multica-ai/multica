package issueproperties

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type fakePropertyCreator struct {
	params []db.CreateIssuePropertyParams
	errAt  int
}

func (fake *fakePropertyCreator) CreateIssueProperty(_ context.Context, params db.CreateIssuePropertyParams) (db.IssueProperty, error) {
	fake.params = append(fake.params, params)
	if fake.errAt > 0 && len(fake.params) == fake.errAt {
		return db.IssueProperty{}, errors.New("database unavailable")
	}
	return db.IssueProperty{}, nil
}

func TestSeedDefaultsCreatesBothDKKNumberProperties(t *testing.T) {
	workspaceID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	fake := &fakePropertyCreator{}
	if err := SeedDefaults(context.Background(), fake, workspaceID); err != nil {
		t.Fatalf("SeedDefaults() error = %v", err)
	}
	if len(fake.params) != 2 {
		t.Fatalf("created %d properties, want 2", len(fake.params))
	}
	wantNames := []string{"Business value (DKK)", "Effort (DKK)"}
	for index, params := range fake.params {
		if params.WorkspaceID != workspaceID || params.Name != wantNames[index] || params.Type != "number" || string(params.Config) != "{}" {
			t.Errorf("property %d = %#v", index, params)
		}
	}
}

func TestSeedDefaultsStopsOnFailure(t *testing.T) {
	fake := &fakePropertyCreator{errAt: 1}
	if err := SeedDefaults(context.Background(), fake, pgtype.UUID{}); err == nil {
		t.Fatal("SeedDefaults() error = nil, want failure")
	}
	if len(fake.params) != 1 {
		t.Fatalf("created %d properties after failure, want 1", len(fake.params))
	}
}
