package credentials

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
)

// fixedChecker remembers whether it was invoked and returns a fixed answer.
type fixedChecker struct {
	decision PolicyDecision
	invoked  bool
}

func (r *fixedChecker) Check(_ context.Context, _ PolicyRequest) PolicyDecision {
	r.invoked = true
	return r.decision
}

func sampleReq() PolicyRequest {
	return PolicyRequest{
		WorkspaceID:    mustUUID("11111111-1111-1111-1111-111111111111"),
		CredentialID:   mustUUID("22222222-2222-2222-2222-222222222222"),
		CredentialType: Type("openai"),
		Permission:     PermReveal,
		ActorType:      "member",
		ActorID:        mustUUID("33333333-3333-3333-3333-333333333333"),
	}
}

func mustUUID(s string) pgtype.UUID {
	id, err := util.ParseUUID(s)
	if err != nil {
		panic(err)
	}
	return id
}

func TestCutover_PersonaModeAnswersPersonaOnly(t *testing.T) {
	persona := &fixedChecker{decision: Allow("persona allow")}
	multica := &fixedChecker{decision: Deny("multica deny")}
	c := NewCutoverPolicyChecker(persona, multica, PermissionEnginePersona, 100, slog.Default())

	got := c.Check(context.Background(), sampleReq())

	if !got.Allowed || got.Reason != "persona allow" {
		t.Fatalf("expected persona's allow decision, got %+v", got)
	}
	if !persona.invoked {
		t.Fatal("persona should have been consulted")
	}
	if multica.invoked {
		t.Fatal("multica must NOT run in persona mode (no shadow on rollback path)")
	}
}

func TestCutover_MulticaModeAnswersMultica(t *testing.T) {
	persona := &fixedChecker{decision: Allow("persona allow")}
	multica := &fixedChecker{decision: Deny("multica deny")}
	c := NewCutoverPolicyChecker(persona, multica, PermissionEngineMultica, 100, slog.Default())

	got := c.Check(context.Background(), sampleReq())

	if got.Allowed || got.Reason != "multica deny" {
		t.Fatalf("expected multica's deny decision, got %+v", got)
	}
	if !multica.invoked {
		t.Fatal("multica should have answered")
	}
	if !persona.invoked {
		t.Fatal("persona should still be sampled for comparison at 100%")
	}
}

func TestCutover_ParallelModeAnswersPersonaButShadowsMultica(t *testing.T) {
	persona := &fixedChecker{decision: Allow("persona allow")}
	multica := &fixedChecker{decision: Allow("multica allow")}
	c := NewCutoverPolicyChecker(persona, multica, PermissionEngineParallel, 100, slog.Default())

	got := c.Check(context.Background(), sampleReq())

	if !got.Allowed || got.Reason != "persona allow" {
		t.Fatalf("parallel mode must return persona's answer, got %+v", got)
	}
	if !persona.invoked || !multica.invoked {
		t.Fatalf("parallel mode at 100%% sample must run both engines (persona=%v multica=%v)", persona.invoked, multica.invoked)
	}
}

func TestCutover_ZeroSampleSkipsShadowEngine(t *testing.T) {
	persona := &fixedChecker{decision: Allow("persona allow")}
	multica := &fixedChecker{decision: Deny("multica deny")}
	c := NewCutoverPolicyChecker(persona, multica, PermissionEngineParallel, 0, slog.Default())

	got := c.Check(context.Background(), sampleReq())

	if !got.Allowed {
		t.Fatalf("expected persona allow, got %+v", got)
	}
	if multica.invoked {
		t.Fatal("0%% sample must not run the shadow engine")
	}
}

func TestCutover_UnknownModeFallsBackToPersona(t *testing.T) {
	persona := &fixedChecker{decision: Allow("persona allow")}
	multica := &fixedChecker{decision: Deny("multica deny")}
	c := NewCutoverPolicyChecker(persona, multica, PermissionEngineMode("garbage"), 100, slog.Default())

	got := c.Check(context.Background(), sampleReq())
	if !got.Allowed || got.Reason != "persona allow" {
		t.Fatalf("unknown mode must default to persona, got %+v", got)
	}
	if multica.invoked {
		t.Fatal("unknown mode (treated as persona) must not run multica")
	}
}

