package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// TestRequireHumanActor_AllowsHumanRequest pins the happy path: a
// request that passed Auth as a JWT or mul_ PAT does NOT carry
// X-Actor-Source, so the guard lets it through and the inner handler
// runs.
//
// We construct a bare http.Handler chain (no full router) so the test
// exercises only the middleware logic and is independent of any
// downstream wiring.
func TestRequireHumanActor_AllowsHumanRequest(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	mw := RequireHumanActor(next)

	req := httptest.NewRequest(http.MethodGet, "/api/cloud-billing/balance", nil)
	// No X-Actor-Source — this is the JWT / mul_ PAT shape.
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !called {
		t.Fatal("inner handler must run for non-task-token requests")
	}
}

// TestRequireHumanActor_BlocksMachineCredentials walks every machine-
// credential X-Actor-Source value the auth middlewares stamp today
// and confirms each is rejected with 403. The two values must stay
// in lockstep with auth.go and daemon_auth.go: a new machine
// credential added there without a corresponding case here would
// silently grant agents/nodes account-level access.
func TestRequireHumanActor_BlocksMachineCredentials(t *testing.T) {
	cases := []struct {
		name        string
		actorSource string
	}{
		// mat_ task token — set in middleware/auth.go's mat_ branch.
		// An agent process holding its task-scoped token must not be
		// able to read its owner's billing data.
		{name: "task_token", actorSource: "task_token"},
		// mcn_ cloud-node PAT — set in BOTH middleware/auth.go and
		// middleware/daemon_auth.go's mcn_ branches. A cloud-runtime
		// EC2 node operating on the owner's behalf is the same kind
		// of machine credential as mat_ for billing-authorization
		// purposes.
		{name: "cloud_pat", actorSource: "cloud_pat"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
				t.Fatalf("inner handler must NOT run for actor source %q", tc.actorSource)
			})
			mw := RequireHumanActor(next)

			req := httptest.NewRequest(http.MethodGet, "/api/cloud-billing/balance", nil)
			// This is what the Auth (or DaemonAuth) middleware sets
			// for the matching token kind. Setting it directly here
			// proves the gate triggers on the header regardless of
			// upstream context — the auth middlewares strip any
			// client-supplied value before stamping their own, so a
			// non-empty value at this point IS authoritative.
			req.Header.Set("X-Actor-Source", tc.actorSource)
			w := httptest.NewRecorder()
			mw.ServeHTTP(w, req)

			if w.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", w.Code)
			}
		})
	}
}

// TestRequireHumanActor_IgnoresUnknownActorSource pins the gate's
// scope: it is an explicit denylist against the known-bad
// "task_token" value, NOT an allowlist against "human only / empty".
//
// Why the denylist shape:
//
//   - The Auth middleware today sets X-Actor-Source for exactly one
//     case: mat_ task tokens. Every other authenticated path (JWT,
//     mul_ PAT) leaves the header empty. So "non-empty AND not
//     task_token" is unreachable in current production.
//
//   - If a future actor kind is added (say a hypothetical
//     `service_account` token), this gate's silence on the new value
//     is a CONSCIOUS DECISION POINT, not an accident. The added auth
//     branch is the right place to decide whether the new kind should
//     be allowed at billing endpoints — and that decision belongs in
//     a security review at the time, not in a default-deny rule here
//     that pre-emptively shuts out hypothetical use cases we cannot
//     reason about today.
//
// If you are reading this comment because a new actor kind needs to
// reach billing or needs to be blocked from it, update
// RequireHumanActor to handle the new kind explicitly (and update
// this test's expectation accordingly).
func TestRequireHumanActor_IgnoresUnknownActorSource(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	mw := RequireHumanActor(next)

	// A hypothetical future value the gate doesn't know about.
	req := httptest.NewRequest(http.MethodGet, "/api/cloud-billing/balance", nil)
	req.Header.Set("X-Actor-Source", "future_kind")
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — gate should only block exact 'task_token'", w.Code)
	}
	if !called {
		t.Fatal("inner handler must run for unknown actor sources")
	}
}

// TestRequireHumanActor_AppliedViaChiRouterUse pins the wiring side of
// the contract: when the guard is attached to a chi route group via
// r.Use, every endpoint in that group is protected, and a task-token
// request never reaches the handler — even one we add later. This is
// what router.go's r.Route("/api/cloud-billing", ...) + r.Use(...)
// guarantees in production; the test is small but a developer adding
// a new billing endpoint and forgetting to re-attach the middleware
// would not be caught by the per-handler tests above.
func TestRequireHumanActor_AppliedViaChiRouterUse(t *testing.T) {
	// Use a real chi router so we exercise r.Use(), not just the
	// middleware function in isolation.
	r := chi.NewRouter()
	r.Use(RequireHumanActor)
	r.Get("/billing/probe", func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("inner handler must NOT run when guard rejects")
	})

	req := httptest.NewRequest(http.MethodGet, "/billing/probe", nil)
	req.Header.Set("X-Actor-Source", "task_token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
}
