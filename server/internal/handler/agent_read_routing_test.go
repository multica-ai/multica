package handler

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/dbreader"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type agentListEmptyRows struct{}

func (*agentListEmptyRows) Close()                                       {}
func (*agentListEmptyRows) Err() error                                   { return nil }
func (*agentListEmptyRows) CommandTag() pgconn.CommandTag                { return pgconn.NewCommandTag("") }
func (*agentListEmptyRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (*agentListEmptyRows) Next() bool                                   { return false }
func (*agentListEmptyRows) Scan(...any) error                            { return nil }
func (*agentListEmptyRows) Values() ([]any, error)                       { return nil, nil }
func (*agentListEmptyRows) RawValues() [][]byte                          { return nil }
func (*agentListEmptyRows) Conn() *pgx.Conn                              { return nil }

type agentListQueryCountingDB struct {
	queries int
}

func (*agentListQueryCountingDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag(""), nil
}

func (d *agentListQueryCountingDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	d.queries++
	return &agentListEmptyRows{}, nil
}

func (*agentListQueryCountingDB) QueryRow(context.Context, string, ...any) pgx.Row {
	return nil
}

func TestListAgentSkillsByWorkspaceUsesConfiguredReplica(t *testing.T) {
	primaryDB := &agentListQueryCountingDB{}
	replicaDB := &agentListQueryCountingDB{}
	primaryQueries := db.New(primaryDB)
	replicaQueries := db.New(replicaDB)
	h := &Handler{
		Queries:      primaryQueries,
		ReadSelector: dbreader.New(primaryQueries, replicaQueries, nil),
	}

	rows, err := h.listAgentSkillsByWorkspace(context.Background(), pgtype.UUID{})
	if err != nil {
		t.Fatalf("list agent skills by workspace: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows = %#v, want empty result", rows)
	}
	if primaryDB.queries != 0 || replicaDB.queries != 1 {
		t.Fatalf("query calls: primary=%d replica=%d, want primary=0 replica=1", primaryDB.queries, replicaDB.queries)
	}
}
