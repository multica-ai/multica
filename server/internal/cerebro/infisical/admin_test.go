package infisical

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeInfisical is a minimal in-memory stand-in for the subset of Infisical
// admin endpoints the AdminClient calls. It tracks every request so tests can
// assert on the recreation behavior (delete-then-create on scope change).
type fakeInfisical struct {
	t            *testing.T
	mu           sync.Mutex
	identities   map[string]bool             // identityID -> exists
	privileges   map[string][]string         // identityID -> slug list
	memberships  map[string]string           // identityID -> projectId
	clientByID   map[string]string           // identityID -> clientId
	calls        []string                    // method+path log
	loginToken   string
	clientSecret string
	server       *httptest.Server
}

func newFakeInfisical(t *testing.T) *fakeInfisical {
	t.Helper()
	f := &fakeInfisical{
		t:            t,
		identities:   map[string]bool{},
		privileges:   map[string][]string{},
		memberships:  map[string]string{},
		clientByID:   map[string]string{},
		loginToken:   "admin-access-token",
		clientSecret: "fresh-client-secret",
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/universal-auth/login", func(w http.ResponseWriter, r *http.Request) {
		f.log("POST /api/v1/auth/universal-auth/login")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"accessToken": f.loginToken,
			"expiresIn":   1800,
		})
	})
	mux.HandleFunc("/api/v1/identities", func(w http.ResponseWriter, r *http.Request) {
		f.requireAuth(w, r)
		f.log("POST /api/v1/identities")
		id := "identity-" + randSuffix()
		f.mu.Lock()
		f.identities[id] = true
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"identity": map[string]any{"id": id},
		})
	})
	mux.HandleFunc("/api/v1/identities/", func(w http.ResponseWriter, r *http.Request) {
		f.requireAuth(w, r)
		id := strings.TrimPrefix(r.URL.Path, "/api/v1/identities/")
		f.log(r.Method + " /api/v1/identities/" + id)
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Method != http.MethodDelete {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !f.identities[id] {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		delete(f.identities, id)
		delete(f.privileges, id)
		delete(f.memberships, id)
		delete(f.clientByID, id)
		_ = json.NewEncoder(w).Encode(map[string]any{"identity": map[string]any{"id": id}})
	})
	mux.HandleFunc("/api/v1/auth/universal-auth/identities/", func(w http.ResponseWriter, r *http.Request) {
		f.requireAuth(w, r)
		rest := strings.TrimPrefix(r.URL.Path, "/api/v1/auth/universal-auth/identities/")
		if strings.HasSuffix(rest, "/client-secrets") {
			id := strings.TrimSuffix(rest, "/client-secrets")
			f.log("POST /api/v1/auth/universal-auth/identities/" + id + "/client-secrets")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"clientSecret": f.clientSecret,
			})
			return
		}
		f.log("POST /api/v1/auth/universal-auth/identities/" + rest)
		clientID := "client-" + rest
		f.mu.Lock()
		f.clientByID[rest] = clientID
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"identityUniversalAuth": map[string]any{"clientId": clientID},
		})
	})
	mux.HandleFunc("/api/v1/projects/", func(w http.ResponseWriter, r *http.Request) {
		f.requireAuth(w, r)
		// /api/v1/projects/{projectId}/identity-memberships/{identityId}
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/projects/"), "/")
		if len(parts) != 3 || parts[1] != "identity-memberships" {
			http.Error(w, "bad path", http.StatusNotFound)
			return
		}
		projectID, identityID := parts[0], parts[2]
		f.log("POST /api/v1/projects/" + projectID + "/identity-memberships/" + identityID)
		f.mu.Lock()
		f.memberships[identityID] = projectID
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"identityMembership": map[string]any{"id": "mship-" + identityID, "projectId": projectID, "identityId": identityID},
		})
	})
	mux.HandleFunc("/api/v2/identity-project-additional-privilege", func(w http.ResponseWriter, r *http.Request) {
		f.requireAuth(w, r)
		f.log("POST /api/v2/identity-project-additional-privilege")
		var body struct {
			IdentityID string `json:"identityId"`
			Slug       string `json:"slug"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.privileges[body.IdentityID] = append(f.privileges[body.IdentityID], body.Slug)
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"privilege": map[string]any{"id": "priv-" + body.Slug}})
	})
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeInfisical) requireAuth(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+f.loginToken {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}
}

func (f *fakeInfisical) log(s string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, s)
}

func (f *fakeInfisical) callCounts() map[string]int {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]int{}
	for _, c := range f.calls {
		out[c]++
	}
	return out
}

func (f *fakeInfisical) liveIdentities() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.identities)
}

var suffixCounter int
var suffixMu sync.Mutex

func randSuffix() string {
	suffixMu.Lock()
	defer suffixMu.Unlock()
	suffixCounter++
	return "x" + fmtInt(suffixCounter)
}

func fmtInt(i int) string {
	if i == 0 {
		return "0"
	}
	s := ""
	for i > 0 {
		s = string(rune('0'+i%10)) + s
		i /= 10
	}
	return s
}

func newAdminFromFake(f *fakeInfisical) *AdminClient {
	return &AdminClient{
		Domain:       f.server.URL,
		OrgID:        "org-1",
		ProjectID:    "proj-1",
		ClientID:     "admin-client-id",
		ClientSecret: "admin-client-secret",
		HTTP:         f.server.Client(),
	}
}

func TestEnsureUserIdentityProvisionsFreshIdentityWithPrivileges(t *testing.T) {
	f := newFakeInfisical(t)
	admin := newAdminFromFake(f)

	ctx := context.Background()
	creds, err := admin.EnsureUserIdentity(ctx, "multica-user-1", "", ScopedPaths{
		Folders: []FolderRef{
			{Environment: "prod", SecretPath: "/app"},
			{Environment: "prod", SecretPath: "/cache"},
		},
	})
	if err != nil {
		t.Fatalf("EnsureUserIdentity: %v", err)
	}
	if creds.IdentityID == "" || creds.ClientID == "" || creds.ClientSecret == "" {
		t.Fatalf("credentials missing fields: %+v", creds)
	}
	counts := f.callCounts()
	if counts["POST /api/v1/identities"] != 1 {
		t.Fatalf("expected one create-identity call, got %#v", counts)
	}
	if counts["POST /api/v2/identity-project-additional-privilege"] != 2 {
		t.Fatalf("expected one privilege per folder, got %#v", counts)
	}
}

func TestEnsureUserIdentityDeletesPriorIdentityOnRescope(t *testing.T) {
	f := newFakeInfisical(t)
	admin := newAdminFromFake(f)

	ctx := context.Background()
	first, err := admin.EnsureUserIdentity(ctx, "multica-user-1", "", ScopedPaths{
		Folders: []FolderRef{{Environment: "prod", SecretPath: "/app"}},
	})
	if err != nil {
		t.Fatalf("first EnsureUserIdentity: %v", err)
	}

	// Re-scope: pass the existing identity id so the admin deletes it first.
	second, err := admin.EnsureUserIdentity(ctx, "multica-user-1", first.IdentityID, ScopedPaths{
		Folders: []FolderRef{{Environment: "prod", SecretPath: "/different"}},
	})
	if err != nil {
		t.Fatalf("re-scope EnsureUserIdentity: %v", err)
	}
	if second.IdentityID == first.IdentityID {
		t.Fatalf("expected fresh identity id after rescope")
	}
	if got := f.liveIdentities(); got != 1 {
		t.Fatalf("expected exactly 1 live identity after rescope, got %d", got)
	}
}

func TestEnsureUserIdentityTolerantOfMissingPriorIdentity(t *testing.T) {
	f := newFakeInfisical(t)
	admin := newAdminFromFake(f)

	// Pass an id that does not exist — fake returns 404 on DELETE, but the
	// provisioner must continue and recreate.
	creds, err := admin.EnsureUserIdentity(context.Background(), "multica-user-1", "identity-never-existed", ScopedPaths{
		Folders: []FolderRef{{Environment: "prod", SecretPath: "/app"}},
	})
	if err != nil {
		t.Fatalf("EnsureUserIdentity: %v", err)
	}
	if creds.IdentityID == "" {
		t.Fatalf("expected fresh identity created even when prior delete 404'd")
	}
}

func TestScopedPathsHashIsOrderIndependent(t *testing.T) {
	a := ScopedPaths{Folders: []FolderRef{
		{Environment: "prod", SecretPath: "/a"},
		{Environment: "prod", SecretPath: "/b"},
	}}.Hash()
	b := ScopedPaths{Folders: []FolderRef{
		{Environment: "prod", SecretPath: "/b"},
		{Environment: "prod", SecretPath: "/a"},
	}}.Hash()
	if a != b {
		t.Fatalf("hash must be order-independent: %s vs %s", a, b)
	}
}

func TestScopedPathsHashChangesWhenFoldersChange(t *testing.T) {
	a := ScopedPaths{Folders: []FolderRef{{Environment: "prod", SecretPath: "/a"}}}.Hash()
	b := ScopedPaths{Folders: []FolderRef{{Environment: "prod", SecretPath: "/b"}}}.Hash()
	if a == b {
		t.Fatalf("hash must change when folders change")
	}
}
