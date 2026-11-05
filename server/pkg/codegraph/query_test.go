package codegraph

import "testing"

func TestQueryValidation(t *testing.T) {
	if err := (&QueryInput{Limit: 51}).Normalize(); err == nil {
		t.Fatal("oversized query limit accepted")
	}
	for _, q := range []QueryInput{{Operation: "execute"}, {Operation: "search"}, {Operation: "callers"}, {Operation: "document_refs"}, {Operation: "read", Path: ".env"}, {Operation: "read", Path: "README.md", StartLine: -1}, {Operation: "read", Path: "README.md", StartLine: 1, EndLine: 201}} {
		if err := q.Normalize(); err == nil {
			t.Fatalf("invalid query accepted: %+v", q)
		}
	}
	for _, q := range []QueryInput{{}, {Operation: "documents"}, {Operation: "search", Query: "main"}, {Operation: "document_refs", SymbolID: "doc:known"}, {Operation: "read", Path: ".agents/agent.yaml"}} {
		if err := q.Normalize(); err != nil {
			t.Fatalf("valid query rejected: %+v %v", q, err)
		}
		if q.Operation == "read" && (q.StartLine != 1 || q.EndLine != 100) {
			t.Fatal("wrong source defaults")
		}
	}
}
