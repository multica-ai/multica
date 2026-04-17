package gitlab

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// fakeResolverQueries lets us simulate "found" / "not found" for both
// connection types without wiring a real DB.
type fakeResolverQueries struct {
	workspaceConn *db.WorkspaceGitlabConnection
	userConn      *db.UserGitlabConnection
}

func (f *fakeResolverQueries) GetWorkspaceGitlabConnection(_ context.Context, _ pgtype.UUID) (db.WorkspaceGitlabConnection, error) {
	if f.workspaceConn == nil {
		return db.WorkspaceGitlabConnection{}, pgx.ErrNoRows
	}
	return *f.workspaceConn, nil
}

func (f *fakeResolverQueries) GetUserGitlabConnection(_ context.Context, _ db.GetUserGitlabConnectionParams) (db.UserGitlabConnection, error) {
	if f.userConn == nil {
		return db.UserGitlabConnection{}, pgx.ErrNoRows
	}
	return *f.userConn, nil
}

// stubDecrypt returns the plaintext "{prefix}|{hex}" so tests can assert
// which encrypted column we resolved against.
func stubDecrypt(prefix string) TokenDecrypter {
	return func(_ context.Context, encrypted []byte) (string, error) {
		return prefix + "|" + string(encrypted), nil
	}
}

// validUUID returns a syntactically valid UUID for tests.
const validUUID = "00000000-0000-0000-0000-000000000001"

func TestResolveTokenForWrite_HumanWithPATPicksUserPAT(t *testing.T) {
	q := &fakeResolverQueries{
		workspaceConn: &db.WorkspaceGitlabConnection{
			ServiceTokenEncrypted: []byte("svc"),
		},
		userConn: &db.UserGitlabConnection{
			PatEncrypted: []byte("usr"),
			GitlabUserID: 100,
		},
	}
	r := NewResolver(q, stubDecrypt("dec"))
	tok, src, err := r.ResolveTokenForWrite(context.Background(), validUUID, "member", validUUID)
	if err != nil {
		t.Fatalf("ResolveTokenForWrite: %v", err)
	}
	if src != "user" {
		t.Errorf("source = %q, want user", src)
	}
	if tok != "dec|usr" {
		t.Errorf("token = %q, want dec|usr (the decrypted user PAT)", tok)
	}
}

func TestResolveTokenForWrite_HumanWithoutPATFallsBackToServicePAT(t *testing.T) {
	q := &fakeResolverQueries{
		workspaceConn: &db.WorkspaceGitlabConnection{
			ServiceTokenEncrypted: []byte("svc"),
		},
		userConn: nil, // user hasn't connected
	}
	r := NewResolver(q, stubDecrypt("dec"))
	tok, src, err := r.ResolveTokenForWrite(context.Background(), validUUID, "member", validUUID)
	if err != nil {
		t.Fatalf("ResolveTokenForWrite: %v", err)
	}
	if src != "service" {
		t.Errorf("source = %q, want service", src)
	}
	if tok != "dec|svc" {
		t.Errorf("token = %q, want dec|svc", tok)
	}
}

func TestResolveTokenForWrite_AgentAlwaysUsesServicePAT(t *testing.T) {
	// Even if a "user_gitlab_connection" row somehow exists for the agent UUID,
	// the resolver MUST ignore it and pick the service PAT. Agents don't have
	// GitLab identities.
	q := &fakeResolverQueries{
		workspaceConn: &db.WorkspaceGitlabConnection{ServiceTokenEncrypted: []byte("svc")},
		userConn: &db.UserGitlabConnection{
			PatEncrypted: []byte("usr"),
			GitlabUserID: 100,
		},
	}
	r := NewResolver(q, stubDecrypt("dec"))
	tok, src, err := r.ResolveTokenForWrite(context.Background(), validUUID, "agent", validUUID)
	if err != nil {
		t.Fatalf("ResolveTokenForWrite: %v", err)
	}
	if src != "service" {
		t.Errorf("source = %q, want service", src)
	}
	if tok != "dec|svc" {
		t.Errorf("token = %q, want dec|svc (service PAT)", tok)
	}
}

func TestResolveTokenForWrite_NoWorkspaceConnection(t *testing.T) {
	q := &fakeResolverQueries{} // both nil
	r := NewResolver(q, stubDecrypt("dec"))
	_, _, err := r.ResolveTokenForWrite(context.Background(), validUUID, "member", validUUID)
	if err == nil {
		t.Fatalf("expected error when workspace has no connection")
	}
}

// Use distinct UUIDs for workspace and user to keep this test independent
// of Minor M2's rename of validUUID → workspaceUUID/userUUID.
const (
	rejectWorkspaceUUID = "00000000-0000-0000-0000-00000000aaaa"
	rejectUserUUID      = "00000000-0000-0000-0000-00000000bbbb"
)

func TestResolveTokenForWrite_RejectsUnknownActorType(t *testing.T) {
	q := &fakeResolverQueries{
		workspaceConn: &db.WorkspaceGitlabConnection{
			ServiceTokenEncrypted: []byte("svc"),
		},
		userConn: &db.UserGitlabConnection{
			PatEncrypted: []byte("usr"),
			GitlabUserID: 100,
		},
	}
	r := NewResolver(q, stubDecrypt("dec"))

	cases := []struct {
		name      string
		actorType string
	}{
		{name: "empty", actorType: ""},
		{name: "typo", actorType: "manager"},
		{name: "unknown", actorType: "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := r.ResolveTokenForWrite(context.Background(), rejectWorkspaceUUID, tc.actorType, rejectUserUUID)
			if err == nil {
				t.Fatalf("expected error for actorType %q, got nil", tc.actorType)
			}
			if !strings.Contains(err.Error(), "unknown actor type") {
				t.Errorf("error = %q, want to contain %q", err.Error(), "unknown actor type")
			}
		})
	}
}
