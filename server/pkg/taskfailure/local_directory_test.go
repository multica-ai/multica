package taskfailure

import "testing"

// TestNormalizeDaemonReasonUpgradesLocalDirectoryMissing covers MUL-9665's
// mixed-version window. A daemon predating ReasonLocalDirectoryMissing reports
// a deleted project folder as the generic local_directory_error; the error
// text it has always written identifies the case, so the label is upgraded
// the moment the server deploys.
func TestNormalizeDaemonReasonUpgradesLocalDirectoryMissing(t *testing.T) {
	t.Parallel()

	missing := `local_directory: path does not exist: "/Users/u/code/game"`
	if got := NormalizeDaemonReason("local_directory_error", missing); got != ReasonLocalDirectoryMissing {
		t.Errorf("NormalizeDaemonReason(local_directory_error, missing path) = %q, want %q", got, ReasonLocalDirectoryMissing)
	}
	if got := NormalizeDaemonReason(ReasonLocalDirectoryMissing.String(), missing); got != ReasonLocalDirectoryMissing {
		t.Errorf("NormalizeDaemonReason(local_directory_missing) = %q, want it preserved", got)
	}

	// Every other local_directory failure has a different fix and keeps the
	// generic reason.
	for _, rawError := range []string{
		`local_directory: path is not a directory: "/Users/u/code/game"`,
		`local_directory: stat "/Users/u/code/game": permission denied`,
		`local_directory: path is the user's home directory ("/Users/u")`,
		"local_directory wait cancelled: context canceled",
	} {
		if got := NormalizeDaemonReason("local_directory_error", rawError); got != "local_directory_error" {
			t.Errorf("NormalizeDaemonReason(local_directory_error, %q) = %q, want it preserved", rawError, got)
		}
	}
}
