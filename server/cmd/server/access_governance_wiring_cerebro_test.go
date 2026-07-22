package main

import (
	"os"
	"strings"
	"testing"
)

// The governance scanner is only real when the production server starts it.
// This contract keeps the pure scanner from silently becoming dead code again.
func TestMainStartsAccessGovernanceSweeper(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if !strings.Contains(string(source), "cerebroaccessgovernance.NewDatabaseSweeper(pool).Run") {
		t.Fatal("main server does not start the access-governance sweeper")
	}
}
