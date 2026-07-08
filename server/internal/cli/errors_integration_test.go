package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestHelperStatusErrorsAreClassified is an integration test that drives the
// real client helpers against a fake server and asserts the error they return
// flows correctly through the top-level FormatError / ExitCodeFor. This is the
// coverage the unit tests were missing: it proves that the actual command
// paths (issue update -> PutJSON, comment list -> GetJSONWithHeaders, project
// delete -> DeleteJSON, agent update -> PatchJSON, upload, download) all get
// the friendly copy and the tiered exit code, not the old raw string + exit 1.
func TestHelperStatusErrorsAreClassified(t *testing.T) {
	withLang(t, "en_US.UTF-8")

	// Each helper wraps a real client call onto a fixed path. The server
	// returns whatever status the current case dictates.
	helpers := []struct {
		name string
		call func(c *APIClient, ctx context.Context) error
	}{
		{"GetJSON (issue get)", func(c *APIClient, ctx context.Context) error {
			var out map[string]any
			return c.GetJSON(ctx, "/api/issues/abc", &out)
		}},
		{"GetJSONWithHeaders (comment list)", func(c *APIClient, ctx context.Context) error {
			var out []any
			_, err := c.GetJSONWithHeaders(ctx, "/api/issues/abc/comments", &out)
			return err
		}},
		{"PostJSON (issue create)", func(c *APIClient, ctx context.Context) error {
			return c.PostJSON(ctx, "/api/issues", map[string]any{"title": "t"}, nil)
		}},
		{"PutJSON (issue update)", func(c *APIClient, ctx context.Context) error {
			return c.PutJSON(ctx, "/api/issues/abc", map[string]any{"title": "t"}, nil)
		}},
		{"PatchJSON (agent update)", func(c *APIClient, ctx context.Context) error {
			return c.PatchJSON(ctx, "/api/agents/abc", map[string]any{"name": "n"}, nil)
		}},
		{"DeleteJSON (project delete)", func(c *APIClient, ctx context.Context) error {
			return c.DeleteJSON(ctx, "/api/projects/abc")
		}},
		{"DeleteJSONWithBody (squad member remove)", func(c *APIClient, ctx context.Context) error {
			return c.DeleteJSONWithBody(ctx, "/api/squads/abc/members", map[string]any{"member_id": "x"})
		}},
		{"UploadFile (issue attachment)", func(c *APIClient, ctx context.Context) error {
			_, err := c.UploadFile(ctx, []byte("x"), "x.txt", "abc")
			return err
		}},
		{"UploadFileWithURL (avatar)", func(c *APIClient, ctx context.Context) error {
			_, _, err := c.UploadFileWithURL(ctx, []byte("x"), "x.txt")
			return err
		}},
		{"DownloadFile (attachment)", func(c *APIClient, ctx context.Context) error {
			_, err := c.DownloadFile(ctx, "/uploads/abc")
			return err
		}},
	}

	statusCases := []struct {
		status   int
		wantExit int
		wantCopy string
	}{
		{http.StatusUnauthorized, ExitAuth, "multica login"},
		{http.StatusForbidden, ExitAuth, "permission"},
		{http.StatusNotFound, ExitNotFound, "not found"},
		{http.StatusUnprocessableEntity, ExitValidation, "title is required"},
		{http.StatusInternalServerError, ExitGeneric, "temporarily unavailable"},
	}

	for _, sc := range statusCases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(sc.status)
			// Validation responses carry a server message that FormatError
			// should surface verbatim; other statuses ignore the body.
			io.WriteString(w, `{"error":"title is required"}`)
		}))

		for _, h := range helpers {
			t.Run(http.StatusText(sc.status)+"/"+h.name, func(t *testing.T) {
				client := NewAPIClient(srv.URL, "ws", "token")
				err := h.call(client, context.Background())
				if err == nil {
					t.Fatalf("expected error, got nil")
				}

				// 1. The helper must return a *HTTPError (possibly wrapped).
				var httpErr *HTTPError
				if !errors.As(err, &httpErr) {
					t.Fatalf("expected *HTTPError, got %T: %v", err, err)
				}
				if httpErr.StatusCode != sc.status {
					t.Errorf("status = %d, want %d", httpErr.StatusCode, sc.status)
				}

				// 2. Tiered exit code.
				if got := ExitCodeFor(err); got != sc.wantExit {
					t.Errorf("ExitCodeFor = %d, want %d", got, sc.wantExit)
				}

				// 3. Friendly, localized copy — and no raw path/verb leak.
				msg := FormatError(err, false)
				if !strings.Contains(msg, sc.wantCopy) {
					t.Errorf("FormatError = %q, want it to contain %q", msg, sc.wantCopy)
				}
				if strings.Contains(msg, "/api/") || strings.Contains(msg, "returned") {
					t.Errorf("FormatError leaked raw detail: %q", msg)
				}
			})
		}

		srv.Close()
	}
}

