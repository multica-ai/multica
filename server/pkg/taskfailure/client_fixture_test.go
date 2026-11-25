package taskfailure

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

// The web and mobile clients each keep their own label table for
// failure_reason, so a new reason here can ship with no label and show up as
// the raw code (web) or a bare "Failed" (mobile). The clients' tests read
// this fixture and fail when a reason in it has no label, so it must keep
// matching AllReasons. Regenerate it with
// UPDATE_FAILURE_REASON_FIXTURE=1 go test ./pkg/taskfailure/ -run TestClientFailureReasonFixture
func TestClientFailureReasonFixture(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(here), "..", "..", "..", "packages", "views", "agents", "components", "tabs", "test-fixtures", "server-failure-reasons.json")

	want := make([]string, 0, len(allReasons))
	for _, r := range AllReasons() {
		want = append(want, r.String())
	}

	if os.Getenv("UPDATE_FAILURE_REASON_FIXTURE") == "1" {
		b, err := json.MarshalIndent(want, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("fixture not readable: %v", err)
	}
	var got []string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("fixture not JSON: %v", err)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("fixture %v\nAllReasons %v\n(regenerate the fixture, then add web/mobile labels for any new reason)", got, want)
	}
}
