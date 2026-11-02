package handler

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The knowledge section is not windowed by since, so the zero time (the default
// window) is used for every digest call here.
var digestAnySince = time.Time{}

// enableDigestKnowledge makes knowledge ready for one workspace, on the fake
// sidecar that useKnowledge installed, and removes its rows afterwards. The
// knowledge tables have no foreign keys, so the cleanup names them.
func enableDigestKnowledge(t *testing.T, ws string) {
	t.Helper()
	ctx := context.Background()
	if _, err := testHandler.Knowledge.Enable(ctx, parseUUID(ws), parseUUID(testUserID)); err != nil {
		t.Fatalf("enable knowledge: %v", err)
	}
	t.Cleanup(func() {
		for _, stmt := range []string{
			`DELETE FROM agent_mcp_server WHERE server_id IN (SELECT id FROM workspace_mcp_server WHERE workspace_id = $1 AND name = 'company-read')`,
			`DELETE FROM workspace_mcp_server WHERE workspace_id = $1 AND name = 'company-read'`,
			`DELETE FROM knowledge_link WHERE workspace_id = $1`,
			`DELETE FROM knowledge_base WHERE workspace_id = $1`,
			`DELETE FROM workspace_knowledge WHERE workspace_id = $1`,
		} {
			if _, err := testPool.Exec(ctx, stmt, ws); err != nil {
				t.Errorf("cleanup: %v", err)
			}
		}
	})
}

// digestDocument writes a document as a member. A published one has its
// publication moved back by ageDays, so the digest sees an old publication.
func digestDocument(t *testing.T, ws, kb, title string, publish bool, ageDays int) {
	t.Helper()
	ctx := context.Background()
	view, _, err := testHandler.Knowledge.CreateDocument(ctx, parseUUID(ws),
		service.KnowledgeActor{Type: service.KnowledgeActorMember, ID: parseUUID(testUserID)},
		service.KnowledgeDocumentInput{KB: kb, Title: title, Content: "Body of " + title, Publish: publish})
	if err != nil {
		t.Fatalf("create %q: %v", title, err)
	}
	if !publish {
		return
	}
	if view.State != service.KnowledgeLinkPublished {
		t.Fatalf("%q state = %s, want published", title, view.State)
	}
	if _, err := testPool.Exec(ctx,
		`UPDATE knowledge_link SET published_at = now() - make_interval(days => $2) WHERE id = $1`,
		view.ID, ageDays); err != nil {
		t.Fatalf("backdate %q: %v", title, err)
	}
}

// archiveDigestBase hides a knowledge base the way an archive does.
func archiveDigestBase(t *testing.T, ws, key string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(),
		`UPDATE knowledge_base SET archived_at = now() WHERE workspace_id = $1 AND key = $2`, ws, key); err != nil {
		t.Fatalf("archive %s: %v", key, err)
	}
}