// TestCommandContextNotTruncatedBelowHTTPTimeout proves the must-fix: a
// command-level context derived from APIContext never cancels a request before
// the configured transport timeout. Previously commands hardcoded a 15s
// context that truncated a longer MULTICA_HTTP_TIMEOUT.
func TestCommandContextNotTruncatedBelowHTTPTimeout(t *testing.T) {
	withLang(t, "en_US.UTF-8")
	// Use a short transport timeout so the test runs fast; the command budget
	// (transport + grace) is what we assert is NOT the limiting factor.
	t.Setenv("MULTICA_HTTP_TIMEOUT", "400ms")

	if APITimeout() <= httpTimeout() {
		t.Fatalf("APITimeout (%v) must be strictly greater than httpTimeout (%v)", APITimeout(), httpTimeout())
	}

	t.Run("request within transport budget completes (context does not fire first)", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(120 * time.Millisecond) // < 400ms transport timeout
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
		}))
		defer srv.Close()

		client := NewAPIClient(srv.URL, "", "")
		ctx, cancel := APIContext(context.Background())
		defer cancel()

		var out map[string]string
		if err := client.GetJSON(ctx, "/x", &out); err != nil {
			t.Fatalf("expected success within transport budget, got %v", err)
		}
	})

	t.Run("transport timeout governs and yields a friendly timeout error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(900 * time.Millisecond) // > 400ms transport timeout
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		client := NewAPIClient(srv.URL, "", "")
		ctx, cancel := APIContext(context.Background())
		defer cancel()

		start := time.Now()
		err := client.GetJSON(ctx, "/x", nil)
		elapsed := time.Since(start)

		if err == nil {
			t.Fatal("expected a timeout error")
		}
		// Must fail near the 400ms transport timeout, not the ~5.4s command
		// context deadline. A generous 3s ceiling keeps the test non-flaky
		// while still proving the context did not govern.
		if elapsed > 3*time.Second {
			t.Errorf("request took %v; the command context likely truncated/governed instead of the transport timeout", elapsed)
		}
		if msg := FormatError(err, false); !strings.Contains(msg, "timed out") {
			t.Errorf("expected friendly timeout copy, got %q", msg)
		}
		if got := ExitCodeFor(err); got != ExitNetwork {
			t.Errorf("ExitCodeFor = %d, want ExitNetwork(%d)", got, ExitNetwork)
		}
	})
}

// TestAPITimeoutRespectsEnv verifies the command budget tracks
// MULTICA_HTTP_TIMEOUT and the AtLeast floor.
func TestAPITimeoutRespectsEnv(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		t.Setenv("MULTICA_HTTP_TIMEOUT", "")
		if got, want := APITimeout(), defaultHTTPTimeout+apiContextGrace; got != want {
			t.Errorf("APITimeout = %v, want %v", got, want)
		}
	})
	t.Run("env override raises both transport and command budget", func(t *testing.T) {
		t.Setenv("MULTICA_HTTP_TIMEOUT", "90s")
		if got, want := APITimeout(), 90*time.Second+apiContextGrace; got != want {
			t.Errorf("APITimeout = %v, want %v", got, want)
		}
	})
	t.Run("AtLeast floor wins when larger", func(t *testing.T) {
		t.Setenv("MULTICA_HTTP_TIMEOUT", "10s")
		if got, want := AtLeastAPITimeout(60*time.Second), 60*time.Second; got != want {
			t.Errorf("AtLeastAPITimeout = %v, want %v", got, want)
		}
	})
	t.Run("AtLeast budget wins when larger than floor", func(t *testing.T) {
		t.Setenv("MULTICA_HTTP_TIMEOUT", "120s")
		if got, want := AtLeastAPITimeout(60*time.Second), 120*time.Second+apiContextGrace; got != want {
			t.Errorf("AtLeastAPITimeout = %v, want %v", got, want)
		}
	})
	// The login workspace-creation poll uses a short 10s floor to stay
	// responsive, but it must still honor MULTICA_HTTP_TIMEOUT (it is not a
	// silent exception to the env-override promise). With the env unset the
	// budget is at least the 10s floor; with the env raised it scales up.
	t.Run("login poll 10s floor honors env override", func(t *testing.T) {
		t.Setenv("MULTICA_HTTP_TIMEOUT", "")
		if got := AtLeastAPITimeout(10 * time.Second); got < 10*time.Second {
			t.Errorf("AtLeastAPITimeout(10s) = %v, want >= 10s floor", got)
		}
		t.Setenv("MULTICA_HTTP_TIMEOUT", "90s")
		if got, want := AtLeastAPITimeout(10*time.Second), 90*time.Second+apiContextGrace; got != want {
			t.Errorf("AtLeastAPITimeout(10s) with env=90s = %v, want %v", got, want)
		}
	})
}
