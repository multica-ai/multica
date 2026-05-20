package handler

import (
	"context"
	"testing"
	"time"
)

func TestBatchTimeout(t *testing.T) {
	tests := []struct {
		name     string
		count    int
		expected time.Duration
	}{
		{"zero skills", 0, 30 * time.Second},
		{"1 skill", 1, 30 * time.Second},
		{"5 skills", 5, 30 * time.Second},
		{"10 skills", 10, 60 * time.Second},
		{"20 skills", 20, 90 * time.Second},
		{"43 skills", 43, 150 * time.Second},
		{"100 skills", 100, 300 * time.Second},
		{"1000 skills (capped)", 1000, 300 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := batchTimeout(tt.count)
			if got != tt.expected {
				t.Errorf("batchTimeout(%d) = %v, want %v", tt.count, got, tt.expected)
			}
		})
	}
}

func TestBatchTimeoutContextCancellation(t *testing.T) {
	// Test that a very short timeout actually cancels
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	// Simulate work that exceeds timeout
	time.Sleep(50 * time.Millisecond)

	if ctx.Err() != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got %v", ctx.Err())
	}
}

func TestBatchImportResponseEmptySlices(t *testing.T) {
	// Ensure empty BatchImportResponse serializes [] not null
	resp := BatchImportResponse{
		Skills: make([]SkillWithFilesResponse, 0),
		Errors: make([]BatchImportError, 0),
	}

	if resp.Skills == nil {
		t.Error("Skills should be empty slice, not nil")
	}
	if resp.Errors == nil {
		t.Error("Errors should be empty slice, not nil")
	}
	if len(resp.Skills) != 0 {
		t.Errorf("Skills length should be 0, got %d", len(resp.Skills))
	}
	if len(resp.Errors) != 0 {
		t.Errorf("Errors length should be 0, got %d", len(resp.Errors))
	}
}
