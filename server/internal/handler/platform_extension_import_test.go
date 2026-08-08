package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestImportPlatformExtensionCreatesNativeSquadAtomically(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	workspaceID := createPlatformExtensionTestWorkspace(t, "member")
	runtimeID := createPlatformExtensionTestRuntime(t, workspaceID, platformExtensionRuntimeSeed{
		Provider: "platform-agent-cli", Status: "online", LastSeenAt: time.Now(),
		Visibility: "public", OwnerID: createPlatformExtensionOtherUser(t),
	})
	source, raw := twoByTwoPlatformExtensionSource(t, "atomic")
	h := platformExtensionHandlerWithLiveness(platformExtensionFakeLiveness{
		alive: map[string]bool{runtimeID: true}, ok: true,
	})

	recorder := importPlatformExtensionForTest(t, h, workspaceID, testUserID, raw)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("import status = %d, want 201: %s", recorder.Code, recorder.Body.String())
	}
	response := decodePlatformExtensionImportResponse(t, recorder.Body.Bytes())
	if response.Idempotent {
		t.Fatal("first import reported idempotent")
	}
	if response.Release.ExtensionKey != source.Extension.Key || response.Release.Version != source.Extension.Version {
		t.Fatalf("release response = %+v", response.Release)
	}
	if response.Runtime.ID != runtimeID || response.Runtime.Provider != "platform-agent-cli" {
		t.Fatalf("runtime response = %+v, want %s/platform-agent-cli", response.Runtime, runtimeID)
	}
	if response.Squad.Name != source.Extension.Name+" v"+source.Extension.Version {
		t.Fatalf("squad name = %q", response.Squad.Name)
	}
	if len(response.Agents) != 2 || len(response.Skills) != 2 {
		t.Fatalf("response mappings = agents:%+v skills:%+v", response.Agents, response.Skills)
	}

	assertPlatformExtensionNativeResources(t, workspaceID, runtimeID, source, response)
}

func TestImportPlatformExtensionIsIdempotentAndVersionImmutable(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	workspaceID := createPlatformExtensionTestWorkspace(t, "owner")
	runtimeID := createPlatformExtensionTestRuntime(t, workspaceID, platformExtensionRuntimeSeed{
		Provider: "platform-agent-cli", Status: "online", LastSeenAt: time.Now(), Visibility: "private", OwnerID: createPlatformExtensionOtherUser(t),
	})
	_, raw := twoByTwoPlatformExtensionSource(t, "idempotent")
	h := platformExtensionHandlerWithLiveness(platformExtensionFakeLiveness{alive: map[string]bool{runtimeID: true}, ok: true})

	firstRecorder := importPlatformExtensionForTest(t, h, workspaceID, testUserID, raw)
	if firstRecorder.Code != http.StatusCreated {
		t.Fatalf("first import = %d: %s", firstRecorder.Code, firstRecorder.Body.String())
	}
	first := decodePlatformExtensionImportResponse(t, firstRecorder.Body.Bytes())

	secondRecorder := importPlatformExtensionForTest(t, h, workspaceID, testUserID, raw)
	if secondRecorder.Code != http.StatusOK {
		t.Fatalf("idempotent import = %d: %s", secondRecorder.Code, secondRecorder.Body.String())
	}
	second := decodePlatformExtensionImportResponse(t, secondRecorder.Body.Bytes())
	if !second.Idempotent {
		t.Fatal("repeat import did not report idempotent")
	}
	if first.Release.ID != second.Release.ID || first.Squad.ID != second.Squad.ID || first.Runtime.ID != second.Runtime.ID {
		t.Fatalf("repeat mapping changed:\nfirst=%+v\nsecond=%+v", first, second)
	}
	assertPlatformExtensionWorkspaceResourceCounts(t, workspaceID, 1, 2, 2, 4, 1)

	changedSource, err := DecodePlatformExtensionSource(raw)
	if err != nil {
		t.Fatal(err)
	}
	changedSource.Extension.Description = "content changed without a version bump"
	changedRaw, err := json.Marshal(changedSource)
	if err != nil {
		t.Fatal(err)
	}
	conflict := importPlatformExtensionForTest(t, h, workspaceID, testUserID, changedRaw)
	assertPlatformExtensionHTTPError(t, conflict, http.StatusConflict, "EXTENSION_VERSION_IMMUTABLE")
	assertPlatformExtensionWorkspaceResourceCounts(t, workspaceID, 1, 2, 2, 4, 1)
}

