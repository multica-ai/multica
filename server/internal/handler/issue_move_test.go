package handler

import "testing"

func TestIssueMovePositionUsesRelativeAnchors(t *testing.T) {
	before := 10.0
	after := 20.0
	position, err := issueMovePosition(3, &before, &after)
	if err != nil {
		t.Fatalf("issueMovePosition: %v", err)
	}
	if position != 15 {
		t.Fatalf("position = %v, want 15", position)
	}
}

func TestIssueMovePositionRejectsStaleAnchors(t *testing.T) {
	before := 20.0
	after := 10.0
	if _, err := issueMovePosition(3, &before, &after); err == nil {
		t.Fatal("out-of-order anchors were accepted")
	}
}

func TestIssueMovePositionKeepsPositionWithoutAnchors(t *testing.T) {
	position, err := issueMovePosition(7, nil, nil)
	if err != nil {
		t.Fatalf("issueMovePosition: %v", err)
	}
	if position != 7 {
		t.Fatalf("position = %v, want 7", position)
	}
}
