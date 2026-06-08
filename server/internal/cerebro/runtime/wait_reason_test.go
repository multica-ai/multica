package runtime

import (
	"testing"
	"time"
)

func TestFormatRuntimePauseWaitReason(t *testing.T) {
	until := time.Date(2026, 6, 1, 11, 55, 0, 0, time.UTC)
	got := FormatRuntimePauseWaitReason("rate_limit", until, false)
	want := "runtime_paused|rate_limit|2026-06-01T11:55:00Z"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	circuit := FormatRuntimePauseWaitReason("rate_limit", until, true)
	if circuit != "runtime_paused|rate_limit|" {
		t.Fatalf("circuit: got %q", circuit)
	}
	if !IsRuntimePauseWaitReason(got) {
		t.Fatal("expected prefix match")
	}
	if IsRuntimePauseWaitReason("local_directory:/foo") {
		t.Fatal("unrelated wait_reason must not match")
	}
}