func TestConcurrentPlatformExtensionImportsHaveOneWinnerAndOneIdempotentLoser(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	workspaceID := createPlatformExtensionTestWorkspace(t, "owner")
	runtimeID := createPlatformExtensionTestRuntime(t, workspaceID, platformExtensionRuntimeSeed{
		Provider: "platform-agent-cli", Status: "online", LastSeenAt: time.Now(), Visibility: "private", OwnerID: testUserID,
	})
	_, raw := twoByTwoPlatformExtensionSource(t, "concurrent-idempotent")
	h := platformExtensionHandlerWithLiveness(platformExtensionFakeLiveness{alive: map[string]bool{runtimeID: true}, ok: true})

	start := make(chan struct{})
	recorders := make([]*httptest.ResponseRecorder, 2)
	var wait sync.WaitGroup
	for i := range recorders {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			recorders[index] = importPlatformExtensionForTest(t, h, workspaceID, testUserID, raw)
		}(i)
	}
	close(start)
	wait.Wait()

	statuses := []int{recorders[0].Code, recorders[1].Code}
	sort.Ints(statuses)
	if statuses[0] != http.StatusOK || statuses[1] != http.StatusCreated {
		t.Fatalf("concurrent statuses = %v; bodies: %s | %s", statuses, recorders[0].Body.String(), recorders[1].Body.String())
	}
	first := decodePlatformExtensionImportResponse(t, recorders[0].Body.Bytes())
	second := decodePlatformExtensionImportResponse(t, recorders[1].Body.Bytes())
	if first.Release.ID != second.Release.ID || first.Squad.ID != second.Squad.ID {
		t.Fatalf("concurrent imports produced different mappings: %+v / %+v", first, second)
	}
	if first.Idempotent == second.Idempotent {
		t.Fatalf("idempotent flags = %v/%v, want one winner and one loser", first.Idempotent, second.Idempotent)
	}
	assertPlatformExtensionWorkspaceResourceCounts(t, workspaceID, 1, 2, 2, 4, 1)
}

func TestImportPlatformExtensionReturnsRuntimeUnavailableWithoutReservationOrResources(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	workspaceID := createPlatformExtensionTestWorkspace(t, "member")
	_, raw := twoByTwoPlatformExtensionSource(t, "no-runtime")
	h := platformExtensionHandlerWithLiveness(NewNoopLivenessStore())

	recorder := importPlatformExtensionForTest(t, h, workspaceID, testUserID, raw)
	assertPlatformExtensionHTTPError(t, recorder, http.StatusConflict, "PLATFORM_RUNTIME_UNAVAILABLE")
	assertPlatformExtensionWorkspaceResourceCounts(t, workspaceID, 0, 0, 0, 0, 0)
}

func TestImportPlatformExtensionRollsBackReservationWhenRuntimeIsLocked(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	workspaceID := createPlatformExtensionTestWorkspace(t, "owner")
	runtimeID := createPlatformExtensionTestRuntime(t, workspaceID, platformExtensionRuntimeSeed{
		Provider: "platform-agent-cli", Status: "online", LastSeenAt: time.Now(), Visibility: "private", OwnerID: testUserID,
	})
	_, raw := twoByTwoPlatformExtensionSource(t, "locked-runtime")
	h := platformExtensionHandlerWithLiveness(platformExtensionFakeLiveness{alive: map[string]bool{runtimeID: true}, ok: true})

	lockTx, err := testPool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer lockTx.Rollback(context.Background())
	if _, err := lockTx.Exec(context.Background(), `SELECT id FROM agent_runtime WHERE id = $1 FOR UPDATE`, runtimeID); err != nil {
		t.Fatalf("lock runtime: %v", err)
	}

	recorder := importPlatformExtensionForTest(t, h, workspaceID, testUserID, raw)
	assertPlatformExtensionHTTPError(t, recorder, http.StatusConflict, "PLATFORM_RUNTIME_UNAVAILABLE")
	assertPlatformExtensionWorkspaceResourceCounts(t, workspaceID, 0, 0, 0, 0, 0)
}

