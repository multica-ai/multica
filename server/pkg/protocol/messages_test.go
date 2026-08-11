package protocol

import (
	"encoding/json"
	"os"
	"testing"
)

// TestWireTypesDecodeCanonicalFixtures decodes the V5-5 canonical fixture
// response subtrees into the typed wire objects. This is the byte-exact
// contract bridge: the committed fixtures file (SHA-256
// 28765E18F3715EC7484EC69125264F14D2E18D3C7F9C501892DB90F963680991) must
// decode into these types without field loss.
func TestWireTypesDecodeCanonicalFixtures(t *testing.T) {
	raw, err := os.ReadFile("../../internal/handler/testdata/memoryhub/wire-v1/fixtures.json")
	if err != nil {
		t.Fatalf("read fixtures: %v", err)
	}
	var doc struct {
		Responses map[string]json.RawMessage `json:"responses"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse fixtures: %v", err)
	}

	// config response
	var cfg ConfigResponse
	if err := json.Unmarshal(doc.Responses["config"], &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if cfg.SchemaVersion != 1 || !cfg.Config.HasKey || cfg.Capabilities.SchemaVersion != 1 {
		t.Fatalf("unexpected config: %+v", cfg)
	}

	// binding response
	var binding BindingResponse
	if err := json.Unmarshal(doc.Responses["binding"], &binding); err != nil {
		t.Fatalf("decode binding: %v", err)
	}
	if binding.Binding.Status != MemoryHubBindingBound || binding.Binding.Version != 4 {
		t.Fatalf("unexpected binding: %+v", binding.Binding)
	}
	if binding.Binding.RemoteRef.AgentID == nil || *binding.Binding.RemoteRef.AgentID != "agent-9" {
		t.Fatalf("unexpected remote ref: %+v", binding.Binding.RemoteRef)
	}

	// binding list
	var list BindingListResponse
	if err := json.Unmarshal(doc.Responses["binding_list"], &list); err != nil {
		t.Fatalf("decode binding list: %v", err)
	}
	if list.Page.HasMore || list.Page.NextCursor != nil {
		t.Fatalf("unexpected page: %+v", list.Page)
	}

	// health response
	var health HealthResponse
	if err := json.Unmarshal(doc.Responses["health"], &health); err != nil {
		t.Fatalf("decode health: %v", err)
	}
	if !health.Services.MemoryCore.OK || health.Services.Knowledge.ErrorCode == nil {
		t.Fatalf("unexpected health: %+v", health.Services)
	}

	// candidate list
	var candidates CandidateListResponse
	if err := json.Unmarshal(doc.Responses["candidate_list"], &candidates); err != nil {
		t.Fatalf("decode candidates: %v", err)
	}
	if len(candidates.Candidates) != 1 || candidates.Candidates[0].RemoteID != "agent-9" {
		t.Fatalf("unexpected candidates: %+v", candidates.Candidates)
	}

	// memory docket
	var docket MemoryDocketResponse
	if err := json.Unmarshal(doc.Responses["memory_docket"], &docket); err != nil {
		t.Fatalf("decode docket: %v", err)
	}
	if len(docket.Docket.Items) != 1 || docket.Docket.Items[0].State != MemoryItemActive {
		t.Fatalf("unexpected docket: %+v", docket.Docket)
	}

	// evidence aggregate
	var ev ExecutionEvidenceResponse
	if err := json.Unmarshal(doc.Responses["execution_evidence"], &ev); err != nil {
		t.Fatalf("decode evidence: %v", err)
	}
	if ev.Record.ReviewVersion < 1 || ev.Record.ReviewState == "" {
		t.Fatalf("unexpected record: %+v", ev.Record)
	}

	// evidence score
	var score EvidenceScoreResponse
	if err := json.Unmarshal(doc.Responses["evidence_score"], &score); err != nil {
		t.Fatalf("decode score: %v", err)
	}
	if score.Score.Overall < 0 || score.Score.Overall > 100 {
		t.Fatalf("unexpected score: %+v", score.Score)
	}

	// error envelope (inner MemoryHubError.schema_version must be 1)
	var errResp ErrorResponse
	if err := json.Unmarshal(doc.Responses["error"], &errResp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if errResp.Error.SchemaVersion != 1 || errResp.Error.Code != "binding_transition_invalid" {
		t.Fatalf("unexpected error: %+v", errResp.Error)
	}
}

// TestReviewRepairFixtureDecodes decodes the V6-6 companion fixture request
// and success record (SHA-256
// 554F300DD5F599FA0EC2014A81F55372B6741F9DA9397502877A096A6ADAE446).
func TestReviewRepairFixtureDecodes(t *testing.T) {
	raw, err := os.ReadFile("../../internal/handler/testdata/memoryhub/wire-v1/review-repair-v1.6.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var doc struct {
		Request ReviewRepairRequest `json:"request"`
		Success struct {
			Body ReviewRepairResponse `json:"body"`
		} `json:"success"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	if doc.Request.ExpectedReviewVersion != 2 {
		t.Fatalf("unexpected request: %+v", doc.Request)
	}
	rec := doc.Success.Body.Record
	if rec.ReviewState != ReviewStatePending || rec.ReviewVersion != 3 {
		t.Fatalf("unexpected repaired record: review_state=%s version=%d", rec.ReviewState, rec.ReviewVersion)
	}
	if rec.ReviewAttempt != 0 || rec.ReviewNextWakeup == nil {
		t.Fatalf("unexpected repaired fields: %+v", rec)
	}
}
