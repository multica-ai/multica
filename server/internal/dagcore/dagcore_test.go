package dagcore

import "testing"

func TestValidateEventRequiresMatchingDVTAgent(t *testing.T) {
	event := Event{
		ID:        "evt-1",
		RecordIDs: []string{"rec-1"},
		AgentID:   "agent-a",
		DVT:       DVT{Dot: Dot{AgentID: "agent-b", Counter: 1}, Context: map[string]int64{"agent-b": 1}},
		Operation: OperationCreate,
	}
	if err := ValidateEvent(event); err == nil {
		t.Fatal("expected mismatched agent validation error")
	}
}

func TestDVTCompareDetectsBeforeAfterEqualAndConcurrent(t *testing.T) {
	a := DVT{Dot: Dot{AgentID: "a", Counter: 1}, Context: map[string]int64{"a": 1}}
	b := DVT{Dot: Dot{AgentID: "b", Counter: 1}, Context: map[string]int64{"a": 1, "b": 1}}
	c := DVT{Dot: Dot{AgentID: "c", Counter: 1}, Context: map[string]int64{"a": 1, "c": 1}}
	if got := Compare(a, a); got != DVTEqual {
		t.Fatalf("equal compare = %s", got)
	}
	if got := Compare(a, b); got != DVTBefore {
		t.Fatalf("before compare = %s", got)
	}
	if got := Compare(b, a); got != DVTAfter {
		t.Fatalf("after compare = %s", got)
	}
	if got := Compare(b, c); got != DVTConcurrent {
		t.Fatalf("concurrent compare = %s", got)
	}
}

func TestMissingInverseLinks(t *testing.T) {
	links := []Link{
		{FromID: "a", ToID: "b", Type: "blocks", EventID: "e1"},
		{FromID: "c", ToID: "d", Type: "blocks", EventID: "e2"},
		{FromID: "d", ToID: "c", Type: "blocked_by", EventID: "e2"},
	}
	missing := MissingInverseLinks(links, map[string]string{"blocks": "blocked_by", "blocked_by": "blocks"})
	if len(missing) != 1 {
		t.Fatalf("missing inverse count = %d", len(missing))
	}
	if missing[0].Link.FromID != "a" || missing[0].InverseType != "blocked_by" {
		t.Fatalf("unexpected missing inverse: %+v", missing[0])
	}
}

func TestValidateAcyclicSchemasRejectsCyclesAndUnknownTypes(t *testing.T) {
	if err := ValidateAcyclicSchemas([]Schema{{Name: "task", DependsOn: []string{"agent"}}, {Name: "agent"}}); err != nil {
		t.Fatalf("valid schema graph rejected: %v", err)
	}
	if err := ValidateAcyclicSchemas([]Schema{{Name: "a", DependsOn: []string{"b"}}}); err == nil {
		t.Fatal("expected unknown dependency error")
	}
	if err := ValidateAcyclicSchemas([]Schema{{Name: "a", DependsOn: []string{"b"}}, {Name: "b", DependsOn: []string{"a"}}}); err == nil {
		t.Fatal("expected cycle error")
	}
}

func TestSortFactsIsDeterministic(t *testing.T) {
	facts := []Fact{
		{ID: "2", Predicate: "z", Args: []string{"b"}, EventID: "e2"},
		{ID: "1", Predicate: "a", Args: []string{"b"}, EventID: "e1"},
		{ID: "3", Predicate: "a", Args: []string{"a"}, EventID: "e1"},
	}
	sorted := SortFacts(facts)
	want := []string{"3", "1", "2"}
	for i := range want {
		if sorted[i].ID != want[i] {
			t.Fatalf("sorted[%d] = %s, want %s", i, sorted[i].ID, want[i])
		}
	}
}

func TestDetectContradictionsRequiresGrounding(t *testing.T) {
	facts := []Fact{
		{ID: "ungrounded", Predicate: "asserts", Args: []string{"issue-1", "ready", "true"}},
		{ID: "left", Predicate: "asserts", Args: []string{"issue-1", "ready", "true"}, GroundedBy: []string{"source-a"}},
		{ID: "right", Predicate: "asserts", Args: []string{"issue-1", "ready", "false"}, GroundedBy: []string{"source-b"}},
	}
	conflicts := DetectContradictions(facts)
	if len(conflicts) != 1 {
		t.Fatalf("conflict count = %d", len(conflicts))
	}
	if conflicts[0].Status != "open" || conflicts[0].Severity != "requires_review" {
		t.Fatalf("unexpected conflict state: %+v", conflicts[0])
	}
}

func TestValidateCitationChain(t *testing.T) {
	prob := 0.8
	if err := ValidateCitationChain(CitationChain{AssertionID: "assert-1", Citations: []Citation{{CitationID: "cite-1", SourceID: "source-1", Probability: &prob}}}); err != nil {
		t.Fatalf("valid citation chain rejected: %v", err)
	}
	badProb := 1.5
	if err := ValidateCitationChain(CitationChain{AssertionID: "assert-1", Citations: []Citation{{CitationID: "cite-1", SourceID: "source-1", Probability: &badProb}}}); err == nil {
		t.Fatal("expected invalid probability error")
	}
}

func TestConcurrentFieldConflicts(t *testing.T) {
	writes := []FieldWrite{
		{EventID: "e1", RecordID: "issue-1", Field: "status", Value: "todo", DVT: DVT{Dot: Dot{AgentID: "a", Counter: 1}, Context: map[string]int64{"a": 1}}},
		{EventID: "e2", RecordID: "issue-1", Field: "status", Value: "done", DVT: DVT{Dot: Dot{AgentID: "b", Counter: 1}, Context: map[string]int64{"b": 1}}},
		{EventID: "e3", RecordID: "issue-1", Field: "priority", Value: "high", DVT: DVT{Dot: Dot{AgentID: "c", Counter: 1}, Context: map[string]int64{"a": 1, "b": 1, "c": 1}}},
	}
	conflicts := ConcurrentFieldConflicts(writes)
	if len(conflicts) != 1 {
		t.Fatalf("conflict count = %d", len(conflicts))
	}
	if conflicts[0].Reason != "concurrent single-field write" || conflicts[0].Predicate != "status" {
		t.Fatalf("unexpected conflict: %+v", conflicts[0])
	}
}
