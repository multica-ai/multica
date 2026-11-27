package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/featureflag"
)

func trajectoryCapabilityCall(t *testing.T, h *Handler, workspaceID string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/trajectory/capability", nil)
	req.Header.Set("X-User-ID", testUserID)
	req = req.WithContext(middleware.SetMemberContext(req.Context(), workspaceID, db.Member{}))
	w := httptest.NewRecorder()
	h.GetTrajectoryCapability(w, req)
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "private,no-store" {
		t.Fatalf("capability status=%d headers=%v body=%s", w.Code, w.Header(), w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body
}

func TestTrajectoryCapabilityDefaultsOffAndTargetsWorkspaces(t *testing.T) {
	workspace, allowedWorkspace := uuid.NewString(), uuid.NewString()
	if got := trajectoryCapabilityCall(t, &Handler{}, workspace); got["enabled"] != false || len(got) != 1 {
		t.Fatalf("default capability = %v", got)
	}
	provider := featureflag.NewStaticProvider()
	provider.Set(featureflags.TrajectoryV1, featureflag.Rule{Default: false, Allow: []string{allowedWorkspace}, AllowBy: "workspace_id"})
	h := &Handler{FeatureFlags: featureflag.NewService(provider)}
	if got := trajectoryCapabilityCall(t, h, workspace); got["enabled"] != false {
		t.Fatalf("other workspace capability = %v", got)
	}
	if got := trajectoryCapabilityCall(t, h, allowedWorkspace); got["enabled"] != true {
		t.Fatalf("allowed workspace capability = %v", got)
	}
}

func TestTrajectoryCapabilityEnvOptIn(t *testing.T) {
	t.Setenv("FF_TRAJECTORY_V1", "on")
	h := &Handler{FeatureFlags: featureflag.NewService(featureflag.NewEnvProvider(featureflag.EnvOverridePrefix))}
	if got := trajectoryCapabilityCall(t, h, uuid.NewString()); got["enabled"] != true {
		t.Fatalf("FF_TRAJECTORY_V1=on capability = %v", got)
	}
}

func TestTrajectoryGraphReadsAnswer404WhenDisabled(t *testing.T) {
	h := &Handler{}
	issue := uuid.NewString()
	for _, call := range []struct {
		name string
		run  func(http.ResponseWriter, *http.Request)
		req  *http.Request
	}{
		{"list", h.ListTaskTrajectory, newRequest("POST", "/api/issues/trajectory", map[string]any{"issue_ids": []string{issue}})},
		{"issue", h.GetTrajectory, withURLParam(newRequest("GET", "/api/issues/"+issue+"/trajectory", nil), "id", issue)},
	} {
		w := httptest.NewRecorder()
		call.run(w, call.req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s status=%d want=404 body=%s", call.name, w.Code, w.Body.String())
		}
	}
}

func TestTrimTrajectoryGraphRecordsKeepsIssuesAndReadOrder(t *testing.T) {
	big := strings.Repeat("x", 400)
	nodes := []trajectoryGraphNode{
		{ID: "issue:a", Kind: "issue", Details: map[string]any{"description": big}},
		{ID: "run:1", Kind: "run", Details: map[string]any{"output": big}},
		{ID: "run:2", Kind: "run", Details: map[string]any{"output": big}},
		{ID: "comment:1", Kind: "comment", Details: map[string]any{}},
	}
	kept, trimmed := trimTrajectoryGraphRecords(nodes, 600)
	if !trimmed || len(kept) != 2 || kept[0].ID != "issue:a" || kept[1].ID != "run:1" {
		t.Fatalf("trimmed=%v kept=%v", trimmed, kept)
	}
	kept, trimmed = trimTrajectoryGraphRecords(nodes, 1<<20)
	if trimmed || len(kept) != len(nodes) {
		t.Fatalf("within budget: trimmed=%v kept=%d", trimmed, len(kept))
	}
}
