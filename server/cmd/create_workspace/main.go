// Command create_workspace creates a workspace on an instance running with
// DISABLE_WORKSPACE_CREATION=true, where no caller — not even an existing
// owner — can reach POST /api/workspaces. It exists for the one job that gate
// cannot cover on a self-hosted deployment: standing up the first workspace,
// or seeding one for an account that has signed up but has none.
//
// It drives the same handler the API route uses, with the gate forced off for
// this single in-process call. That is deliberate: slug format, reserved-slug
// rejection, issue-prefix derivation, the owner membership row, and the
// built-in issue-status seed all stay in handler.CreateWorkspace instead of
// being copied here, where they would drift.
//
// The command does not mark the owner as onboarded. CreateWorkspace
// deliberately leaves `onboarded_at` to CompleteOnboarding, and the onboarding
// flow already handles this case: with creation disabled and a workspace
// present, step 2 offers "Continue with <name>" and finishes through the
// existing-workspace path.
//
// Run it where DATABASE_URL is set (the server container, or a shell with the
// deployment's connection string):
//
//	create_workspace -slug acme -name "Acme" -owner admin@example.com
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type createInput struct {
	Slug        string
	Name        string
	Owner       string
	Description string
	Context     string
	IssuePrefix string
}

type createdWorkspace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

func main() {
	// Building the handler emits its server boot-log line ("llm retry policy")
	// at INFO. This command is not booting a server, and an operator running it
	// should see only the command's own result — keep warnings and errors.
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))

	var in createInput
	flag.StringVar(&in.Slug, "slug", "", "workspace slug (lowercase letters, numbers, and hyphens)")
	flag.StringVar(&in.Name, "name", "", "display name of the workspace")
	flag.StringVar(&in.Owner, "owner", "", "email of the account that will own the workspace; it must already have signed up")
	flag.StringVar(&in.Description, "description", "", "optional workspace description")
	flag.StringVar(&in.Context, "context", "", "optional context handed to agents working in this workspace")
	flag.StringVar(&in.IssuePrefix, "issue-prefix", "", "optional issue key prefix; defaults to one derived from the slug")
	flag.Parse()

	if in.Slug == "" || in.Name == "" || in.Owner == "" {
		fmt.Fprintln(os.Stderr, "create_workspace: -slug, -name, and -owner are required")
		flag.Usage()
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		fail(err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		fail(fmt.Errorf("connect to the database: %w", err))
	}

	created, err := runCreateWorkspace(ctx, pool, in)
	if err != nil {
		fail(err)
	}

	fmt.Printf("created workspace %q (%s)\n", created.Slug, created.ID)
	fmt.Printf("%s now sees it in their workspace list, and as \"Continue with %s\" if they have not finished onboarding\n",
		in.Owner, created.Name)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "create_workspace:", err)
	os.Exit(1)
}

// runCreateWorkspace resolves the owner, then creates the workspace through
// the API handler itself.
//
// The handler is built with DisableWorkspaceCreation left at its zero value on
// purpose: the gate is the thing the operator is working around, so honouring
// it here would make the command a no-op. Every other check in the handler
// still applies.
func runCreateWorkspace(ctx context.Context, pool *pgxpool.Pool, in createInput) (createdWorkspace, error) {
	var zero createdWorkspace

	// Resolve the owner before creating anything: an unknown email must fail
	// loudly rather than leave behind a workspace with no member row, which
	// nobody could administer or even see.
	owner, err := db.New(pool).GetUserByEmail(ctx, in.Owner)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return zero, fmt.Errorf("no account for owner %q; the owner must sign up before it can own a workspace", in.Owner)
		}
		return zero, fmt.Errorf("look up owner %q: %w", in.Owner, err)
	}
	ownerID := util.UUIDToString(owner.ID)

	body := map[string]any{"name": in.Name, "slug": in.Slug}
	if in.Description != "" {
		body["description"] = in.Description
	}
	if in.Context != "" {
		body["context"] = in.Context
	}
	if in.IssuePrefix != "" {
		body["issue_prefix"] = in.IssuePrefix
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return zero, fmt.Errorf("encode request: %w", err)
	}

	// httptest gives the handler a real *http.Request and *http.ResponseWriter
	// without binding a port, so the command calls the exact code path the
	// route serves.
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces", bytes.NewReader(payload))
	req = req.WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", ownerID)
	rec := httptest.NewRecorder()

	newHandler(pool).CreateWorkspace(rec, req)

	if rec.Code != http.StatusCreated {
		return zero, errors.New(errorMessage(rec))
	}

	var created createdWorkspace
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		return zero, fmt.Errorf("decode response: %w", err)
	}
	return created, nil
}

// newHandler builds the smallest handler that can serve CreateWorkspace. The
// realtime hub, event bus, mailer, and storage are nil because this call
// creates no tasks, sends no mail, and touches no objects; the handler's
// side effects are all nil-safe, and Metrics stays nil so only the (no-op)
// analytics client is consulted.
func newHandler(pool *pgxpool.Pool) *handler.Handler {
	return handler.New(
		db.New(pool), pool,
		nil, nil, nil, nil, nil,
		analytics.NoopClient{},
		handler.Config{},
	)
}

// errorMessage surfaces the handler's own {"error": "..."} text, so a rejected
// slug or a duplicate reads the same here as it would in the API response.
func errorMessage(rec *httptest.ResponseRecorder) string {
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err == nil && body.Error != "" {
		return body.Error
	}
	return fmt.Sprintf("unexpected status %d: %s", rec.Code, rec.Body.String())
}
