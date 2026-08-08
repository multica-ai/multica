package handler

import (
	"testing"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestValidateCodeMRSnapshot(t *testing.T) {
	ready := true
	valid := protocol.CodeMRSnapshotResult{
		Title:                  "Deliver model catalog",
		State:                  "open",
		CreatedAt:              "2026-08-01T21:38:22+08:00",
		UpdatedAt:              "2026-08-02T12:49:01+08:00",
		ReadyToMerge:           &ready,
		CommentCount:           3,
		UnresolvedCommentCount: 2,
	}
	if err := validateCodeMRSnapshot(valid); err != nil {
		t.Fatalf("valid snapshot rejected: %v", err)
	}

	tests := []struct {
		name string
		edit func(*protocol.CodeMRSnapshotResult)
	}{
		{name: "empty title", edit: func(v *protocol.CodeMRSnapshotResult) { v.Title = "" }},
		{name: "unknown state", edit: func(v *protocol.CodeMRSnapshotResult) { v.State = "deleted" }},
		{name: "negative comments", edit: func(v *protocol.CodeMRSnapshotResult) { v.CommentCount = -1 }},
		{name: "unresolved exceeds total", edit: func(v *protocol.CodeMRSnapshotResult) { v.UnresolvedCommentCount = 4 }},
		{name: "invalid timestamp", edit: func(v *protocol.CodeMRSnapshotResult) { v.UpdatedAt = "yesterday" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := valid
			tc.edit(&got)
			if err := validateCodeMRSnapshot(got); err == nil {
				t.Fatal("expected validation failure")
			}
		})
	}
}
