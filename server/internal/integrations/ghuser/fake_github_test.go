package ghuser

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	testClientID     = "Iv1.testclient"
	testClientSecret = "test-client-secret"
)

// fakeGitHub serves the OAuth token endpoint, GET /user and the grant
// revocation endpoint. Refresh tokens rotate: each one is accepted once.
type fakeGitHub struct {
	srv *httptest.Server

	mu               sync.Mutex
	issued           int
	exchanges        int
	refreshes        int
	revokes          int
	revokedToken     string
	validRefresh     map[string]bool
	refreshDelay     time.Duration
	refreshError     string
	refreshStatus    int
	revokeStatus     int
	expiresIn        int64
	refreshExpiresIn int64
	badCredentials   bool
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	t.Helper()
	f := &fakeGitHub{validRefresh: map[string]bool{}, expiresIn: 8 * 3600, refreshExpiresIn: 184 * 24 * 3600}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeGitHub) issueLocked() map[string]any {
	f.issued++
	refresh := fmt.Sprintf("ghr_%d", f.issued)
	f.validRefresh[refresh] = true
	return map[string]any{
		"access_token":             fmt.Sprintf("ghu_%d", f.issued),
		"expires_in":               f.expiresIn,
		"refresh_token":            refresh,
		"refresh_token_expires_in": f.refreshExpiresIn,
		"token_type":               "bearer",
	}
}

func (f *fakeGitHub) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/login/oauth/access_token":
		_ = r.ParseForm()
		if r.Form.Get("client_id") != testClientID || r.Form.Get("client_secret") != testClientSecret {
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "incorrect_client_credentials"})
			return
		}
		if r.Form.Get("grant_type") == "refresh_token" {
			f.mu.Lock()
			f.refreshes++
			delay, status, oauthErr := f.refreshDelay, f.refreshStatus, f.refreshError
			f.mu.Unlock()
			time.Sleep(delay)
			if status != 0 {
				w.WriteHeader(status)
				return
			}
			if oauthErr != "" {
				_ = json.NewEncoder(w).Encode(map[string]string{"error": oauthErr})
				return
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			token := r.Form.Get("refresh_token")
			if !f.validRefresh[token] {
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "bad_refresh_token"})
				return
			}
			delete(f.validRefresh, token)
			_ = json.NewEncoder(w).Encode(f.issueLocked())
			return
		}
		if r.Form.Get("code") != "good-code" {
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "bad_verification_code"})
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.exchanges++
		_ = json.NewEncoder(w).Encode(f.issueLocked())
	case r.Method == http.MethodGet && r.URL.Path == "/user":
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ghu_") {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":4242,"login":"octocat","avatar_url":"https://avatars.example/u/4242"}`))
	case r.Method == http.MethodDelete && r.URL.Path == "/applications/"+testClientID+"/grant":
		user, pass, ok := r.BasicAuth()
		if !ok || user != testClientID || pass != testClientSecret {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var body struct {
			AccessToken string `json:"access_token"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.revokes++
		f.revokedToken = body.AccessToken
		status := f.revokeStatus
		f.mu.Unlock()
		if status == 0 {
			status = http.StatusNoContent
		}
		w.WriteHeader(status)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeGitHub) counts() (exchanges, refreshes, revokes int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.exchanges, f.refreshes, f.revokes
}

func testKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	return key
}

// clock is a settable time source shared by a test and the service.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newTestService(t *testing.T, queries *db.Queries, pool TxBeginner, fake *fakeGitHub) (*Service, *clock) {
	t.Helper()
	svc, err := New(Config{ClientID: testClientID, ClientSecret: testClientSecret, SecretKey: testKey(t)}, queries, pool)
	if err != nil {
		t.Fatal(err)
	}
	c := &clock{now: time.Now()}
	svc.SetClock(c.Now)
	if fake != nil {
		svc.SetEndpoints(fake.srv.URL, fake.srv.URL)
	}
	return svc, c
}

func testDBPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Skipf("skipping DB test: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Skipf("skipping DB test: database not reachable: %v", err)
	}
	// Registered first so it runs after every row cleanup a test adds.
	t.Cleanup(pool.Close)
	return pool
}

func newUserID(t *testing.T, pool *pgxpool.Pool) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	copy(id.Bytes[:], b)
	id.Valid = true
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM github_user_connection WHERE user_id = $1`, id)
	})
	return id
}
