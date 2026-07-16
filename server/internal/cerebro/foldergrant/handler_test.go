package foldergrant

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
)

func TestValidSurface(t *testing.T) {
	for _, s := range []string{"artifact", "entity", "app"} {
		if !validSurface(s) {
			t.Errorf("expected %q to be valid", s)
		}
	}
	for _, s := range []string{"", "note", "document", "ARTIFACT"} {
		if validSurface(s) {
			t.Errorf("expected %q to be invalid", s)
		}
	}
}

func TestAppCollectionGrantQueriesCoverWorkspaceAndInheritance(t *testing.T) {
	query, err := os.ReadFile("../queries/folder_grant.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, contract := range []string{"ListCerebroAppFolderEffectiveGrants", "GetCerebroAppFolderWorkspace", "cerebro_app_folder", "g.surface = 'app'"} {
		if !strings.Contains(string(query), contract) {
			t.Errorf("app Collection grant query is missing %q", contract)
		}
	}
	migration, err := os.ReadFile("../../../migrations/9140_cerebro_app_collections.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(migration), "'artifact', 'entity', 'app'") {
		t.Fatal("app grant surface is missing from the migration")
	}
}

func TestValidGranteeType(t *testing.T) {
	for _, s := range []string{"group", "member", "workspace", "agent", "runtime"} {
		if !validGranteeType(s) {
			t.Errorf("expected %q to be valid", s)
		}
	}
	for _, s := range []string{"", "user", "team", "Group"} {
		if validGranteeType(s) {
			t.Errorf("expected %q to be invalid", s)
		}
	}
}

func TestValidRole(t *testing.T) {
	for _, s := range []string{"viewer", "editor", "full_access"} {
		if !validRole(s) {
			t.Errorf("expected %q to be valid", s)
		}
	}
	for _, s := range []string{"", "owner", "admin", "Viewer", "full access"} {
		if validRole(s) {
			t.Errorf("expected %q to be invalid", s)
		}
	}
}

func TestGranteeIDPtr(t *testing.T) {
	// An unset pgtype.UUID (Valid=false) is the workspace-grant case -> nil.
	var unset pgtype.UUID
	if granteeIDPtr(unset) != nil {
		t.Errorf("expected nil pointer for an unset uuid")
	}
	valid, err := util.ParseUUID("11111111-1111-1111-1111-111111111111")
	if err != nil {
		t.Fatalf("parse uuid: %v", err)
	}
	if got := granteeIDPtr(valid); got == nil || *got != "11111111-1111-1111-1111-111111111111" {
		t.Errorf("expected the uuid string, got %v", got)
	}
}

func TestUpsertGrantRequiresAuth(t *testing.T) {
	h := NewHandler(nil) // no DB hit on the auth-failure path
	req := httptest.NewRequest(http.MethodPut, "/api/cerebro/folder-grants",
		strings.NewReader(`{"surface":"artifact","folder_id":"11111111-1111-1111-1111-111111111111","grantee_type":"workspace","role":"viewer"}`))
	rec := httptest.NewRecorder()
	h.UpsertGrant(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without X-User-ID, got %d", rec.Code)
	}
}
