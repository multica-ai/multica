package ship

import (
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestMapDeploymentStatusState(t *testing.T) {
	cases := map[string]db.DeployStatus{
		"success":     db.DeployStatusSucceeded,
		"failure":     db.DeployStatusFailed,
		"error":       db.DeployStatusFailed,
		"in_progress": db.DeployStatusInProgress,
		"queued":      db.DeployStatusPending,
		"pending":     db.DeployStatusPending,
		"inactive":    db.DeployStatusRolledBack,
		// Unknown values must NOT crash — the GitHub enum could grow.
		"galactic": db.DeployStatusPending,
		"":         db.DeployStatusPending,
	}
	for input, want := range cases {
		if got := mapDeploymentStatusState(input); got != want {
			t.Errorf("mapDeploymentStatusState(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestMapStatusToConclusion(t *testing.T) {
	cases := map[string]string{
		"success": "success",
		"failure": "failure",
		"error":   "failure",
		"pending": "",
		// Unknown / unset → empty (treated as "not yet conclusive" by
		// the rollup so we don't lock in a wrong final status).
		"weirdo": "",
	}
	for input, want := range cases {
		if got := mapStatusToConclusion(input); got != want {
			t.Errorf("mapStatusToConclusion(%q) = %q, want %q", input, got, want)
		}
	}
}