func TestImportPlatformExtensionRollsBackEveryWriteWhenNativeResourceCreationFails(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	workspaceID := createPlatformExtensionTestWorkspace(t, "owner")
	runtimeID := createPlatformExtensionTestRuntime(t, workspaceID, platformExtensionRuntimeSeed{
		Provider: "platform-agent-cli", Status: "online", LastSeenAt: time.Now(), Visibility: "private", OwnerID: testUserID,
	})
	source, raw := twoByTwoPlatformExtensionSource(t, "rollback")
	conflictingName := wantPlatformExtensionNativeResourceName(source.Extension, source.Skills[0].Name)
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO skill (workspace_id, name, description, content, config, created_by)
		VALUES ($1, $2, '', '', '{}'::jsonb, $3)
	`, workspaceID, conflictingName, testUserID); err != nil {
		t.Fatalf("seed conflicting skill: %v", err)
	}
	h := platformExtensionHandlerWithLiveness(platformExtensionFakeLiveness{alive: map[string]bool{runtimeID: true}, ok: true})

	recorder := importPlatformExtensionForTest(t, h, workspaceID, testUserID, raw)
	assertPlatformExtensionHTTPError(t, recorder, http.StatusInternalServerError, "EXTENSION_IMPORT_FAILED")
	assertPlatformExtensionWorkspaceResourceCounts(t, workspaceID, 0, 0, 1, 0, 0)
}

func TestListAndGetPlatformExtensionsAreWorkspaceScopedOrderedAndDanglingTolerant(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	workspaceID := createPlatformExtensionTestWorkspace(t, "owner")
	runtimeID := createPlatformExtensionTestRuntime(t, workspaceID, platformExtensionRuntimeSeed{
		Provider: "platform-agent-cli", Status: "online", LastSeenAt: time.Now(), Visibility: "private", OwnerID: testUserID,
	})
	h := platformExtensionHandlerWithLiveness(platformExtensionFakeLiveness{alive: map[string]bool{runtimeID: true}, ok: true})

	source1, raw1 := twoByTwoPlatformExtensionSource(t, "listing-first")
	firstRecorder := importPlatformExtensionForTest(t, h, workspaceID, testUserID, raw1)
	if firstRecorder.Code != http.StatusCreated {
		t.Fatalf("first import = %d: %s", firstRecorder.Code, firstRecorder.Body.String())
	}
	first := decodePlatformExtensionImportResponse(t, firstRecorder.Body.Bytes())

	source2, _ := twoByTwoPlatformExtensionSource(t, "listing-second")
	source2.Extension.Version = "2.0.0"
	raw2, err := json.Marshal(source2)
	if err != nil {
		t.Fatal(err)
	}
	secondRecorder := importPlatformExtensionForTest(t, h, workspaceID, testUserID, raw2)
	if secondRecorder.Code != http.StatusCreated {
		t.Fatalf("second import = %d: %s", secondRecorder.Code, secondRecorder.Body.String())
	}
	second := decodePlatformExtensionImportResponse(t, secondRecorder.Body.Bytes())

	if _, err := testPool.Exec(context.Background(), `
		UPDATE platform_extension_release
		SET created_at = CASE id WHEN $1 THEN now() - interval '1 hour' ELSE now() END
		WHERE id IN ($1, $2)
	`, first.Release.ID, second.Release.ID); err != nil {
		t.Fatalf("set deterministic release ordering: %v", err)
	}

	listRecorder := httptest.NewRecorder()
	listReq := platformExtensionRequest(http.MethodGet, "/api/extensions", workspaceID, testUserID, nil)
	h.ListPlatformExtensions(listRecorder, listReq)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list = %d: %s", listRecorder.Code, listRecorder.Body.String())
	}
	var listed []platformExtensionMappingResponse
	if err := json.NewDecoder(listRecorder.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed) != 2 || listed[0].Release.ID != second.Release.ID || listed[1].Release.ID != first.Release.ID {
		t.Fatalf("release order = %+v, want newest then oldest", listed)
	}

	detailRecorder := httptest.NewRecorder()
	detailReq := platformExtensionRequest(http.MethodGet, "/api/extensions/"+first.Release.ID, workspaceID, testUserID, nil)
	detailReq = withURLParam(detailReq, "id", first.Release.ID)
	h.GetPlatformExtension(detailRecorder, detailReq)
	if detailRecorder.Code != http.StatusOK {
		t.Fatalf("detail = %d: %s", detailRecorder.Code, detailRecorder.Body.String())
	}
	detail := decodePlatformExtensionDetailResponse(t, detailRecorder.Body.Bytes())
	if detail.Release.ID != first.Release.ID || detail.Manifest.Extension.Key != source1.Extension.Key || len(detail.Agents) != 2 || len(detail.Skills) != 2 {
		t.Fatalf("detail response = %+v", detail)
	}

	deletePlatformExtensionNativeResources(t, first)
	danglingRecorder := httptest.NewRecorder()
	danglingReq := platformExtensionRequest(http.MethodGet, "/api/extensions/"+first.Release.ID, workspaceID, testUserID, nil)
	danglingReq = withURLParam(danglingReq, "id", first.Release.ID)
	h.GetPlatformExtension(danglingRecorder, danglingReq)
	if danglingRecorder.Code != http.StatusOK {
		t.Fatalf("dangling detail = %d: %s", danglingRecorder.Code, danglingRecorder.Body.String())
	}
	dangling := decodePlatformExtensionDetailResponse(t, danglingRecorder.Body.Bytes())
	if dangling.Runtime.ID != first.Runtime.ID || dangling.Squad.ID != first.Squad.ID || len(dangling.Agents) != 2 || len(dangling.Skills) != 2 {
		t.Fatalf("dangling audit mapping was lost: %+v", dangling)
	}

	otherWorkspaceID := createPlatformExtensionTestWorkspace(t, "owner")
	crossWorkspace := httptest.NewRecorder()
	crossReq := platformExtensionRequest(http.MethodGet, "/api/extensions/"+first.Release.ID, otherWorkspaceID, testUserID, nil)
	crossReq = withURLParam(crossReq, "id", first.Release.ID)
	h.GetPlatformExtension(crossWorkspace, crossReq)
	assertPlatformExtensionHTTPError(t, crossWorkspace, http.StatusNotFound, "EXTENSION_NOT_FOUND")
}

func TestImportPlatformExtensionEnforcesFiveMiBAndStrictContractErrors(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	t.Run("exactly five MiB is accepted and one extra byte is rejected", func(t *testing.T) {
		workspaceID := createPlatformExtensionTestWorkspace(t, "owner")
		runtimeID := createPlatformExtensionTestRuntime(t, workspaceID, platformExtensionRuntimeSeed{
			Provider: "platform-agent-cli", Status: "online", LastSeenAt: time.Now(), Visibility: "private", OwnerID: testUserID,
		})
		h := platformExtensionHandlerWithLiveness(platformExtensionFakeLiveness{alive: map[string]bool{runtimeID: true}, ok: true})
		_, raw := twoByTwoPlatformExtensionSource(t, "five-mib")
		atLimit := append(bytes.Clone(raw), bytes.Repeat([]byte(" "), PlatformExtensionMaxImportBytes-len(raw))...)
		if len(atLimit) != PlatformExtensionMaxImportBytes {
			t.Fatalf("test body size = %d", len(atLimit))
		}
		accepted := importPlatformExtensionForTest(t, h, workspaceID, testUserID, atLimit)
		if accepted.Code != http.StatusCreated {
			t.Fatalf("exact limit = %d: %s", accepted.Code, accepted.Body.String())
		}

		overLimit := append(atLimit, ' ')
		rejected := importPlatformExtensionForTest(t, h, workspaceID, testUserID, overLimit)
		assertPlatformExtensionHTTPError(t, rejected, http.StatusBadRequest, "EXTENSION_INVALID")
	})

	t.Run("source and bundle failures have stable business codes", func(t *testing.T) {
		workspaceID := createPlatformExtensionTestWorkspace(t, "owner")
		runtimeID := createPlatformExtensionTestRuntime(t, workspaceID, platformExtensionRuntimeSeed{
			Provider: "platform-agent-cli", Status: "online", LastSeenAt: time.Now(), Visibility: "private", OwnerID: testUserID,
		})
		h := platformExtensionHandlerWithLiveness(platformExtensionFakeLiveness{alive: map[string]bool{runtimeID: true}, ok: true})
		_, validSource := twoByTwoPlatformExtensionSource(t, "strict-errors")
		validBundle := compiledPlatformExtensionBundleForTest(t, validSource)

		tests := []struct {
			name   string
			body   []byte
			status int
			code   string
		}{
			{name: "duplicate key", body: bytes.Replace(validSource, []byte(`"leader":`), []byte(`"leader":"shadow","leader":`), 1), status: 400, code: "EXTENSION_INVALID"},
			{name: "unknown field", body: bytes.Replace(validSource, []byte(`"leader":`), []byte(`"unknown":true,"leader":`), 1), status: 400, code: "EXTENSION_INVALID"},
			{name: "trailing JSON", body: append(bytes.Clone(validSource), []byte(` {}`)...), status: 400, code: "EXTENSION_INVALID"},
			{name: "policy mismatch", body: bytes.Replace(validSource, []byte(`".flow"`), []byte(`".workflow"`), 1), status: 400, code: "COMMAND_SUFFIX_POLICY_MISMATCH"},
			{name: "tool command", body: platformExtensionSourceWithToolCommand(t, validSource), status: 422, code: "TOOL_COMMAND_UNSUPPORTED"},
			{name: "bundle digest mismatch", body: bytes.Replace(validBundle, []byte(`"description": "Research and summarize a topic."`), []byte(`"description": "tampered"`), 1), status: 400, code: "EXTENSION_DIGEST_MISMATCH"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				recorder := importPlatformExtensionForTest(t, h, workspaceID, testUserID, tt.body)
				assertPlatformExtensionHTTPError(t, recorder, tt.status, tt.code)
			})
		}
	})
}

func TestImportPlatformExtensionAcceptsCanonicalBundleAndInjectedTrustedPolicy(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	t.Run("canonical bundle persists canonical manifest", func(t *testing.T) {
		workspaceID := createPlatformExtensionTestWorkspace(t, "owner")
		runtimeID := createPlatformExtensionTestRuntime(t, workspaceID, platformExtensionRuntimeSeed{
			Provider: "platform-agent-cli", Status: "online", LastSeenAt: time.Now(), Visibility: "private", OwnerID: testUserID,
		})
		h := platformExtensionHandlerWithLiveness(platformExtensionFakeLiveness{alive: map[string]bool{runtimeID: true}, ok: true})
		_, sourceRaw := twoByTwoPlatformExtensionSource(t, "bundle-input")
		bundleRaw := compiledPlatformExtensionBundleForTest(t, sourceRaw)

		recorder := importPlatformExtensionForTest(t, h, workspaceID, testUserID, bundleRaw)
		if recorder.Code != http.StatusCreated {
			t.Fatalf("bundle import = %d: %s", recorder.Code, recorder.Body.String())
		}
		response := decodePlatformExtensionImportResponse(t, recorder.Body.Bytes())
		var stored []byte
		if err := testPool.QueryRow(context.Background(), `SELECT manifest::text FROM platform_extension_release WHERE id = $1`, response.Release.ID).Scan(&stored); err != nil {
			t.Fatalf("load stored manifest: %v", err)
		}
		var got, want any
		if err := json.Unmarshal(stored, &got); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(bundleRaw, &want); err != nil {
			t.Fatal(err)
		}
		if !platformExtensionJSONEqual(got, want) {
			t.Fatalf("stored manifest differs from canonical bundle\nstored=%s\nwant=%s", stored, bundleRaw)
		}
	})

	t.Run("handler policy is caller-injected", func(t *testing.T) {
		workspaceID := createPlatformExtensionTestWorkspace(t, "owner")
		runtimeID := createPlatformExtensionTestRuntime(t, workspaceID, platformExtensionRuntimeSeed{
			Provider: "platform-agent-cli", Status: "online", LastSeenAt: time.Now(), Visibility: "private", OwnerID: testUserID,
		})
		policy := PlatformExtensionPolicy{CommandSuffixes: PlatformExtensionCommandSuffixes{
			Flow: []string{".workflow"}, Tool: []string{".action"},
		}}
		source, _ := twoByTwoPlatformExtensionSource(t, "injected-policy")
		source.CommandSuffixes = policy.CommandSuffixes
		source.Commands[0].Name = "delegate.workflow"
		raw, err := json.Marshal(source)
		if err != nil {
			t.Fatal(err)
		}
		h := platformExtensionHandlerWithLiveness(platformExtensionFakeLiveness{alive: map[string]bool{runtimeID: true}, ok: true})
		h.PlatformExtensionPolicy = policy

		recorder := importPlatformExtensionForTest(t, h, workspaceID, testUserID, raw)
		if recorder.Code != http.StatusCreated {
			t.Fatalf("custom policy import = %d: %s", recorder.Code, recorder.Body.String())
		}
	})

	t.Run("invalid injected policy is a server error", func(t *testing.T) {
		workspaceID := createPlatformExtensionTestWorkspace(t, "owner")
		_, raw := twoByTwoPlatformExtensionSource(t, "invalid-policy")
		h := platformExtensionHandlerWithLiveness(NewNoopLivenessStore())
		h.PlatformExtensionPolicy = PlatformExtensionPolicy{CommandSuffixes: PlatformExtensionCommandSuffixes{Flow: []string{".flow"}}}
		recorder := importPlatformExtensionForTest(t, h, workspaceID, testUserID, raw)
		assertPlatformExtensionHTTPError(t, recorder, http.StatusInternalServerError, "COMMAND_SUFFIX_POLICY_INVALID")
	})
}

func TestPlatformExtensionHandlersRequireWorkspaceMembership(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	var workspaceID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('No Membership Extension Test', $1, '', 'NME') RETURNING id
	`, "no-extension-membership-"+randomID()).Scan(&workspaceID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM platform_extension_release WHERE workspace_id = $1`, workspaceID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
	})
	_, raw := twoByTwoPlatformExtensionSource(t, "no-membership")

	for _, tt := range []struct {
		name string
		call func(http.ResponseWriter, *http.Request)
		req  *http.Request
	}{
		{name: "import", call: testHandler.ImportPlatformExtension, req: platformExtensionRequest(http.MethodPost, "/api/extensions/import", workspaceID, testUserID, raw)},
		{name: "list", call: testHandler.ListPlatformExtensions, req: platformExtensionRequest(http.MethodGet, "/api/extensions", workspaceID, testUserID, nil)},
		{name: "detail", call: testHandler.GetPlatformExtension, req: withURLParam(platformExtensionRequest(http.MethodGet, "/api/extensions/00000000-0000-0000-0000-000000000001", workspaceID, testUserID, nil), "id", "00000000-0000-0000-0000-000000000001")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			tt.call(recorder, tt.req)
			if recorder.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404: %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

type platformExtensionReleaseResponse struct {
	ID           string `json:"id"`
	ExtensionKey string `json:"extension_key"`
	Version      string `json:"version"`
	Digest       string `json:"digest"`
}

type platformExtensionRuntimeResponse struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Name     string `json:"name"`
}

type platformExtensionSquadResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type platformExtensionAgentMappingResponse struct {
	SourceKey string `json:"source_key"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	Leader    bool   `json:"leader"`
}

