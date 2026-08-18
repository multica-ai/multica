package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// This catches the regression where a browser can bypass the disabled Squad
// controls and mutate an Extension's internal composition through the public
// Squad endpoints.
func TestExtensionManagedSquadExposesIdentityAndRejectsCompositionWrites(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	workspaceID := createPlatformExtensionTestWorkspace(t, "owner")
	runtimeID := createPlatformExtensionTestRuntime(t, workspaceID, platformExtensionRuntimeSeed{
		Provider: "platform-agent-cli", Status: "online", LastSeenAt: time.Now(), Visibility: "private", OwnerID: testUserID,
	})
	source, raw := twoByTwoPlatformExtensionSource(t, "managed-squad")
	h := platformExtensionHandlerWithLiveness(platformExtensionFakeLiveness{alive: map[string]bool{runtimeID: true}, ok: true})
	imported := decodePlatformExtensionImportResponse(t, importPlatformExtensionForTest(t, h, workspaceID, testUserID, raw).Body.Bytes())

	t.Run("list and detail expose extension identity", func(t *testing.T) {
		var listed []struct {
			ID        string `json:"id"`
			Extension *struct {
				ReleaseID    string `json:"release_id"`
				ExtensionKey string `json:"extension_key"`
				Version      string `json:"version"`
			} `json:"extension"`
		}
		listRecorder := httptest.NewRecorder()
		h.ListSquads(listRecorder, withURLParam(platformExtensionRequest(http.MethodGet, "/api/squads", workspaceID, testUserID, nil), "workspaceId", workspaceID))
		if listRecorder.Code != http.StatusOK {
			t.Fatalf("list status = %d: %s", listRecorder.Code, listRecorder.Body.String())
		}
		if err := json.Unmarshal(listRecorder.Body.Bytes(), &listed); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		var got *struct {
			ID        string `json:"id"`
			Extension *struct {
				ReleaseID    string `json:"release_id"`
				ExtensionKey string `json:"extension_key"`
				Version      string `json:"version"`
			} `json:"extension"`
		}
		for i := range listed {
			if listed[i].ID == imported.Squad.ID {
				got = &listed[i]
				break
			}
		}
		if got == nil || got.Extension == nil {
			t.Fatalf("managed Squad missing extension identity: %+v", listed)
		}
		if got.Extension.ReleaseID != imported.Release.ID || got.Extension.ExtensionKey != source.Extension.Key || got.Extension.Version != source.Extension.Version {
			t.Fatalf("extension identity = %+v, want release=%s key=%s version=%s", got.Extension, imported.Release.ID, source.Extension.Key, source.Extension.Version)
		}

		detailRecorder := httptest.NewRecorder()
		h.GetSquad(detailRecorder, withURLParams(platformExtensionRequest(http.MethodGet, "/api/squads/"+imported.Squad.ID, workspaceID, testUserID, nil), "workspaceId", workspaceID, "id", imported.Squad.ID))
		if detailRecorder.Code != http.StatusOK {
			t.Fatalf("detail status = %d: %s", detailRecorder.Code, detailRecorder.Body.String())
		}
		var detail struct {
			Extension *struct {
				ReleaseID string `json:"release_id"`
			} `json:"extension"`
		}
		if err := json.Unmarshal(detailRecorder.Body.Bytes(), &detail); err != nil {
			t.Fatalf("decode detail: %v", err)
		}
		if detail.Extension == nil || detail.Extension.ReleaseID != imported.Release.ID {
			t.Fatalf("detail extension = %+v, want release %s", detail.Extension, imported.Release.ID)
		}
	})

	for _, endpoint := range []struct {
		name string
		call func(http.ResponseWriter, *http.Request)
		body any
	}{
		{"add member", h.AddSquadMember, map[string]any{"member_type": "agent", "member_id": imported.Agents[0].ID}},
		{"remove member", h.RemoveSquadMember, map[string]any{"member_type": "agent", "member_id": imported.Agents[0].ID}},
		{"change role", h.UpdateSquadMemberRole, map[string]any{"member_type": "agent", "member_id": imported.Agents[0].ID, "role": "worker"}},
		{"change leader", h.UpdateSquad, map[string]any{"leader_id": imported.Agents[1].ID}},
	} {
		t.Run(endpoint.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := withURLParams(platformExtensionRequest(http.MethodPatch, "/api/squads/"+imported.Squad.ID, workspaceID, testUserID, mustJSON(t, endpoint.body)), "workspaceId", workspaceID, "id", imported.Squad.ID)
			endpoint.call(recorder, request)
			if recorder.Code != http.StatusConflict {
				t.Fatalf("status = %d, want 409: %s", recorder.Code, recorder.Body.String())
			}
			var response struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode error: %v", err)
			}
			if response.Code != "EXTENSION_MANAGED_SQUAD" {
				t.Fatalf("code = %q, want EXTENSION_MANAGED_SQUAD", response.Code)
			}
		})
	}
}

