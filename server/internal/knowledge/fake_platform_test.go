package knowledge

import (
	"context"
	"errors"
	"testing"
)

func TestFakeEnsureUserKeepsFirstPassword(t *testing.T) {
	f := NewFake()
	ctx := context.Background()
	first, err := f.EnsureUser(ctx, UserSpec{Username: "mul-owner", Email: "owner@example.test", Password: "first-pass"})
	if err != nil {
		t.Fatal(err)
	}
	again, err := f.EnsureUser(ctx, UserSpec{Username: "mul-owner", Email: "owner@example.test", Password: "second-pass"})
	if err != nil || again.ID != first.ID {
		t.Fatalf("EnsureUser is not idempotent: %+v vs %+v (%v)", again, first, err)
	}
	if got := f.UserPassword("owner@example.test"); got != "first-pass" {
		t.Fatalf("password = %q; a repeated create must not reset it", got)
	}
}

func TestFakeAddTenantMemberNeedsTenantAndLogin(t *testing.T) {
	f := NewFake()
	ctx := context.Background()
	tenant, _ := f.EnsureTenant(ctx, "multica-ws", "")
	if err := f.AddTenantMember(ctx, tenant.ID, "nobody@example.test", "admin"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a missing login err = %v, want ErrNotFound", err)
	}
	_, _ = f.EnsureUser(ctx, UserSpec{Username: "mul-owner", Email: "owner@example.test", Password: "p"})
	if err := f.AddTenantMember(ctx, tenant.ID, "owner@example.test", "admin"); err != nil {
		t.Fatal(err)
	}
	if err := f.AddTenantMember(ctx, tenant.ID, "owner@example.test", "viewer"); err != nil {
		t.Fatal(err)
	}
	if got := f.Members(tenant.ID)["owner@example.test"]; got != "admin" {
		t.Fatalf("role = %q; a repeated add must not change the role", got)
	}
}

func TestFakeModelTestFailsOnlyTheMarkedModel(t *testing.T) {
	f := NewFake()
	ctx := context.Background()
	tenant, _ := f.EnsureTenant(ctx, "multica-ws", "")
	ws := f.Workspace(f.mustKey(t, tenant.ID))
	chat, err := ws.EnsureModel(ctx, ModelSpec{Name: "summary-chat", Type: ModelTypeKnowledgeQA, BaseURL: "https://llm.example.test/v1"})
	if err != nil {
		t.Fatal(err)
	}
	rerank, err := ws.EnsureModel(ctx, ModelSpec{Name: "rerank-model", Type: ModelTypeRerank, BaseURL: "https://llm.example.test/v1"})
	if err != nil {
		t.Fatal(err)
	}
	f.FailModelTest("rerank-model", errors.New("upstream returned 404"))
	if err := ws.TestModel(ctx, chat.ID, ModelProbe{Input: "OK"}); err != nil {
		t.Fatalf("the chat model failed: %v", err)
	}
	err = ws.TestModel(ctx, rerank.ID, ModelProbe{Input: "q", Documents: []string{"a"}})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Message != "upstream returned 404" {
		t.Fatalf("rerank test err = %v", err)
	}
}

func TestFakeDataSourcesAreListedUnderTheirBaseOnly(t *testing.T) {
	f := NewFake()
	ctx := context.Background()
	tenant, _ := f.EnsureTenant(ctx, "multica-ws", "")
	ws := f.Workspace(f.mustKey(t, tenant.ID))
	kb, err := ws.CreateKnowledgeBase(ctx, KnowledgeBaseSpec{Name: "Market", EmbeddingModelID: f.mustEmbedding(t, ws)})
	if err != nil {
		t.Fatal(err)
	}
	other, err := ws.CreateKnowledgeBase(ctx, KnowledgeBaseSpec{Name: "Product", EmbeddingModelID: f.mustEmbedding(t, ws)})
	if err != nil {
		t.Fatal(err)
	}
	f.AddDataSource(tenant.ID, kb.ID, DataSource{Name: "Competitor feed", Type: "rss", Status: "active"})
	list, err := ws.ListDataSources(ctx, kb.ID)
	if err != nil || len(list) != 1 || list[0].Name != "Competitor feed" {
		t.Fatalf("ListDataSources = %+v, %v", list, err)
	}
	empty, err := ws.ListDataSources(ctx, other.ID)
	if err != nil || len(empty) != 0 {
		t.Fatalf("another base lists %+v, %v", empty, err)
	}
}

func TestFakeUpdateKnowledgeBaseSetsWikiAndSummaries(t *testing.T) {
	f := NewFake()
	ctx := context.Background()
	tenant, _ := f.EnsureTenant(ctx, "multica-ws", "")
	ws := f.Workspace(f.mustKey(t, tenant.ID))
	kb, _ := ws.CreateKnowledgeBase(ctx, KnowledgeBaseSpec{Name: "Company", EmbeddingModelID: f.mustEmbedding(t, ws)})
	chat, _ := ws.EnsureModel(ctx, ModelSpec{Name: "summary-chat", Type: ModelTypeKnowledgeQA, BaseURL: "https://llm.example.test/v1"})
	on := true
	got, err := ws.UpdateKnowledgeBase(ctx, kb.ID, KnowledgeBasePatch{SummaryModelID: &chat.ID, WikiEnabled: &on})
	if err != nil || !got.WikiEnabled || got.SummaryModelID != chat.ID {
		t.Fatalf("UpdateKnowledgeBase = %+v, %v", got, err)
	}
	off := false
	got, err = ws.UpdateKnowledgeBase(ctx, kb.ID, KnowledgeBasePatch{WikiEnabled: &off})
	if err != nil || got.WikiEnabled || got.SummaryModelID != chat.ID {
		t.Fatalf("a nil field must keep its value: %+v, %v", got, err)
	}
}

// mustKey mints a workspace key for a tenant in the fake.
func (f *Fake) mustKey(t *testing.T, tenantID string) string {
	t.Helper()
	key, err := f.CreateWorkspaceKey(context.Background(), tenantID, "test")
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// mustEmbedding registers an embedding model in the workspace.
func (f *Fake) mustEmbedding(t *testing.T, ws Workspace) string {
	t.Helper()
	m, err := ws.EnsureEmbeddingModel(context.Background(), EmbeddingModelSpec{Name: "bge-m3", Dimension: 1024})
	if err != nil {
		t.Fatal(err)
	}
	return m.ID
}