type platformExtensionSkillMappingResponse struct {
	SourceKey string `json:"source_key"`
	ID        string `json:"id"`
	Name      string `json:"name"`
}

type platformExtensionMappingResponse struct {
	Release platformExtensionReleaseResponse        `json:"release"`
	Runtime platformExtensionRuntimeResponse        `json:"runtime"`
	Squad   platformExtensionSquadResponse          `json:"squad"`
	Agents  []platformExtensionAgentMappingResponse `json:"agents"`
	Skills  []platformExtensionSkillMappingResponse `json:"skills"`
}

type platformExtensionImportTestResponse struct {
	platformExtensionMappingResponse
	Idempotent bool `json:"idempotent"`
}

type platformExtensionDetailTestResponse struct {
	platformExtensionMappingResponse
	Manifest PlatformExtensionBundle `json:"manifest"`
}

func twoByTwoPlatformExtensionSource(t *testing.T, suffix string) (PlatformExtensionSource, []byte) {
	t.Helper()
	source := readPlatformExtensionSource(t)
	unique := suffix + "-" + randomID()[:8]
	source.Extension.Key = "research-team-" + unique
	source.Extension.Name = "Research Team " + unique
	source.Agents = append(source.Agents, PlatformExtensionAgent{
		Key: "analyst", Name: "Analyst", Description: "Analyzes gathered evidence.", Prompt: "Analyze the evidence and report gaps.",
	})
	source.Skills = append(source.Skills, PlatformExtensionSkill{
		Key: "evidence-writing", Name: "Evidence Writing", Description: "Write grounded conclusions.",
		Files: []PlatformExtensionSkillFile{
			{Path: "SKILL.md", Content: "---\nname: evidence-writing\ndescription: Write grounded conclusions.\n---\n\nWrite concise evidence-backed conclusions."},
			{Path: "references/style.md", Content: "Cite claims next to evidence."},
		},
	})
	raw, err := json.Marshal(source)
	if err != nil {
		t.Fatalf("marshal two-by-two source: %v", err)
	}
	return source, raw
}

