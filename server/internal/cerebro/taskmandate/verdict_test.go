package taskmandate

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestVerdictForErrorUsesOneStableDenialContract(t *testing.T) {
	t.Parallel()
	tests := []struct {
		err      error
		code     VerdictCode
		recovery RecoveryAction
	}{
		{ErrMissing, VerdictMandateMissing, RecoveryRetryClaim},
		{ErrExpired, VerdictMandateExpired, RecoveryStartNewTask},
		{ErrIdentityMismatch, VerdictIdentityMismatch, RecoveryRefreshTaskContext},
		{ErrStaleClaimGeneration, VerdictStaleGeneration, RecoveryRetryClaim},
		{ErrToolDeny, VerdictToolNotAuthorized, RecoveryStartNewTask},
		{ErrFinalizedGrantsChanged, VerdictFinalizationConflict, RecoveryFixInventory},
	}
	for _, tt := range tests {
		verdict := VerdictForError(tt.err)
		if verdict.Allowed || verdict.Code != tt.code || verdict.RecoveryAction != tt.recovery {
			t.Errorf("VerdictForError(%v) = %+v, want %s/%s", tt.err, verdict, tt.code, tt.recovery)
		}
		if verdict.Message == "" {
			t.Errorf("VerdictForError(%v) has no stable message", tt.err)
		}
	}
	unknown := VerdictForError(errors.New("database unavailable"))
	if unknown.Code != VerdictInternalError || unknown.RecoveryAction != RecoveryRetry {
		t.Fatalf("unknown verdict = %+v, want internal error/retry", unknown)
	}
}

func TestVerdictJSONCarriesStableContractAndDiagnostic(t *testing.T) {
	var payload struct {
		Verdict Verdict `json:"verdict"`
		Detail  string  `json:"detail"`
	}
	if err := json.Unmarshal([]byte(VerdictJSON(ErrExpired)), &payload); err != nil {
		t.Fatalf("VerdictJSON returned invalid JSON: %v", err)
	}
	if payload.Verdict.Code != VerdictMandateExpired || payload.Verdict.RecoveryAction != RecoveryStartNewTask {
		t.Fatalf("verdict = %+v, want expired/start_new_task", payload.Verdict)
	}
	if payload.Detail != ErrExpired.Error() {
		t.Fatalf("detail = %q, want %q", payload.Detail, ErrExpired)
	}
}
