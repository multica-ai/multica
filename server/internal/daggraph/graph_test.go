package daggraph

import (
	"testing"
)

func TestGraphCycles(t *testing.T) {
	// Acyclic diamond
	g := NewGraph([]Edge{
		{From: "a", To: "b"},
		{From: "a", To: "c"},
		{From: "b", To: "d"},
		{From: "c", To: "d"},
	})
	cycles := g.Cycles()
	if len(cycles) != 0 {
		t.Fatalf("expected no cycles, got %d", len(cycles))
	}

	// Simple cycle
	g2 := NewGraph([]Edge{
		{From: "a", To: "b"},
		{From: "b", To: "c"},
		{From: "c", To: "a"},
	})
	cycles2 := g2.Cycles()
	if len(cycles2) != 1 {
		t.Fatalf("expected 1 cycle, got %d", len(cycles2))
	}
}

func TestGraphTopologicalSort(t *testing.T) {
	g := NewGraph([]Edge{
		{From: "a", To: "b"},
		{From: "a", To: "c"},
		{From: "b", To: "d"},
		{From: "c", To: "d"},
	})
	order, err := g.TopologicalSort()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(order) != 4 {
		t.Fatalf("expected 4 nodes, got %d", len(order))
	}
	// a must come before b, c, d
	idx := make(map[string]int)
	for i, id := range order {
		idx[id] = i
	}
	if idx["a"] >= idx["b"] || idx["a"] >= idx["c"] || idx["a"] >= idx["d"] {
		t.Fatal("a must come before its dependents")
	}
}

func TestGraphCriticalPath(t *testing.T) {
	g := NewGraph([]Edge{
		{From: "a", To: "b"},
		{From: "b", To: "c"},
		{From: "c", To: "d"},
	})
	cp := g.CriticalPath()
	if cp == nil {
		t.Fatal("expected critical path map")
	}
	if cp["a"] != 3 {
		t.Fatalf("expected critical path from a = 3, got %d", cp["a"])
	}
	if cp["d"] != 0 {
		t.Fatalf("expected critical path from d = 0, got %d", cp["d"])
	}
}