func compiledPlatformExtensionBundleForTest(t *testing.T, sourceRaw []byte) []byte {
	t.Helper()
	source, err := DecodePlatformExtensionSource(sourceRaw)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := CompilePlatformExtension(source)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := CanonicalPlatformExtensionBundleJSON(bundle)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func platformExtensionSourceWithToolCommand(t *testing.T, sourceRaw []byte) []byte {
	t.Helper()
	source, err := DecodePlatformExtensionSource(sourceRaw)
	if err != nil {
		t.Fatal(err)
	}
	source.Commands = append(source.Commands, PlatformExtensionCommand{
		Name: "shell.tool", Description: "Unsupported tool command.", Content: "Run a tool.", Metadata: json.RawMessage(`{}`),
	})
	raw, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func importPlatformExtensionForTest(t *testing.T, h *Handler, workspaceID, userID string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	h.ImportPlatformExtension(recorder, platformExtensionRequest(http.MethodPost, "/api/extensions/import", workspaceID, userID, body))
	return recorder
}

func platformExtensionRequest(method, path, workspaceID, userID string, body []byte) *http.Request {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Workspace-ID", workspaceID)
	req.Header.Set("X-User-ID", userID)
	return req
}

func decodePlatformExtensionImportResponse(t *testing.T, body []byte) platformExtensionImportTestResponse {
	t.Helper()
	var response platformExtensionImportTestResponse
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode import response: %v\n%s", err, body)
	}
	return response
}

func decodePlatformExtensionDetailResponse(t *testing.T, body []byte) platformExtensionDetailTestResponse {
	t.Helper()
	var response platformExtensionDetailTestResponse
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode detail response: %v\n%s", err, body)
	}
	return response
}

