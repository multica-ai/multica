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

type dashboardEmptyRows struct{}

func (*dashboardEmptyRows) Close()                                       {}
func (*dashboardEmptyRows) Err() error                                   { return nil }
func (*dashboardEmptyRows) CommandTag() pgconn.CommandTag                { return pgconn.NewCommandTag("") }
func (*dashboardEmptyRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (*dashboardEmptyRows) Next() bool                                   { return false }
func (*dashboardEmptyRows) Scan(...any) error                            { return nil }
func (*dashboardEmptyRows) Values() ([]any, error)                       { return nil, nil }
func (*dashboardEmptyRows) RawValues() [][]byte                          { return nil }
func (*dashboardEmptyRows) Conn() *pgx.Conn                              { return nil }

type dashboardQueryCountingDB struct {
	queries int
}

func (*dashboardQueryCountingDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag(""), nil
}

func (d *dashboardQueryCountingDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	d.queries++
	return &dashboardEmptyRows{}, nil
}

func (*dashboardQueryCountingDB) QueryRow(context.Context, string, ...any) pgx.Row {
	return nil
}

func TestListDashboardUsageDailyUsesConfiguredReplica(t *testing.T) {
	primaryDB := &dashboardQueryCountingDB{}
	replicaDB := &dashboardQueryCountingDB{}
	primaryQueries := db.New(primaryDB)
	replicaQueries := db.New(replicaDB)
	h := &Handler{
		Queries:      primaryQueries,
		ReadSelector: dbreader.New(primaryQueries, replicaQueries, nil),
	}

	rows, err := h.listDashboardUsageDaily(
		context.Background(),
		pgtype.UUID{},
		"UTC",
		pgtype.Timestamptz{},
		pgtype.UUID{},
	)
	if err != nil {
		t.Fatalf("list dashboard usage daily: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows = %#v, want empty result", rows)
	}
	if primaryDB.queries != 0 || replicaDB.queries != 1 {
		t.Fatalf("query calls: primary=%d replica=%d, want primary=0 replica=1", primaryDB.queries, replicaDB.queries)
	}
}