func TestCutover_NilCheckerDeniesByDefault(t *testing.T) {
	var c *CutoverPolicyChecker
	got := c.Check(context.Background(), sampleReq())
	if got.Allowed {
		t.Fatalf("nil cutover checker must deny, got %+v", got)
	}
}

func TestCutover_MismatchIsLoggedAsWarning(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	persona := &fixedChecker{decision: Allow("persona allow")}
	multica := &fixedChecker{decision: Deny("multica deny")}
	c := NewCutoverPolicyChecker(persona, multica, PermissionEngineMultica, 100, log)

	_ = c.Check(context.Background(), sampleReq())

	out := buf.String()
	if !strings.Contains(out, "permission engine mismatch") {
		t.Fatalf("divergent decisions must log a mismatch warning, got: %s", out)
	}
	// verify it is logged at WARN and carries both engine answers
	dec := json.NewDecoder(strings.NewReader(out))
	foundWarn := false
	for {
		var entry map[string]any
		if err := dec.Decode(&entry); err != nil {
			break
		}
		if entry["msg"] == "permission engine mismatch" {
			foundWarn = true
			if entry["level"] != "WARN" {
				t.Errorf("mismatch should be WARN, got level=%v", entry["level"])
			}
			if entry["persona_allowed"] != true || entry["multica_allowed"] != false {
				t.Errorf("mismatch log must carry both engine answers, got %+v", entry)
			}
			if entry["answering_engine"] != "multica" {
				t.Errorf("answering_engine should be multica, got %v", entry["answering_engine"])
			}
		}
	}
	if !foundWarn {
		t.Fatal("expected a WARN-level mismatch entry")
	}
}

func TestCutover_MatchIsLoggedAsInfo(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	persona := &fixedChecker{decision: Allow("persona allow")}
	multica := &fixedChecker{decision: Allow("multica allow")}
	c := NewCutoverPolicyChecker(persona, multica, PermissionEngineParallel, 100, log)

	_ = c.Check(context.Background(), sampleReq())

	if !strings.Contains(buf.String(), "permission engine comparison matched") {
		t.Fatalf("agreeing decisions must log a match at info, got: %s", buf.String())
	}
}

func TestCutover_SamplingIsDeterministicAndPartial(t *testing.T) {
	persona := PolicyCheckerFunc(func(_ context.Context, _ PolicyRequest) PolicyDecision { return Allow("p") })
	multica := PolicyCheckerFunc(func(_ context.Context, _ PolicyRequest) PolicyDecision { return Allow("m") })
	c := NewCutoverPolicyChecker(persona, multica, PermissionEngineParallel, 50, slog.Default())

	// shouldCompare must be deterministic for the same request key.
	req := sampleReq()
	first := c.shouldCompare(req)
	for i := 0; i < 100; i++ {
		if c.shouldCompare(req) != first {
			t.Fatal("shouldCompare must be deterministic for a stable request key")
		}
	}

	// Across many distinct keys, a 50% sample must select some but not all.
	selected := 0
	total := 500
	for i := 0; i < total; i++ {
		r := sampleReq()
		r.ActorID = mustUUID(uuidFromInt(i))
		if c.shouldCompare(r) {
			selected++
		}
	}
	if selected == 0 || selected == total {
		t.Fatalf("50%% sample should select a partial subset, selected %d/%d", selected, total)
	}
}

// uuidFromInt builds a deterministic UUID string from an int for sampling spread tests.
func uuidFromInt(i int) string {
	hex := "0123456789abcdef"
	b := make([]byte, 12)
	for j := 0; j < 12; j++ {
		b[j] = hex[(i>>(uint(j)*2))&0xf]
	}
	return "00000000-0000-4000-8000-0000" + string(b[:8])
}