func TestDigest_KnowledgeFreshnessAndPendingDrafts(t *testing.T) {
	useKnowledge(t)
	e := newDigestEnv(t)
	enableDigestKnowledge(t, e.WorkspaceID)

	// Twelve documents published well over 90 days ago, one just past the line,
	// and two recent ones that are not stale.
	for i := range 12 {
		digestDocument(t, e.WorkspaceID, "market", fmt.Sprintf("Old market %02d", i), true, 100+10*i)
	}
	digestDocument(t, e.WorkspaceID, "engineering", "Just past the line", true, 91)
	digestDocument(t, e.WorkspaceID, "engineering", "Just inside the line", true, 89)
	digestDocument(t, e.WorkspaceID, "market", "Recent", true, 30)

	// Drafts are pending, not published, so they are never stale.
	digestDocument(t, e.WorkspaceID, "market", "Draft one", false, 0)
	digestDocument(t, e.WorkspaceID, "market", "Draft two", false, 0)
	digestDocument(t, e.WorkspaceID, "engineering", "Draft three", false, 0)

	// A live base with a stale publication and a draft, archived afterwards: it
	// counts for neither.
	digestDocument(t, e.WorkspaceID, "product", "Archived product spec", true, 400)
	digestDocument(t, e.WorkspaceID, "product", "Archived draft", false, 0)
	archiveDigestBase(t, e.WorkspaceID, "product")

	d := e.digest(t, digestAnySince)
	if d.Knowledge.StalePublished.Count != 13 {
		t.Fatalf("stale count = %d, want 13 (12 market, the 91-day engineering one)", d.Knowledge.StalePublished.Count)
	}
	items := d.Knowledge.StalePublished.Items
	if len(items) != service.DigestKnowledgeListLimit {
		t.Fatalf("stale items = %d, want the cap of %d", len(items), service.DigestKnowledgeListLimit)
	}
	// Oldest first: the 210-day document leads.
	if items[0].Title != "Old market 11" || items[0].KnowledgeBase != "market" {
		t.Fatalf("first stale item = %+v, want the oldest market document", items[0])
	}
	if items[0].ID == "" || items[0].PublishedAt == "" {
		t.Fatalf("stale item lacks id or published_at: %+v", items[0])
	}
	if d.Knowledge.PendingDrafts != 3 {
		t.Fatalf("pending drafts = %d, want 3 (the archived base's draft is excluded)", d.Knowledge.PendingDrafts)
	}
}

func TestDigest_KnowledgeSectionEmptyWhenNotReady(t *testing.T) {
	useKnowledge(t)

	// Never enabled: the section is present, empty, and not an error.
	never := newDigestEnv(t)
	var out service.Digest
	req := newRequest("GET", "/api/office/digest", nil)
	req.Header.Set("X-Workspace-ID", never.WorkspaceID)
	testutil.Call(t, testHandler.GetOfficeDigest, req).Want(http.StatusOK).JSON(&out)
	if out.Knowledge.StalePublished.Count != 0 || len(out.Knowledge.StalePublished.Items) != 0 || out.Knowledge.PendingDrafts != 0 {
		t.Fatalf("knowledge section for a workspace without knowledge = %+v", out.Knowledge)
	}

	// Enabled, then failed: rows exist but the workspace is not ready, so the
	// same documents are not reported.
	failed := newDigestEnv(t)
	enableDigestKnowledge(t, failed.WorkspaceID)
	digestDocument(t, failed.WorkspaceID, "market", "Old but unreachable", true, 200)
	digestDocument(t, failed.WorkspaceID, "market", "Unreachable draft", false, 0)
	if _, err := testHandler.Queries.SetWorkspaceKnowledgeState(context.Background(), db.SetWorkspaceKnowledgeStateParams{
		WorkspaceID: parseUUID(failed.WorkspaceID), State: service.KnowledgeStateFailed,
	}); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	d := failed.digest(t, digestAnySince)
	if d.Knowledge.StalePublished.Count != 0 || d.Knowledge.PendingDrafts != 0 {
		t.Fatalf("failed workspace knowledge = %+v, want empty", d.Knowledge)
	}
}

func TestDigest_KnowledgeIsIsolatedByWorkspace(t *testing.T) {
	useKnowledge(t)
	a := newDigestEnv(t)
	b := newDigestEnv(t)
	enableDigestKnowledge(t, a.WorkspaceID)
	enableDigestKnowledge(t, b.WorkspaceID)

	digestDocument(t, a.WorkspaceID, "market", "A stale profile", true, 120)
	digestDocument(t, a.WorkspaceID, "market", "A draft", false, 0)

	inA := a.digest(t, digestAnySince).Knowledge
	if inA.StalePublished.Count != 1 || inA.PendingDrafts != 1 {
		t.Fatalf("workspace A knowledge = %+v", inA)
	}
	inB := b.digest(t, digestAnySince).Knowledge
	if inB.StalePublished.Count != 0 || len(inB.StalePublished.Items) != 0 || inB.PendingDrafts != 0 {
		t.Fatalf("workspace B sees A's knowledge: %+v", inB)
	}
}