func assertPlatformExtensionHTTPError(t *testing.T, recorder *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if recorder.Code != status {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, status, recorder.Body.String())
	}
	var response struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if response.Code != code || response.Error == "" {
		t.Fatalf("error response = %+v, want code %s and non-empty error", response, code)
	}
}

func assertPlatformExtensionWorkspaceResourceCounts(t *testing.T, workspaceID string, releases, agents, skills, bindings, squads int) {
	t.Helper()
	queries := []struct {
		name string
		sql  string
		want int
	}{
		{name: "releases", sql: `SELECT count(*) FROM platform_extension_release WHERE workspace_id = $1`, want: releases},
		{name: "agents", sql: `SELECT count(*) FROM agent WHERE workspace_id = $1`, want: agents},
		{name: "skills", sql: `SELECT count(*) FROM skill WHERE workspace_id = $1`, want: skills},
		{name: "bindings", sql: `SELECT count(*) FROM agent_skill ask JOIN agent a ON a.id = ask.agent_id WHERE a.workspace_id = $1`, want: bindings},
		{name: "squads", sql: `SELECT count(*) FROM squad WHERE workspace_id = $1`, want: squads},
	}
	for _, query := range queries {
		var got int
		if err := testPool.QueryRow(context.Background(), query.sql, workspaceID).Scan(&got); err != nil {
			t.Fatalf("count %s: %v", query.name, err)
		}
		if got != query.want {
			t.Fatalf("%s count = %d, want %d", query.name, got, query.want)
		}
	}
}

