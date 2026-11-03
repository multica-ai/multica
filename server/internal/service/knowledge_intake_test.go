package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/knowledge"
)

// marketSizingBody carries every heading of market_report except Sources.
const marketSizingBody = "# Sizing\n\n## Summary\n\nx\n\n## Market size\n\nx\n\n## Trends\n\nx\n"

// K3 and K4: an uploaded text file must carry its kind's headings, and the upload
// is stored with the kind as its WeKnora tag.
func TestUploadChecksHeadingsAndTagsTheKind(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	ctx := context.Background()
	upload := func(name, body, kind string) (KnowledgeDocumentView, bool, error) {
		return e.svc.UploadDocument(ctx, e.ws, memberActor(e), KnowledgeUploadInput{
			KB: "market", Kind: kind, Name: name, Size: int64(len(body)), Content: strings.NewReader(body),
		})
	}

	_, _, err := upload("sizing.md", marketSizingBody, "market_report")
	var missing *KnowledgeMissingSectionsError
	if !errors.As(err, &missing) || strings.Join(missing.Missing, ",") != "Sources" {
		t.Fatalf("upload without Sources: err = %v, want the missing list [Sources]", err)
	}
	if e.fake.Calls(knowledge.OpCreateFile) != 0 || e.linkCount() != 0 {
		t.Fatal("a refused upload reached WeKnora or the registry")
	}

	view, created, err := upload("sizing.md", marketSizingBody+"\n## Sources\n\nx\n", "market_report")
	if err != nil || !created {
		t.Fatalf("complete upload: created %v, err %v", created, err)
	}
	if tags := e.fake.DocumentTags(view.DocumentID); len(tags) != 1 || tags[0] != "market_report" {
		t.Fatalf("WeKnora tags of the upload = %v, want [market_report]", tags)
	}
	if len(view.Tags) != 1 || view.Tags[0] != "market_report" {
		t.Fatalf("registry tags = %v", view.Tags)
	}

	// A PDF's headings are read before storage too (TestUploadChecksWordAndPDFHeadingsBeforeStorage),
	// so bytes that are not a readable PDF are refused rather than stored unchecked.
	if _, _, err := upload("sizing.pdf", "%PDF-1.4 binary", "market_report"); err == nil || !strings.Contains(err.Error(), "cannot be read") {
		t.Fatalf("an unreadable PDF: err = %v, want a refusal before storage", err)
	}

	_, _, err = upload("sizing.md", marketSizingBody, "no_such_kind")
	var invalid *KnowledgeInvalidError
	if !errors.As(err, &invalid) || !strings.Contains(invalid.Message, "unknown kind") {
		t.Fatalf("unknown kind: err = %v, want a 400 invalid error", err)
	}
}

// K3: a page imported by URL must carry its kind's headings, and a page without
// them is removed from WeKnora again.
func TestURLImportRefusesAPageMissingItsKindsHeadings(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	ctx := context.Background()
	const short = "https://example.test/short"
	e.fake.SetURLContent(short, "# Sizing\n\n## Summary\n\nx\n")

	_, _, err := e.svc.CreateDocument(ctx, e.ws, memberActor(e), KnowledgeDocumentInput{
		KB: "market", Title: "Short page", URL: short, Kind: "market_report", Publish: true,
	})
	var missing *KnowledgeMissingSectionsError
	if !errors.As(err, &missing) || strings.Join(missing.Missing, ",") != "Market size,Trends,Sources" {
		t.Fatalf("short page: err = %v, want the three missing headings", err)
	}
	if e.linkCount() != 0 || e.fake.Calls(knowledge.OpDelete) != 1 {
		t.Fatalf("a refused page stayed: %d links, %d deletes", e.linkCount(), e.fake.Calls(knowledge.OpDelete))
	}

	const full = "https://example.test/full"
	e.fake.SetURLContent(full, marketSizingBody+"\n## Sources\n\nx\n")
	view, created, err := e.svc.CreateDocument(ctx, e.ws, memberActor(e), KnowledgeDocumentInput{
		KB: "market", Title: "Full page", URL: full, Kind: "market_report", Publish: true,
	})
	if err != nil || !created || view.State != "published" {
		t.Fatalf("complete page: view %+v, created %v, err %v", view, created, err)
	}
}

