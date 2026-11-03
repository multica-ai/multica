package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestSupersedingPublishedDocumentHidesTheOldOne(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	ctx := context.Background()

	old := e.mustCreate(memberActor(e), KnowledgeDocumentInput{
		KB: "company", Title: "Pricing", Kind: "reference", Publish: true,
		Content: "# Pricing\n\n## Summary\n\nthe plan costs ten dollars a seat.\n",
	})
	if hits := e.search("dollars").Hits; len(hits) != 1 || hits[0].DocumentID != old.DocumentID {
		t.Fatalf("the old document should be searchable before it is replaced: %+v", hits)
	}

	// A draft that supersedes does not hide the old document until it is published.
	successor := e.mustCreate(agentActor(e), KnowledgeDocumentInput{
		KB: "company", Title: "Pricing v2", Kind: "reference",
		Content:          "# Pricing v2\n\n## Summary\n\nthe plan costs twelve dollars a seat.\n",
		SupersedesLinkID: mustUUID(t, old.ID),
	})
	if len(e.search("dollars").Hits) != 1 {
		t.Fatal("a draft hid the document it supersedes")
	}

	if _, err := e.svc.PublishDocument(ctx, e.ws, mustUUID(t, successor.ID), memberActor(e)); err != nil {
		t.Fatal(err)
	}
	hits := e.search("dollars").Hits
	if len(hits) != 1 || hits[0].DocumentID == old.DocumentID {
		t.Fatalf("after publishing the successor, search returned %+v", hits)
	}
	list, err := e.svc.ListDocuments(ctx, e.ws, "company", "")
	if err != nil {
		t.Fatal(err)
	}
	var sawOld bool
	for _, d := range list.Documents {
		if d.ID == old.ID {
			sawOld = true
			if !d.Superseded {
				t.Fatal("the listing does not mark the replaced document as superseded")
			}
		}
	}
	if !sawOld {
		t.Fatal("the replaced document disappeared from the listing")
	}
}

func TestSupersedesMustNameADocumentOfTheWorkspace(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	_, _, err := e.create(memberActor(e), KnowledgeDocumentInput{
		KB: "company", Title: "x", Content: "y", SupersedesLinkID: mustUUID(t, uuid.NewString()),
	})
	var invalid *KnowledgeInvalidError
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want invalid", err)
	}
}

func TestSearchFiltersByKind(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	ctx := context.Background()
	adr := e.mustCreate(memberActor(e), KnowledgeDocumentInput{Kind: "adr", Title: "Storage", Content: adrBody, Publish: true})
	ref := e.mustCreate(memberActor(e), KnowledgeDocumentInput{
		KB: "company", Title: "Glossary", Kind: "reference", Publish: true,
		Content: "# Glossary\n\n## Summary\n\npartitioned tables are explained here.\n",
	})
	untyped := e.mustCreate(memberActor(e), KnowledgeDocumentInput{KB: "company", Title: "Notes", Content: "partitioned tables again", Publish: true})

	all, err := e.svc.SearchKind(ctx, e.ws, "partitioned", "", "", 10)
	if err != nil || len(all.Hits) != 3 {
		t.Fatalf("unfiltered: %d hits, %v", len(all.Hits), err)
	}
	adrOnly, err := e.svc.SearchKind(ctx, e.ws, "partitioned", "", "adr", 10)
	if err != nil || len(adrOnly.Hits) != 1 || adrOnly.Hits[0].DocumentID != adr.DocumentID || adrOnly.Hits[0].KnowledgeKind != "adr" {
		t.Fatalf("adr: %+v, %v", adrOnly.Hits, err)
	}
	refOnly, _ := e.svc.SearchKind(ctx, e.ws, "partitioned", "", "reference", 10)
	if len(refOnly.Hits) != 1 || refOnly.Hits[0].DocumentID != ref.DocumentID {
		t.Fatalf("reference: %+v", refOnly.Hits)
	}
	if untyped.DocumentID == "" {
		t.Fatal("setup: the untyped document has no WeKnora id")
	}
}

func TestTagsAreCreatedInWeKnoraWithTheDocument(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	initiative := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	v := e.mustCreate(memberActor(e), KnowledgeDocumentInput{
		Kind: "adr", Title: "Storage", Content: adrBody, Publish: true,
		Tags: []string{"Billing", " billing ", "storage"}, InitiativeID: initiative, InitiativeFolder: "INI-3-invoicing",
	})
	got := e.fake.DocumentTags(v.DocumentID)
	want := []string{"INI-3-invoicing", "adr", "billing", "storage"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("WeKnora tags = %v, want %v", got, want)
	}
	stored, err := e.q.GetKnowledgeLink(context.Background(), db.GetKnowledgeLinkParams{ID: mustUUID(t, v.ID), WorkspaceID: e.ws})
	if err != nil || len(stored.Tags) != 4 || stored.Kind != "adr" {
		t.Fatalf("link = %+v, %v", stored, err)
	}
}

func TestTagNormalizationCapsAndDeduplicates(t *testing.T) {
	tags, err := normalizeKnowledgeTags([]string{"A", "a", " B "})
	if err != nil || strings.Join(tags, ",") != "a,b" {
		t.Fatalf("tags = %v, %v", tags, err)
	}
	many := make([]string, 11)
	for i := range many {
		many[i] = strings.Repeat("x", i+1)
	}
	if _, err := normalizeKnowledgeTags(many); err == nil {
		t.Fatal("eleven tags were accepted")
	}
}