// This catches an authorization leak where an internal Agent detail endpoint
// would accept any system Agent ID in the workspace rather than only members
// owned by the requested Extension Squad.
func TestExtensionManagedSquadReadsOnlyItsOwnInternalAgentDetails(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	workspaceID := createPlatformExtensionTestWorkspace(t, "owner")
	runtimeID := createPlatformExtensionTestRuntime(t, workspaceID, platformExtensionRuntimeSeed{
		Provider: "platform-agent-cli", Status: "online", LastSeenAt: time.Now(), Visibility: "private", OwnerID: testUserID,
	})
	h := platformExtensionHandlerWithLiveness(platformExtensionFakeLiveness{alive: map[string]bool{runtimeID: true}, ok: true})
	_, firstRaw := twoByTwoPlatformExtensionSource(t, "internal-detail-one")
	_, secondRaw := twoByTwoPlatformExtensionSource(t, "internal-detail-two")
	first := decodePlatformExtensionImportResponse(t, importPlatformExtensionForTest(t, h, workspaceID, testUserID, firstRaw).Body.Bytes())
	second := decodePlatformExtensionImportResponse(t, importPlatformExtensionForTest(t, h, workspaceID, testUserID, secondRaw).Body.Bytes())

	t.Run("returns a read-only internal Agent detail", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		h.GetExtensionManagedSquadInternalAgent(recorder, withURLParams(
			platformExtensionRequest(http.MethodGet, "/api/squads/"+first.Squad.ID+"/internal-agents/"+first.Agents[0].ID, workspaceID, testUserID, nil),
			"workspaceId", workspaceID,
			"id", first.Squad.ID,
			"agentId", first.Agents[0].ID,
		))
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
		}
		var detail struct {
			ID           string `json:"id"`
			Name         string `json:"name"`
			Instructions string `json:"instructions"`
			Role         string `json:"role"`
			Runtime      *struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"runtime"`
			Skills []struct {
				Name string `json:"name"`
			} `json:"skills"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &detail); err != nil {
			t.Fatalf("decode detail: %v", err)
		}
		if detail.ID != first.Agents[0].ID || detail.Name != first.Agents[0].Name || detail.Instructions == "" {
			t.Fatalf("detail = %+v, want imported internal Agent", detail)
		}
		if detail.Role == "" || detail.Runtime == nil || detail.Runtime.ID != runtimeID || len(detail.Skills) == 0 {
			t.Fatalf("detail missing read-only fields: %+v", detail)
		}
	})

	t.Run("rejects an internal Agent owned by another Extension Squad", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		h.GetExtensionManagedSquadInternalAgent(recorder, withURLParams(
			platformExtensionRequest(http.MethodGet, "/api/squads/"+first.Squad.ID+"/internal-agents/"+second.Agents[0].ID, workspaceID, testUserID, nil),
			"workspaceId", workspaceID,
			"id", first.Squad.ID,
			"agentId", second.Agents[0].ID,
		))
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404: %s", recorder.Code, recorder.Body.String())
		}
	})
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	return raw
}
