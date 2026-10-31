package attribution

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestExecutionIdentity(t *testing.T) {
	id := func(n byte) pgtype.UUID { return pgtype.UUID{Bytes: [16]byte{n}, Valid: true} }
	user := id(9)
	root := func(source string) db.AgentTaskQueue {
		return db.AgentTaskQueue{ID: id(1), OriginatorSource: pgtype.Text{String: source, Valid: true}, OriginatorUserID: user}
	}
	for _, tc := range []struct {
		name string
		edit func(*db.AgentTaskQueue, *db.AgentTaskQueue)
		kind string
	}{
		{"human", func(c, p *db.AgentTaskQueue) {}, "human"},
		{"automation with accountable human", func(c, p *db.AgentTaskQueue) { c.OriginatorSource.String = "trigger_owner" }, "automation"},
		{"legacy automation", func(c, p *db.AgentTaskQueue) {
			c.OriginatorSource.String = "rule_owner"
			c.OriginatorUserID = pgtype.UUID{}
		}, "automation"},
		{"delegated human", func(c, p *db.AgentTaskQueue) { c.OriginatorSource.String = "delegation"; c.DelegatedFromTaskID = p.ID }, "human"},
		{"delegated automation", func(c, p *db.AgentTaskQueue) {
			c.OriginatorSource.String = "comment_source"
			c.DelegatedFromTaskID = p.ID
			p.OriginatorSource.String = "trigger_owner"
		}, "automation"},
		{"retry", func(c, p *db.AgentTaskQueue) {
			c.RetryOfTaskID = p.ID
			p.OriginatorSource.String = "trigger_owner"
			c.OriginatorSource = p.OriginatorSource
		}, "automation"},
		{"manual rerun is new human", func(c, p *db.AgentTaskQueue) { c.RerunOfTaskID = p.ID; p.OriginatorSource.String = "trigger_owner" }, "human"},
		{"missing human", func(c, p *db.AgentTaskQueue) { c.OriginatorUserID = pgtype.UUID{} }, ""},
		{"missing parent", func(c, p *db.AgentTaskQueue) {
			c.OriginatorSource.String = "delegation"
			c.DelegatedFromTaskID = id(99)
		}, ""},
		{"cycle", func(c, p *db.AgentTaskQueue) { c.OriginatorSource.String = "delegation"; c.DelegatedFromTaskID = c.ID }, ""},
		{"changed originator", func(c, p *db.AgentTaskQueue) {
			c.OriginatorSource.String = "delegation"
			c.DelegatedFromTaskID = p.ID
			p.OriginatorUserID = id(10)
		}, ""},
		{"owner fallback", func(c, p *db.AgentTaskQueue) { c.OriginatorSource.String = "owner_fallback" }, ""},
		{"backfill", func(c, p *db.AgentTaskQueue) { c.OriginatorSource.String = "backfill" }, ""},
		{"unattributed", func(c, p *db.AgentTaskQueue) { c.OriginatorSource.String = "unattributed" }, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current, parent := root("direct_human"), root("direct_human")
			parent.ID = id(2)
			tc.edit(&current, &parent)
			got, err := ResolveExecutionIdentity(current, func(key pgtype.UUID) (db.AgentTaskQueue, error) {
				for _, task := range []db.AgentTaskQueue{current, parent} {
					if key == task.ID {
						return task, nil
					}
				}
				return db.AgentTaskQueue{}, errors.New("not found in workspace")
			})
			if tc.kind == "" {
				if err == nil {
					t.Fatal("unproven identity accepted")
				}
				return
			}
			if err != nil || got.Kind != tc.kind {
				t.Fatalf("got %+v, %v", got, err)
			}
			if tc.kind == "human" && got.UserID != user {
				t.Fatal("human binding lost")
			}
			if tc.kind == "automation" && got.UserID.Valid {
				t.Fatal("automation exposed a human identity")
			}
		})
	}
}

func TestExecutionIdentityBoundsLineage(t *testing.T) {
	task := db.AgentTaskQueue{ID: uid(1), OriginatorUserID: uid(99), OriginatorSource: pgtype.Text{String: "delegation", Valid: true}, DelegatedFromTaskID: uid(2)}
	calls := 0
	_, err := ResolveExecutionIdentity(task, func(id pgtype.UUID) (db.AgentTaskQueue, error) {
		calls++
		task.ID = id
		task.DelegatedFromTaskID = uid(byte(calls + 2))
		return task, nil
	})
	if err == nil || calls > 32 {
		t.Fatalf("unbounded or accepted lineage: calls=%d, error=%v", calls, err)
	}
}
