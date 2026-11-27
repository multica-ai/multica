package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/featureflag"
)

func TestTrajectoryRoutesWithFeatureEnabled(t *testing.T) {
	f := testutil.New(testPool, testWorkspaceID, testUserID)
	parent := f.Issue(t, "graph router parent")
	child := f.Issue(t, "graph router child", testutil.Cols{"parent_issue_id": parent})
	flags := featureflag.NewStaticProvider()
	flags.Set(featureflags.TrajectoryV1, featureflag.Rule{Default: true})
	router, _ := NewRouterWithOptions(testPool, realtime.NewHub(), events.New(), analytics.NoopClient{}, nil,
		RouterOptions{FeatureFlags: featureflag.NewService(flags)})
	newRequest := func(t *testing.T, token, method, path string, body any) *http.Request {
		t.Helper()
		req := testutil.JSONRequest(method, path, body)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Workspace-ID", testWorkspaceID)
		return req
	}
	request := func(method, path string, body any) *http.Response {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, newRequest(t, testToken, method, path, body))
		return w.Result()
	}

	resp := request("GET", "/api/trajectory/capability", nil)
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	var capability map[string]any
	if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &capability) != nil || capability["enabled"] != true || len(capability) != 1 {
		t.Fatalf("capability status=%d body=%s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Cache-Control"); got != "private,no-store" {
		t.Fatalf("capability Cache-Control=%q", got)
	}

	for _, tc := range []struct {
		method, path string
		body         any
		want         []string
	}{
		{"POST", "/api/issues/trajectory", map[string]any{"issue_ids": []string{child}}, []string{"issue:" + parent, "issue:" + child}},
		{"GET", "/api/issues/" + child + "/trajectory?scope=issue", nil, []string{"issue:" + child}},
		{"GET", "/api/issues/" + child + "/trajectory", nil, []string{"issue:" + parent, "issue:" + child}},
	} {
		resp := request(tc.method, tc.path, tc.body)
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s %s status=%d body=%s", tc.method, tc.path, resp.StatusCode, body)
		}
		var graph struct {
			Nodes []struct {
				ID string `json:"id"`
			} `json:"nodes"`
		}
		if err := json.Unmarshal(body, &graph); err != nil {
			t.Fatal(err)
		}
		got := map[string]bool{}
		for _, node := range graph.Nodes {
			got[node.ID] = true
		}
		for _, id := range tc.want {
			if !got[id] {
				t.Fatalf("%s %s missing %s in %s", tc.method, tc.path, id, body)
			}
		}
	}

	t.Run("guest capability is disabled even when Trajectory is enabled", func(t *testing.T) {
		email := "trajectory-guest-" + uuid.NewString() + "@example.test"
		guestID := f.User(t, "Trajectory guest", email)
		f.Member(t, testWorkspaceID, guestID, "guest")
		token, err := generateTestJWT(guestID, email, "Trajectory guest")
		if err != nil {
			t.Fatal(err)
		}

		var capability map[string]bool
		testutil.Call(t, router.ServeHTTP, newRequest(t, token, http.MethodGet, "/api/trajectory/capability", nil)).
			Want(http.StatusOK).JSON(&capability)
		if enabled, ok := capability["enabled"]; !ok || enabled {
			t.Fatalf("guest capability = %v, want enabled: false", capability)
		}
		testutil.Call(t, router.ServeHTTP, newRequest(t, token, http.MethodGet, "/api/issues/"+child+"/trajectory", nil)).
			Want(http.StatusForbidden)
		testutil.Call(t, router.ServeHTTP, newRequest(t, token, http.MethodPost, "/api/issues/trajectory", map[string]any{"issue_ids": []string{child}})).
			Want(http.StatusForbidden)
	})
}
