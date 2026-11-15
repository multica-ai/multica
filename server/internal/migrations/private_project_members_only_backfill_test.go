package migrations

import (
	"context"
	"os"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPrivateProjectMembersOnlyBackfill pins what migration 660 does to the
// private projects that exist when the role-based rule is removed: member
// lists stay exactly as they are, so an owner or admin who relied on their
// role is not added, and every project that has a member is led by one.
func TestPrivateProjectMembersOnlyBackfill(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("integration test requires Postgres at DATABASE_URL")
	}
	ctx := context.Background()
	const schema = "private_project_members_only_backfill_test"
	cfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		t.Fatalf("parse DATABASE_URL: %v", err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect to Postgres: %v", err)
	}
	cleanup := func() {
		_, _ = pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
	}
	cleanup()
	t.Cleanup(func() {
		cleanup()
		pool.Close()
	})

	const (
		ws      = "00000000-0000-0000-0000-0000000b0001"
		otherWS = "00000000-0000-0000-0000-0000000b0002"

		owner    = "00000000-0000-0000-0000-0000000b0011"
		admin    = "00000000-0000-0000-0000-0000000b0012"
		regular  = "00000000-0000-0000-0000-0000000b0013"
		veteran  = "00000000-0000-0000-0000-0000000b0014"
		outsider = "00000000-0000-0000-0000-0000000b0015" // owner of another workspace
		departed = "00000000-0000-0000-0000-0000000b0016" // no member row any more
		agent    = "00000000-0000-0000-0000-0000000b0021"

		// Created by a regular member, no lead.
		createdByMember = "00000000-0000-0000-0000-0000000b0101"
		// Led by an agent; its creator is not a project member.
		agentLed = "00000000-0000-0000-0000-0000000b0102"
		// Already led by a member of the project.
		alreadyLed = "00000000-0000-0000-0000-0000000b0103"
		// Its lead left the workspace and it has no member rows at all.
		abandoned = "00000000-0000-0000-0000-0000000b0104"
		// Led by an admin who saw it through their role and is not a member.
		roleLed = "00000000-0000-0000-0000-0000000b0106"
		// Visible to the workspace: must not be touched.
		open = "00000000-0000-0000-0000-0000000b0105"
	)

	if _, err := pool.Exec(ctx, `
		CREATE SCHEMA `+schema+`;
		CREATE TABLE member (
			id UUID NOT NULL DEFAULT gen_random_uuid(),
			workspace_id UUID NOT NULL,
			user_id UUID NOT NULL,
			role TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		CREATE TABLE project (
			id UUID PRIMARY KEY,
			workspace_id UUID NOT NULL,
			lead_type TEXT,
			lead_id UUID,
			creator_type TEXT,
			creator_id UUID,
			visibility TEXT NOT NULL DEFAULT 'workspace',
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		CREATE TABLE project_member (
			workspace_id UUID NOT NULL,
			project_id   UUID NOT NULL,
			user_id      UUID NOT NULL,
			added_by     UUID,
			created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY (project_id, user_id)
		);
	`); err != nil {
		t.Fatalf("create sandbox tables: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role, created_at) VALUES
			($1, $3, 'owner',  now() - interval '9 days'),
			($1, $4, 'admin',  now() - interval '8 days'),
			($1, $5, 'member', now() - interval '7 days'),
			($1, $6, 'member', now() - interval '6 days'),
			($2, $7, 'owner',  now() - interval '9 days')
	`, ws, otherWS, owner, admin, regular, veteran, outsider); err != nil {
		t.Fatalf("seed members: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO project (id, workspace_id, lead_type, lead_id, creator_type, creator_id, visibility) VALUES
			($1, $6, NULL,     NULL, 'member', $7,  'private'),
			($2, $6, 'agent',  $8,   'member', $9,  'private'),
			($3, $6, 'member', $10,  'member', $7,  'private'),
			($4, $6, 'member', $11,  'member', $11, 'private'),
			($5, $6, NULL,     NULL, 'member', $7,  'workspace'),
			($12, $6, 'member', $13, 'member', $7,  'private')
	`, createdByMember, agentLed, alreadyLed, abandoned, open, ws, regular, agent, owner, veteran, departed, roleLed, admin); err != nil {
		t.Fatalf("seed projects: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO project_member (workspace_id, project_id, user_id, created_at) VALUES
			($1, $2, $5, now() - interval '3 days'),
			($1, $2, $6, now() - interval '5 days'),
			($1, $3, $5, now() - interval '2 days'),
			($1, $3, $6, now() - interval '4 days'),
			($1, $4, $5, now() - interval '4 days'),
			($1, $4, $6, now() - interval '2 days'),
			($1, $7, $5, now() - interval '1 day')
	`, ws, createdByMember, agentLed, alreadyLed, regular, veteran, roleLed); err != nil {
		t.Fatalf("seed project members: %v", err)
	}

	applyMigrationFile(t, ctx, pool, "660_private_project_members_only.up.sql")
	// Idempotent: a second run changes nothing the assertions below can see.
	applyMigrationFile(t, ctx, pool, "660_private_project_members_only.up.sql")

	members := func(project string) []string {
		t.Helper()
		rows, err := pool.Query(ctx, `SELECT user_id::text FROM project_member WHERE project_id = $1 ORDER BY 1`, project)
		if err != nil {
			t.Fatalf("list members of %s: %v", project, err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				t.Fatalf("scan member: %v", err)
			}
			out = append(out, id)
		}
		return out
	}
	lead := func(project string) (string, string) {
		t.Helper()
		var leadType, leadID *string
		if err := pool.QueryRow(ctx, `SELECT lead_type, lead_id::text FROM project WHERE id = $1`, project).Scan(&leadType, &leadID); err != nil {
			t.Fatalf("read lead of %s: %v", project, err)
		}
		deref := func(s *string) string {
			if s == nil {
				return ""
			}
			return *s
		}
		return deref(leadType), deref(leadID)
	}

	cases := []struct {
		name        string
		project     string
		wantMembers []string
		wantLead    string
	}{
		// The creator leads, though the veteran joined the project earlier.
		{"created by a member", createdByMember, []string{regular, veteran}, regular},
		// Its creator is the workspace owner, who is not on the list and is
		// not put on it: the longest-standing member leads.
		{"agent lead replaced", agentLed, []string{regular, veteran}, veteran},
		{"human lead kept", alreadyLed, []string{regular, veteran}, veteran},
		// Nobody to hand it to. It stays as it was until an owner takes it over.
		{"abandoned", abandoned, nil, departed},
		// The admin is not added, so the lead passes to a member.
		{"lead outside the member list", roleLed, []string{regular}, regular},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := members(tc.project); !slices.Equal(got, tc.wantMembers) {
				t.Errorf("members = %v, want %v", got, tc.wantMembers)
			}
			if leadType, leadID := lead(tc.project); leadType != "member" || leadID != tc.wantLead {
				t.Errorf("lead = %s %s, want member %s", leadType, leadID, tc.wantLead)
			}
		})
	}

	t.Run("workspace-visible project untouched", func(t *testing.T) {
		if got := members(open); len(got) != 0 {
			t.Errorf("members = %v, want none", got)
		}
		if leadType, leadID := lead(open); leadType != "" || leadID != "" {
			t.Errorf("lead = %q %q, want none", leadType, leadID)
		}
	})

	t.Run("no owner or admin is added to a member list", func(t *testing.T) {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM project_member WHERE user_id = ANY($1::uuid[])`, []string{owner, admin, outsider}).Scan(&n); err != nil {
			t.Fatalf("count role-holder rows: %v", err)
		}
		if n != 0 {
			t.Errorf("owners and admins hold %d project memberships, want 0", n)
		}
	})

	// The down migration is a documented no-op and must stay runnable.
	applyMigrationFile(t, ctx, pool, "660_private_project_members_only.down.sql")
}