func assertPlatformExtensionNativeResources(
	t *testing.T,
	workspaceID string,
	runtimeID string,
	source PlatformExtensionSource,
	response platformExtensionImportTestResponse,
) {
	t.Helper()
	bundle, err := CompilePlatformExtension(source)
	if err != nil {
		t.Fatal(err)
	}
	assertPlatformExtensionWorkspaceResourceCounts(t, workspaceID, 1, 2, 2, 4, 1)

	type agentRow struct {
		ID, Name, Description, Instructions, RuntimeID, RuntimeMode string
		RuntimeConfig, CustomEnv, CustomArgs                        []byte
	}
	rows, err := testPool.Query(context.Background(), `
		SELECT id, name, description, instructions, runtime_id, runtime_mode, runtime_config, custom_env, custom_args
		FROM agent WHERE workspace_id = $1 ORDER BY name
	`, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	agents := make([]agentRow, 0, 2)
	for rows.Next() {
		var row agentRow
		if err := rows.Scan(&row.ID, &row.Name, &row.Description, &row.Instructions, &row.RuntimeID, &row.RuntimeMode, &row.RuntimeConfig, &row.CustomEnv, &row.CustomArgs); err != nil {
			t.Fatal(err)
		}
		agents = append(agents, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(agents) != 2 {
		t.Fatalf("agents = %+v", agents)
	}
	agentByName := make(map[string]agentRow, len(agents))
	for _, agent := range agents {
		agentByName[agent.Name] = agent
		if agent.RuntimeID != runtimeID || agent.RuntimeMode != "local" {
			t.Fatalf("agent runtime = %s/%s, want %s/local", agent.RuntimeID, agent.RuntimeMode, runtimeID)
		}
		if string(agent.CustomEnv) != `{}` || string(agent.CustomArgs) != `[]` {
			t.Fatalf("agent custom command fields = env:%s args:%s", agent.CustomEnv, agent.CustomArgs)
		}
		var config struct {
			PlatformAgent struct {
				SchemaVersion string `json:"schema_version"`
				Extension     struct {
					Key       string `json:"key"`
					Version   string `json:"version"`
					ReleaseID string `json:"release_id"`
					Digest    string `json:"digest"`
				} `json:"extension"`
				Agent struct {
					SourceKey string `json:"source_key"`
				} `json:"agent"`
				Commands []PlatformExtensionCommand `json:"commands"`
			} `json:"platform_agent"`
		}
		decoder := json.NewDecoder(bytes.NewReader(agent.RuntimeConfig))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&config); err != nil {
			t.Fatalf("decode agent runtime_config: %v\n%s", err, agent.RuntimeConfig)
		}
		if config.PlatformAgent.SchemaVersion != "platform-agent.runtime-context/v1" ||
			config.PlatformAgent.Extension.Key != source.Extension.Key ||
			config.PlatformAgent.Extension.Version != source.Extension.Version ||
			config.PlatformAgent.Extension.ReleaseID != response.Release.ID ||
			config.PlatformAgent.Extension.Digest != response.Release.Digest ||
			len(config.PlatformAgent.Commands) != 1 || config.PlatformAgent.Commands[0].Name != "summarize" {
			t.Fatalf("runtime_config = %+v", config)
		}
		if strings.Contains(string(agent.RuntimeConfig), "delegate.flow") {
			t.Fatalf("flow command leaked into runtime config: %s", agent.RuntimeConfig)
		}
	}
	for _, sourceAgent := range source.Agents {
		name := wantPlatformExtensionNativeResourceName(source.Extension, sourceAgent.Name)
		row, ok := agentByName[name]
		if !ok {
			t.Fatalf("missing native agent %q in %+v", name, agents)
		}
		if row.Description != sourceAgent.Description || row.Instructions != sourceAgent.Prompt {
			t.Fatalf("agent %q fields = description:%q instructions:%q", name, row.Description, row.Instructions)
		}
	}

	for _, sourceSkill := range source.Skills {
		name := wantPlatformExtensionNativeResourceName(source.Extension, sourceSkill.Name)
		var skillID, content string
		if err := testPool.QueryRow(context.Background(), `SELECT id, content FROM skill WHERE workspace_id = $1 AND name = $2`, workspaceID, name).Scan(&skillID, &content); err != nil {
			t.Fatalf("load skill %q: %v", name, err)
		}
		if content != sourceSkill.Files[0].Content {
			t.Fatalf("skill %q content = %q", name, content)
		}
		var supportingFiles int
		if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM skill_file WHERE skill_id = $1`, skillID).Scan(&supportingFiles); err != nil {
			t.Fatal(err)
		}
		if supportingFiles != len(sourceSkill.Files)-1 {
			t.Fatalf("skill %q supporting files = %d, want %d", name, supportingFiles, len(sourceSkill.Files)-1)
		}
	}

	var squadName, squadDescription, squadLeaderID, squadInstructions string
	if err := testPool.QueryRow(context.Background(), `
		SELECT name, description, leader_id, instructions FROM squad WHERE id = $1 AND workspace_id = $2
	`, response.Squad.ID, workspaceID).Scan(&squadName, &squadDescription, &squadLeaderID, &squadInstructions); err != nil {
		t.Fatalf("load imported squad: %v", err)
	}
	if squadName != source.Extension.Name+" v"+source.Extension.Version || squadDescription != source.Extension.Description || squadInstructions != bundle.SquadInstructions {
		t.Fatalf("squad fields = name:%q description:%q instructions:%q", squadName, squadDescription, squadInstructions)
	}
	if strings.Contains(squadInstructions, "Summary command.") || strings.Contains(squadInstructions, "\n- summarize\n") {
		t.Fatalf("runtime command leaked into squad instructions: %q", squadInstructions)
	}
	var members []struct{ ID, Role string }
	memberRows, err := testPool.Query(context.Background(), `SELECT member_id, role FROM squad_member WHERE squad_id = $1 ORDER BY member_id`, response.Squad.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer memberRows.Close()
	for memberRows.Next() {
		var member struct{ ID, Role string }
		if err := memberRows.Scan(&member.ID, &member.Role); err != nil {
			t.Fatal(err)
		}
		members = append(members, member)
	}
	if len(members) != 2 {
		t.Fatalf("squad members = %+v", members)
	}
	for _, member := range members {
		wantRole := "member"
		if member.ID == squadLeaderID {
			wantRole = "leader"
		}
		if member.Role != wantRole {
			t.Fatalf("squad member %s role = %q, want %q", member.ID, member.Role, wantRole)
		}
	}

	var resources []byte
	if err := testPool.QueryRow(context.Background(), `SELECT resources FROM platform_extension_release WHERE id = $1`, response.Release.ID).Scan(&resources); err != nil {
		t.Fatalf("load release resources: %v", err)
	}
	var audit platformExtensionMappingResponse
	if err := json.Unmarshal(resources, &audit); err != nil {
		t.Fatalf("decode release resources: %v\n%s", err, resources)
	}
	if audit.Runtime.ID != response.Runtime.ID || audit.Squad.ID != response.Squad.ID || len(audit.Agents) != 2 || len(audit.Skills) != 2 {
		t.Fatalf("release audit mapping = %+v", audit)
	}
}

func wantPlatformExtensionNativeResourceName(extension PlatformExtension, resourceName string) string {
	return fmt.Sprintf("%s v%s / %s", extension.Name, extension.Version, resourceName)
}

func deletePlatformExtensionNativeResources(t *testing.T, response platformExtensionImportTestResponse) {
	t.Helper()
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `DELETE FROM squad WHERE id = $1`, response.Squad.ID); err != nil {
		t.Fatalf("delete imported squad: %v", err)
	}
	for _, agent := range response.Agents {
		if _, err := testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, agent.ID); err != nil {
			t.Fatalf("delete imported agent: %v", err)
		}
	}
	for _, skill := range response.Skills {
		if _, err := testPool.Exec(ctx, `DELETE FROM skill WHERE id = $1`, skill.ID); err != nil {
			t.Fatalf("delete imported skill: %v", err)
		}
	}
}

func platformExtensionJSONEqual(left, right any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}
