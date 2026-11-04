package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
)

// routerWithTestRoutes builds a router with MULTICA_TEST_ROUTES set to flag. The
// router reads the flag when it is built, so each case builds its own.
func routerWithTestRoutes(t *testing.T, flag string) *httptest.Server {
	t.Helper()
	t.Setenv("MULTICA_TEST_ROUTES", flag)
	t.Setenv("APP_ENV", "")
	srv := httptest.NewServer(NewRouter(testPool, realtime.NewHub(), events.New(), analytics.NoopClient{}, nil))
	t.Cleanup(srv.Close)
	return srv
}

func postRetroEvaluate(t *testing.T, srv *httptest.Server) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/test/retro/evaluate", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// With the flag off the test route does not exist: an authenticated call gets 404.
func TestTestRetroRouteAnswers404WhenTheFlagIsOff(t *testing.T) {
	if testPool == nil {
		t.Skip("integration database not available")
	}
	srv := routerWithTestRoutes(t, "")
	if resp := postRetroEvaluate(t, srv); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("flag off: POST /api/test/retro/evaluate = %d, want 404", resp.StatusCode)
	}
}

// With the flag on, the owner runs the retrospective triggers for the workspace.
func TestTestRetroRouteRunsTheTriggersWhenTheFlagIsOn(t *testing.T) {
	if testPool == nil {
		t.Skip("integration database not available")
	}
	srv := routerWithTestRoutes(t, "1")
	resp := postRetroEvaluate(t, srv)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("flag on: POST /api/test/retro/evaluate = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Fired *int `json:"fired"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || body.Fired == nil {
		t.Fatalf("the answer carries the number fired: %v %+v", err, body)
	}
}
