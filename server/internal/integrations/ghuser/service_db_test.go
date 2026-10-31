package ghuser

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func connectedService(t *testing.T) (*Service, *clock, *fakeGitHub, *db.Queries, pgtype.UUID) {
	t.Helper()
	pool := testDBPool(t)
	queries := db.New(pool)
	fake := newFakeGitHub(t)
	svc, clk := newTestService(t, queries, pool, fake)
	user := newUserID(t, pool)
	account, err := svc.Connect(context.Background(), user, "good-code", "")
	if err != nil {
		t.Fatal(err)
	}
	if account.Login != "octocat" || account.GitHubUserID != 4242 {
		t.Fatalf("account = %+v", account)
	}
	return svc, clk, fake, queries, user
}

func TestConnectStoresSealedTokens(t *testing.T) {
	svc, _, _, queries, user := connectedService(t)
	ctx := context.Background()

	row, err := queries.GetGitHubUserConnection(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(row.AccessTokenEncrypted, []byte("ghu_")) || bytes.Contains(row.RefreshTokenEncrypted, []byte("ghr_")) {
		t.Fatal("tokens must be sealed at rest")
	}
	if row.Login != "octocat" || row.GithubUserID != 4242 || row.AvatarUrl.String != "https://avatars.example/u/4242" ||
		!row.AccessTokenExpiresAt.Valid || !row.RefreshTokenExpiresAt.Valid {
		t.Fatalf("row = %+v", row)
	}

	account, connected, err := svc.Status(ctx, user)
	if err != nil || !connected || account.Login != "octocat" {
		t.Fatalf("status = %+v %v %v", account, connected, err)
	}
	tok, err := svc.AccessToken(ctx, user)
	if err != nil || tok.AccessToken != "ghu_1" || tok.Login != "octocat" {
		t.Fatalf("token = %+v err=%v", tok, err)
	}
}

func TestConnectRejectsBadCode(t *testing.T) {
	pool := testDBPool(t)
	queries := db.New(pool)
	fake := newFakeGitHub(t)
	svc, _ := newTestService(t, queries, pool, fake)
	user := newUserID(t, pool)
	_, err := svc.Connect(context.Background(), user, "stolen-code", "")
	var oauthErr *oauthError
	if !errors.As(err, &oauthErr) || oauthErr.Code != "bad_verification_code" {
		t.Fatalf("err = %v", err)
	}
	if _, err := queries.GetGitHubUserConnection(context.Background(), user); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("a failed exchange must not store a connection: %v", err)
	}
}

func TestAccessTokenNotConnected(t *testing.T) {
	pool := testDBPool(t)
	svc, _ := newTestService(t, db.New(pool), pool, newFakeGitHub(t))
	if _, err := svc.AccessToken(context.Background(), newUserID(t, pool)); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("err = %v", err)
	}
	var unconfigured *Service
	if _, err := unconfigured.AccessToken(context.Background(), pgtype.UUID{}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("nil service: err = %v", err)
	}
}

func TestAccessTokenRefreshesAndRotates(t *testing.T) {
	svc, clk, fake, queries, user := connectedService(t)
	ctx := context.Background()

	// Within the 5-minute skew of the 8-hour expiry: refresh before use.
	clk.Advance(8*time.Hour - 4*time.Minute)
	tok, err := svc.AccessToken(ctx, user)
	if err != nil || tok.AccessToken != "ghu_2" {
		t.Fatalf("first refresh: token=%+v err=%v", tok, err)
	}
	// The rotated refresh token is the one used next; GitHub would reject
	// the spent ghr_1.
	clk.Advance(8 * time.Hour)
	tok, err = svc.AccessToken(ctx, user)
	if err != nil || tok.AccessToken != "ghu_3" {
		t.Fatalf("second refresh: token=%+v err=%v", tok, err)
	}
	if _, refreshes, _ := fake.counts(); refreshes != 2 {
		t.Fatalf("refreshes = %d, want 2", refreshes)
	}
	// A fresh token is served from the store without contacting GitHub.
	if tok, err = svc.AccessToken(ctx, user); err != nil || tok.AccessToken != "ghu_3" {
		t.Fatalf("cached: token=%+v err=%v", tok, err)
	}
	if _, refreshes, _ := fake.counts(); refreshes != 2 {
		t.Fatalf("a fresh token must not refresh; refreshes = %d", refreshes)
	}
	row, _ := queries.GetGitHubUserConnection(ctx, user)
	if !row.AccessTokenExpiresAt.Time.After(clk.Now().Add(7 * time.Hour)) {
		t.Fatalf("expiry not advanced: %v", row.AccessTokenExpiresAt.Time)
	}
}

