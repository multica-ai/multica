package engine

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/service"
	dbfx "github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// A FAILED permission lookup is not a denial, and the difference is not
// cosmetic: a denial marks the message processed, so the platform's redelivery
// is thrown away and the sender is told they lack a permission they have.
//
// The router tests next door stub MemberMayInvokeAgent at the interface, so
// they can only prove the Router does the right thing with an error it is
// handed. What they cannot see is whether the real policy ever produces one —
// and for two of its three queries it did not, returning a plain false.
//
// So this drives the REAL service.TaskService against a real database, with
// one query failed at the SQL boundary.
func TestRouter_RealServiceLookupFailure_ReleasesInsteadOfDenying(t *testing.T) {
	pool := invokeGateTestDB(t)

	for _, tc := range []struct {
		name string
		// sql is the fragment identifying the query to fail. Each is a query
		// that used to swallow its error inside the policy.
		sql  string
		what string
	}{
		{
			name: "the agent's invocation targets cannot be listed",
			sql:  "FROM agent_invocation_target",
			what: "a public_to agent's target list",
		},
		{
			name: "the sender's workspace membership cannot be read",
			sql:  "FROM member",
			what: "whether the sender is a workspace member",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agentID, senderID := seedPublicToAgent(t, pool)

			h := newHarness(t)
			h.ident.id = ResolvedIdentity{UserID: senderID}
			h.inst.inst.AgentID = agentID
			// The real policy, reached through the real service, with one
			// query failing the way an unreachable database fails it.
			h.router = NewRouter(h.issues,
				&service.TaskService{Queries: db.New(failQuery{inner: pool, match: tc.sql})},
				h.reader, RouterConfig{Logger: discardLogger(), Lifecycle: h.lifecycle})
			// A fresh Router holds no resolver sets, and Handle answers that
			// with an error too — which would look exactly like the failure
			// this test is trying to observe.
			h.router.Register(channel.TypeFeishu, ResolverSet{
				Installation: h.inst, Identity: h.ident, Dedup: h.dedup,
				Session: h.binder, Audit: h.audit, Replier: h.replier,
				Typing: h.typing, Media: h.media, OriginType: "lark_chat",
			})

			err := h.router.Handle(context.Background(), p2pMessage(t))

			if err == nil {
				t.Fatalf("Handle returned nil when %s could not be read; the Router cannot tell "+
					"a failed lookup from a refusal unless the policy reports one", tc.what)
			}
			if h.dedup.marks() != 0 {
				t.Errorf("marked %d time(s): the message is now consumed, so the platform's redelivery "+
					"is discarded and this turn is lost for good", h.dedup.marks())
			}
			if h.dedup.releases() != 1 {
				t.Errorf("releases = %d, want 1", h.dedup.releases())
			}
			if r, _ := h.audit.last(); r == DropReasonInvokeDenied {
				t.Error("audited as invocation_not_allowed: a database that did not answer is not a verdict " +
					"about this member's permissions")
			}
			if waitFor(200*time.Millisecond, func() bool {
				for _, res := range h.replier.calls() {
					if res.Outcome == OutcomeInvokeDenied {
						return true
					}
				}
				return false
			}) {
				t.Error("the sender was told they may not run the agent, which may well be false — " +
					"nothing here established that")
			}
		})
	}
}

// failQuery fails one query and passes the rest through, which is what lets
// GetAgent succeed and the failure land inside the policy itself.
type failQuery struct {
	inner db.DBTX
	match string
}

var errInjected = errors.New("injected: database unreachable")

func (f failQuery) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if strings.Contains(sql, f.match) {
		return pgconn.CommandTag{}, errInjected
	}
	return f.inner.Exec(ctx, sql, args...)
}

func (f failQuery) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if strings.Contains(sql, f.match) {
		return nil, errInjected
	}
	return f.inner.Query(ctx, sql, args...)
}

func (f failQuery) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, f.match) {
		return errRow{}
	}
	return f.inner.QueryRow(ctx, sql, args...)
}

type errRow struct{}

func (errRow) Scan(...any) error { return errInjected }

// seedPublicToAgent builds the one shape where the two swallowed queries are
// reached at all: a public_to agent the sender does not own.
func seedPublicToAgent(t *testing.T, pool *pgxpool.Pool) (agentID, senderID pgtype.UUID) {
	t.Helper()
	tag := strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	f := dbfx.New(pool, "", "")
	f.WorkspaceID = f.Workspace(t, "invoke-gate "+tag, "invoke-gate-"+tag)
	f.UserID = f.User(t, "Owner "+tag, "owner-"+tag+"@example.com")
	sender := f.User(t, "Sender "+tag, "sender-"+tag+"@example.com")
	agent := f.Agent(t, "gated-agent-"+tag, "", dbfx.Cols{"permission_mode": "public_to"})
	return uuidFromString(t, agent), uuidFromString(t, sender)
}

func invokeGateTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("no database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database not reachable: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
