package knowledge

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// conformance is one behaviour script run against both the Fake (always) and a
// live WeKnora (opt-in, see integration_test.go), so the fake cannot drift from
// the engine on the behaviours other packages rely on.
type conformance struct {
	// TenantName must be unique per run.
	TenantName string
	Model      EmbeddingModelSpec
	// SearchWait is how long a published document may take to become searchable.
	SearchWait time.Duration
	// Token is a word that appears in no other document.
	Token string
}

func (c conformance) run(t *testing.T, k Knowledge) {
	t.Helper()
	ctx := context.Background()

	if err := k.Ping(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}

	tenant, err := k.EnsureTenant(ctx, c.TenantName, "conformance run")
	if err != nil {
		t.Fatalf("EnsureTenant: %v", err)
	}
	t.Cleanup(func() {
		if err := k.DeleteTenant(context.Background(), tenant.ID); err != nil {
			t.Errorf("DeleteTenant: %v", err)
		}
	})
	again, err := k.EnsureTenant(ctx, c.TenantName, "conformance run")
	if err != nil || again.ID != tenant.ID {
		t.Fatalf("EnsureTenant is not idempotent: %+v vs %+v (%v)", again, tenant, err)
	}

	key, err := k.CreateWorkspaceKey(ctx, tenant.ID, "conformance")
	if err != nil || key == "" {
		t.Fatalf("CreateWorkspaceKey: %q, %v", key, err)
	}
	ws := k.Workspace(key)

	if _, err := k.Workspace("sk-not-a-real-key").ListKnowledgeBases(ctx); !errors.Is(err, ErrAuth) {
		t.Fatalf("a wrong workspace key: err = %v, want ErrAuth", err)
	}

	model, err := ws.EnsureEmbeddingModel(ctx, c.Model)
	if err != nil {
		t.Fatalf("EnsureEmbeddingModel: %v", err)
	}
	if again, err := ws.EnsureEmbeddingModel(ctx, c.Model); err != nil || again.ID != model.ID {
		t.Fatalf("EnsureEmbeddingModel is not idempotent: %+v vs %+v (%v)", again, model, err)
	}

	kb, err := ws.CreateKnowledgeBase(ctx, KnowledgeBaseSpec{Name: "Conformance", Description: "d", EmbeddingModelID: model.ID})
	if err != nil {
		t.Fatalf("CreateKnowledgeBase: %v", err)
	}
	kbs, err := ws.ListKnowledgeBases(ctx)
	if err != nil || len(kbs) != 1 || kbs[0].ID != kb.ID || kbs[0].Name != "Conformance" {
		t.Fatalf("ListKnowledgeBases = %+v, %v", kbs, err)
	}

	// A draft is stored but never searchable.
	content := "# Decision record\n\nThe " + c.Token + " rollout is approved. Support answers within 4 hours."
	draft, err := ws.CreateManual(ctx, kb.ID, ManualDocument{Title: "Decision " + c.Token, Content: content})
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	if !draft.IsDraft() || draft.Type != "manual" {
		t.Fatalf("draft = %+v", draft)
	}
	hits, err := ws.Search(ctx, SearchRequest{Query: c.Token, KnowledgeBaseIDs: []string{kb.ID}})
	if err != nil {
		t.Fatalf("Search before publish: %v", err)
	}
	for _, h := range hits {
		if h.DocumentID == draft.ID {
			t.Fatalf("a draft was retrieved: %+v", h)
		}
	}

	// Publishing makes it the answer to its own distinctive query.
	if _, err := ws.Publish(ctx, draft.ID); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if again, err := ws.Publish(ctx, draft.ID); err != nil || again.IsDraft() {
		t.Fatalf("Publish is not idempotent: %+v, %v", again, err)
	}
	deadline := time.Now().Add(c.SearchWait)
	for {
		hits, err = ws.Search(ctx, SearchRequest{Query: c.Token, KnowledgeBaseIDs: []string{kb.ID}, Limit: 5})
		if err != nil {
			t.Fatalf("Search after publish: %v", err)
		}
		if len(hits) > 0 && hits[0].DocumentID == draft.ID {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("published document never became the top hit; hits = %+v", hits)
		}
		time.Sleep(time.Second)
	}

	read, err := ws.Read(ctx, draft.ID)
	if err != nil || strings.TrimSpace(read.Content) != strings.TrimSpace(content) {
		t.Fatalf("Read = %q, %v", read.Content, err)
	}

	// Files are indexed on upload; the same file twice is a duplicate.
	file := func() FileDocument {
		return FileDocument{Name: "runbook-" + c.Token + ".md", FolderPath: "ops", Content: strings.NewReader("# Runbook\n\nRestart the worker.\n")}
	}
	fileDoc, err := ws.CreateFile(ctx, kb.ID, file())
	if err != nil || fileDoc.Type != "file" {
		t.Fatalf("CreateFile = %+v, %v", fileDoc, err)
	}
	var dup *DuplicateError
	if _, err := ws.CreateFile(ctx, kb.ID, file()); !errors.Is(err, ErrDuplicate) || !errors.As(err, &dup) || dup.Existing.ID != fileDoc.ID {
		t.Fatalf("second upload err = %v, want a duplicate of %s", err, fileDoc.ID)
	}
	if _, err := ws.Publish(ctx, fileDoc.ID); !errors.Is(err, ErrNotManual) {
		t.Fatalf("Publish of a file err = %v, want ErrNotManual", err)
	}

	page, err := ws.ListDocuments(ctx, kb.ID, ListOptions{})
	if err != nil || page.Total != 2 || len(page.Items) != 2 {
		t.Fatalf("ListDocuments = %+v, %v", page, err)
	}

	// Read-only MCP endpoint; the token appears once.
	ep, err := ws.CreateMCPEndpoint(ctx, MCPEndpointSpec{Name: "company-read", KnowledgeBaseIDs: []string{kb.ID}, Tools: ReadOnlyMCPTools})
	if err != nil || ep.ID == "" || ep.Token == "" || ep.Path == "" {
		t.Fatalf("CreateMCPEndpoint = %+v, %v", ep, err)
	}
	eps, err := ws.ListMCPEndpoints(ctx)
	if err != nil || len(eps) != 1 || eps[0].ID != ep.ID || eps[0].Token != "" {
		t.Fatalf("ListMCPEndpoints = %+v, %v", eps, err)
	}
	if len(eps[0].Tools) != len(ReadOnlyMCPTools) {
		t.Fatalf("tools = %v", eps[0].Tools)
	}
	if rotated, err := ws.RotateMCPEndpointToken(ctx, ep.ID); err != nil || rotated.Token == "" {
		t.Fatalf("RotateMCPEndpointToken = %+v, %v", rotated, err)
	}
	if got, err := ws.SetMCPEndpointKnowledgeBases(ctx, ep.ID, []string{kb.ID}); err != nil || len(got.KnowledgeBaseIDs) != 1 {
		t.Fatalf("SetMCPEndpointKnowledgeBases = %+v, %v", got, err)
	}

	if err := ws.Delete(ctx, fileDoc.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := ws.DeleteKnowledgeBase(ctx, kb.ID); err != nil {
		t.Fatalf("DeleteKnowledgeBase: %v", err)
	}
}