// K3: a URL held as a draft is checked when it is published, because WeKnora
// only fetches the page then.
func TestURLDraftIsCheckedWhenItIsPublished(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	ctx := context.Background()
	const page = "https://example.test/draft"
	e.fake.SetURLContent(page, "# Sizing\n\n## Summary\n\nx\n")
	draft := e.mustCreate(agentActor(e), KnowledgeDocumentInput{KB: "market", Title: "Draft page", URL: page, Kind: "market_report"})

	_, err := e.svc.PublishDocument(ctx, e.ws, mustUUID(t, draft.ID), memberActor(e))
	var missing *KnowledgeMissingSectionsError
	if !errors.As(err, &missing) {
		t.Fatalf("publish of a short page: err = %v, want the missing list", err)
	}
	if e.fake.Calls(knowledge.OpCreateURL) != 1 || e.fake.Calls(knowledge.OpDelete) != 1 {
		t.Fatalf("publish did not import and remove the page once: %d imports, %d deletes",
			e.fake.Calls(knowledge.OpCreateURL), e.fake.Calls(knowledge.OpDelete))
	}
}

// K5: the kind's default base is edited only to a base of the workspace.
func TestKindPatchSetsTheDefaultBaseOnlyToAKnownBase(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	ctx := context.Background()
	product := "product"
	view, err := e.svc.UpdateKnowledgeKind(ctx, e.ws, "analysis", KnowledgeKindPatch{DefaultBaseKey: &product})
	if err != nil || view.DefaultBaseKey != "product" {
		t.Fatalf("set default base: %+v, %v", view, err)
	}

	missingBase := "nowhere"
	_, err = e.svc.UpdateKnowledgeKind(ctx, e.ws, "analysis", KnowledgeKindPatch{DefaultBaseKey: &missingBase})
	var invalid *KnowledgeInvalidError
	if !errors.As(err, &invalid) {
		t.Fatalf("unknown base: err = %v, want an invalid error", err)
	}
	if kinds, _ := e.svc.ListKnowledgeKinds(ctx, e.ws); kindByKey(kinds, "analysis").DefaultBaseKey != "product" {
		t.Fatal("a refused base changed the kind")
	}

	empty := ""
	view, err = e.svc.UpdateKnowledgeKind(ctx, e.ws, "analysis", KnowledgeKindPatch{DefaultBaseKey: &empty})
	if err != nil || view.DefaultBaseKey != "" {
		t.Fatalf("clear default base: %+v, %v", view, err)
	}
}

func kindByKey(kinds []KnowledgeKindView, key string) KnowledgeKindView {
	for _, k := range kinds {
		if k.Key == key {
			return k
		}
	}
	return KnowledgeKindView{}
}

// K6: a document that names its initiative gets the initiative's folder, derived
// from the initiative's sequence number and title.
func TestInitiativeFolderIsDerivedFromTheInitiative(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	ctx := context.Background()
	var iniID string
	if err := e.pool.QueryRow(ctx, `INSERT INTO initiative (workspace_id, pipeline_id, seq, title, stage_key, issue_id)
		VALUES ($1, gen_random_uuid(), 3, 'Invoice automation', 'validate', gen_random_uuid()) RETURNING id::text`, e.wsID).Scan(&iniID); err != nil {
		t.Fatal(err)
	}
	view := e.mustCreate(memberActor(e), KnowledgeDocumentInput{KB: "ventures", Title: "Invoice market", Content: "# Invoice market\n\nx\n",
		InitiativeID: mustUUID(t, iniID)})
	if len(view.Tags) != 1 || view.Tags[0] != "INI-3-invoice-automation" {
		t.Fatalf("tags = %v, want the derived folder", view.Tags)
	}

	_, _, err := e.svc.CreateDocument(ctx, e.ws, memberActor(e), KnowledgeDocumentInput{KB: "ventures", Title: "Stray", Content: "x",
		InitiativeID: mustUUID(t, "00000000-0000-4000-8000-000000000001")})
	var invalid *KnowledgeInvalidError
	if !errors.As(err, &invalid) {
		t.Fatalf("an initiative of another workspace: err = %v, want an invalid error", err)
	}
}
