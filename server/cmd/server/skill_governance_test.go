package main

// Integration tests for the skill ownership / versioning / change-request /
// fork flow added in JEH-216. Exercises the real router + DB so we catch
// transactional gaps that unit tests miss.

import (
	"net/http"
	"sync"
	"testing"
)

// uniqueSkillName returns a workspace-unique skill name. The skill table has
// UNIQUE(workspace_id, name) and integration tests reuse the workspace, so
// each subtest needs a distinct name.
func uniqueSkillName(t *testing.T) string {
	t.Helper()
	return "skill-" + t.Name()
}

// createSkillFor is a thin wrapper around POST /api/skills that returns the
// new skill id + its current_version. Fails the test on non-201.
func createSkillFor(t *testing.T, name string) (id, version string) {
	t.Helper()
	resp := authRequest(t, "POST", "/api/skills?workspace_id="+testWorkspaceID, map[string]any{
		"name":        name,
		"description": "test skill",
		"content":     "# v1\nhello",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("CreateSkill: expected 201, got %d", resp.StatusCode)
	}
	var s map[string]any
	readJSON(t, resp, &s)
	return s["id"].(string), s["current_version"].(string)
}

// TestSkillCreate_SetsOwnerAndSnapshotsBaseline verifies that a fresh skill
// gets owner_id = creator and a v1.0.0 snapshot in skill_version. This was
// the "imported skills had no owner / no version" bug from review.
func TestSkillCreate_SetsOwnerAndSnapshotsBaseline(t *testing.T) {
	id, version := createSkillFor(t, uniqueSkillName(t))
	if version != "1.0.0" {
		t.Fatalf("expected current_version 1.0.0, got %s", version)
	}

	// Owner must be set to the creator.
	resp := authRequest(t, "GET", "/api/skills/"+id, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GetSkill: expected 200, got %d", resp.StatusCode)
	}
	var s map[string]any
	readJSON(t, resp, &s)
	if s["owner_id"] == nil || s["owner_id"] != testUserID {
		t.Fatalf("expected owner_id=%s, got %v", testUserID, s["owner_id"])
	}

	// History must contain the v1.0.0 baseline.
	resp = authRequest(t, "GET", "/api/skills/"+id+"/versions", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ListSkillVersions: expected 200, got %d", resp.StatusCode)
	}
	var versions []map[string]any
	readJSON(t, resp, &versions)
	if len(versions) != 1 || versions[0]["version"] != "1.0.0" {
		t.Fatalf("expected one v1.0.0 baseline, got %#v", versions)
	}
}

// TestSkillChangeRequest_HappyPath exercises propose → approve → snapshot →
// merged across the live HTTP stack.
func TestSkillChangeRequest_HappyPath(t *testing.T) {
	skillID, _ := createSkillFor(t, uniqueSkillName(t))

	// Propose a change.
	resp := authRequest(t, "POST", "/api/skills/"+skillID+"/change-requests", map[string]any{
		"title":            "Bump content",
		"description":      "tweak intro",
		"proposed_version": "1.1.0",
		"proposed_content": "# v2\nhello updated",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("CreateChangeRequest: expected 201, got %d", resp.StatusCode)
	}
	var cr map[string]any
	readJSON(t, resp, &cr)
	crID := cr["id"].(string)
	if cr["status"] != "pending" {
		t.Fatalf("expected status pending, got %v", cr["status"])
	}

	// Approve.
	resp = authRequest(t, "POST", "/api/skills/change-requests/"+crID+"/review", map[string]any{
		"action":  "approve",
		"comment": "lgtm",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Approve: expected 200, got %d", resp.StatusCode)
	}
	var merged map[string]any
	readJSON(t, resp, &merged)
	if merged["status"] != "merged" {
		t.Fatalf("expected status merged, got %v", merged["status"])
	}

	// Skill now reflects new version + content.
	resp = authRequest(t, "GET", "/api/skills/"+skillID, nil)
	var s map[string]any
	readJSON(t, resp, &s)
	if s["current_version"] != "1.1.0" {
		t.Fatalf("expected current_version 1.1.0, got %v", s["current_version"])
	}
	if s["content"] != "# v2\nhello updated" {
		t.Fatalf("expected updated content, got %v", s["content"])
	}

	// History has v1.0.0 + v1.1.0.
	resp = authRequest(t, "GET", "/api/skills/"+skillID+"/versions", nil)
	var versions []map[string]any
	readJSON(t, resp, &versions)
	if len(versions) != 2 {
		t.Fatalf("expected 2 versions in history, got %d: %#v", len(versions), versions)
	}
}

// TestSkillChangeRequest_DoubleApproveIsIdempotent locks down the race the
// CTO review flagged: two concurrent approves must not both succeed and
// must not corrupt the version history. We fire them in goroutines and
// expect exactly one 200 + one 4xx.
func TestSkillChangeRequest_DoubleApproveIsIdempotent(t *testing.T) {
	skillID, _ := createSkillFor(t, uniqueSkillName(t))

	resp := authRequest(t, "POST", "/api/skills/"+skillID+"/change-requests", map[string]any{
		"title":            "race",
		"proposed_version": "1.1.0",
		"proposed_content": "race",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("CreateChangeRequest: expected 201, got %d", resp.StatusCode)
	}
	var cr map[string]any
	readJSON(t, resp, &cr)
	crID := cr["id"].(string)

	const N = 5
	statuses := make([]int, N)
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := authRequest(t, "POST", "/api/skills/change-requests/"+crID+"/review", map[string]any{
				"action": "approve",
			})
			statuses[i] = r.StatusCode
			r.Body.Close()
		}(i)
	}
	wg.Wait()

	successes := 0
	conflicts := 0
	for _, s := range statuses {
		switch {
		case s == http.StatusOK:
			successes++
		case s >= 400 && s < 500:
			conflicts++
		}
	}
	if successes != 1 {
		t.Fatalf("expected exactly 1 successful approve, got %d (statuses=%v)", successes, statuses)
	}
	if successes+conflicts != N {
		t.Fatalf("unexpected statuses (no 5xx allowed): %v", statuses)
	}

	// History should have v1.0.0 + v1.1.0 — never duplicates from the race.
	r := authRequest(t, "GET", "/api/skills/"+skillID+"/versions", nil)
	var versions []map[string]any
	readJSON(t, r, &versions)
	if len(versions) != 2 {
		t.Fatalf("expected 2 versions after race, got %d: %#v", len(versions), versions)
	}
}

// TestSkillChangeRequest_RejectsNonGreaterVersion ensures the semver check
// re-runs at approve time, not just at propose time.
func TestSkillChangeRequest_RejectsNonGreaterVersion(t *testing.T) {
	skillID, _ := createSkillFor(t, uniqueSkillName(t))

	resp := authRequest(t, "POST", "/api/skills/"+skillID+"/change-requests", map[string]any{
		"title":            "first",
		"proposed_version": "1.1.0",
		"proposed_content": "first",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("CR1: expected 201, got %d", resp.StatusCode)
	}
	var cr1 map[string]any
	readJSON(t, resp, &cr1)

	resp = authRequest(t, "POST", "/api/skills/"+skillID+"/change-requests", map[string]any{
		"title":            "second",
		"proposed_version": "1.1.0",
		"proposed_content": "second",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("CR2: expected 201, got %d", resp.StatusCode)
	}
	var cr2 map[string]any
	readJSON(t, resp, &cr2)

	// Approve CR1 → bumps current_version to 1.1.0.
	resp = authRequest(t, "POST", "/api/skills/change-requests/"+cr1["id"].(string)+"/review", map[string]any{
		"action": "approve",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Approve CR1: expected 200, got %d", resp.StatusCode)
	}

	// Approve CR2 → must fail because 1.1.0 is no longer > current.
	resp = authRequest(t, "POST", "/api/skills/change-requests/"+cr2["id"].(string)+"/review", map[string]any{
		"action": "approve",
	})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("Approve CR2: expected 409, got %d", resp.StatusCode)
	}
}

// TestSkillFork_CopiesContentAndRecordsLineage covers the fork = full skill
// row + parent link model: forked skill is independent and its lineage is
// listable from the parent.
func TestSkillFork_CopiesContentAndRecordsLineage(t *testing.T) {
	parentID, _ := createSkillFor(t, uniqueSkillName(t)+"-parent")

	resp := authRequest(t, "POST", "/api/skills/"+parentID+"/forks", map[string]any{
		"name":        uniqueSkillName(t) + "-child",
		"description": "forked variant",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("Fork: expected 201, got %d", resp.StatusCode)
	}
	var forked map[string]any
	readJSON(t, resp, &forked)
	if forked["current_version"] != "1.0.0" {
		t.Fatalf("expected fork to start at v1.0.0, got %v", forked["current_version"])
	}
	if forked["owner_id"] != testUserID {
		t.Fatalf("expected fork owner = caller, got %v", forked["owner_id"])
	}

	// Fork list on parent shows the child.
	resp = authRequest(t, "GET", "/api/skills/"+parentID+"/forks", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ListForks: expected 200, got %d", resp.StatusCode)
	}
	var forks []map[string]any
	readJSON(t, resp, &forks)
	if len(forks) != 1 {
		t.Fatalf("expected 1 fork, got %d", len(forks))
	}
	if forks[0]["forked_skill_id"] != forked["id"] {
		t.Fatalf("fork link mismatch")
	}
}