func TestConcurrentCallersShareOneRefresh(t *testing.T) {
	svc, clk, fake, _, user := connectedService(t)
	fake.mu.Lock()
	fake.refreshDelay = 150 * time.Millisecond
	fake.mu.Unlock()
	clk.Advance(8 * time.Hour)

	const callers = 8
	var wg sync.WaitGroup
	tokens := make([]string, callers)
	errs := make([]error, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tok, err := svc.AccessToken(context.Background(), user)
			tokens[i], errs[i] = tok.AccessToken, err
		}(i)
	}
	wg.Wait()
	for i := range tokens {
		if errs[i] != nil || tokens[i] != "ghu_2" {
			t.Fatalf("caller %d: token=%q err=%v", i, tokens[i], errs[i])
		}
	}
	if _, refreshes, _ := fake.counts(); refreshes != 1 {
		t.Fatalf("refreshes = %d, want exactly 1", refreshes)
	}
}

func TestFailedRefreshClearsConnection(t *testing.T) {
	for name, setup := range map[string]func(f *fakeGitHub, c *clock){
		"bad refresh token": func(f *fakeGitHub, c *clock) {
			f.refreshError = "bad_refresh_token"
			c.Advance(8 * time.Hour)
		},
		"refresh token expired locally": func(f *fakeGitHub, c *clock) {
			c.Advance(185 * 24 * time.Hour)
		},
	} {
		t.Run(name, func(t *testing.T) {
			svc, clk, fake, queries, user := connectedService(t)
			ctx := context.Background()
			fake.mu.Lock()
			setup(fake, clk)
			fake.mu.Unlock()

			if _, err := svc.AccessToken(ctx, user); !errors.Is(err, ErrReauthRequired) {
				t.Fatalf("err = %v, want ErrReauthRequired", err)
			}
			if _, err := queries.GetGitHubUserConnection(ctx, user); !errors.Is(err, pgx.ErrNoRows) {
				t.Fatalf("connection must be cleared: %v", err)
			}
			if _, connected, err := svc.Status(ctx, user); connected || err != nil {
				t.Fatalf("status after clear: connected=%v err=%v", connected, err)
			}
		})
	}
}

// A GitHub outage or a deployment credential problem must not disconnect
// every user: those failures leave the stored connection alone.
func TestTransientRefreshFailureKeepsConnection(t *testing.T) {
	for name, setup := range map[string]func(f *fakeGitHub){
		"server error":       func(f *fakeGitHub) { f.refreshStatus = http.StatusBadGateway },
		"client credentials": func(f *fakeGitHub) { f.refreshError = "incorrect_client_credentials" },
	} {
		t.Run(name, func(t *testing.T) {
			svc, clk, fake, queries, user := connectedService(t)
			ctx := context.Background()
			fake.mu.Lock()
			setup(fake)
			fake.mu.Unlock()
			clk.Advance(8 * time.Hour)

			_, err := svc.AccessToken(ctx, user)
			if err == nil || errors.Is(err, ErrReauthRequired) {
				t.Fatalf("err = %v, want a transient error", err)
			}
			if strings.Contains(err.Error(), "ghr_") || strings.Contains(err.Error(), testClientSecret) {
				t.Fatalf("error leaked a credential: %v", err)
			}
			if _, err := queries.GetGitHubUserConnection(ctx, user); err != nil {
				t.Fatalf("connection must survive: %v", err)
			}
		})
	}
}

func TestDisconnectRevokesAndDeletes(t *testing.T) {
	svc, _, fake, queries, user := connectedService(t)
	ctx := context.Background()
	revoked, err := svc.Disconnect(ctx, user)
	if err != nil || !revoked {
		t.Fatalf("revoked=%v err=%v", revoked, err)
	}
	if _, _, revokes := fake.counts(); revokes != 1 || fake.revokedToken != "ghu_1" {
		t.Fatalf("revokes=%d token=%q", revokes, fake.revokedToken)
	}
	if _, err := queries.GetGitHubUserConnection(ctx, user); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("row must be deleted: %v", err)
	}
	// Disconnecting again is a no-op.
	if revoked, err := svc.Disconnect(ctx, user); err != nil || revoked {
		t.Fatalf("second disconnect: revoked=%v err=%v", revoked, err)
	}
}

func TestDisconnectDeletesEvenWhenRevokeFails(t *testing.T) {
	svc, _, fake, queries, user := connectedService(t)
	ctx := context.Background()
	fake.mu.Lock()
	fake.revokeStatus = http.StatusInternalServerError
	fake.mu.Unlock()
	revoked, err := svc.Disconnect(ctx, user)
	if err != nil || revoked {
		t.Fatalf("revoked=%v err=%v", revoked, err)
	}
	if _, err := queries.GetGitHubUserConnection(ctx, user); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("row must be deleted even when revocation fails: %v", err)
	}
}

func TestConnectReplacesPreviousAccount(t *testing.T) {
	svc, _, _, _, user := connectedService(t)
	ctx := context.Background()
	if _, err := svc.Connect(ctx, user, "good-code", ""); err != nil {
		t.Fatal(err)
	}
	tok, err := svc.AccessToken(ctx, user)
	if err != nil || tok.AccessToken != "ghu_2" {
		t.Fatalf("reconnect must replace the stored token: %+v %v", tok, err)
	}
}
