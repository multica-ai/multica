package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestPublishedDescriptionAnchorRoundTrip(t *testing.T) {
	text := "Heading\n中文 🐟 quiet release\nNext paragraph\n"
	issueID := dbfx.Issue(t, "Published anchor roundtrip", testutil.Cols{"description": "# Heading\n\n中文 🐟 **quiet release**\n\nNext paragraph"})
	anchor := DescriptionAnchor{Version: 1, ID: "selection-one", IssueID: issueID, Quote: "quiet release", Start: len(utf16.Encode([]rune("Heading\n中文 🐟 "))), Prefix: "中文 🐟 ", Suffix: "\nNext paragraph", SourceText: text}
	second := anchor
	second.ID = "selection-two"
	create := func(anchors []DescriptionAnchor) CommentResponse {
		req := withURLParam(newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{"content": "> quiet release\n\nKeep safe", "description_anchors": anchors}), "id", issueID)
		w := testutil.Call(t, testHandler.CreateComment, req).Want(http.StatusCreated)
		var c CommentResponse
		if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil {
			t.Fatal(err)
		}
		return c
	}
	created := create([]DescriptionAnchor{anchor, second})
	if len(created.DescriptionAnchors) != 2 || created.DescriptionAnchors[0] != anchor {
		t.Fatalf("create lost anchors: %#v", created)
	}
	var raw []byte
	dbfx.QueryRow(t, "SELECT description_anchors FROM comment WHERE id=$1", created.ID).Scan(&raw)
	var stored []DescriptionAnchor
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	if len(stored) != 2 || stored[1] != second {
		t.Fatalf("DB mismatch: %s", raw)
	}
	// Normal list, roots-only, recent threads, exact thread and timeline all use
	// their real productionSQLprojection rather than a custom test HTTP oracle.
	for _, suffix := range []string{"/comments", "/comments?roots_only=true", "/comments?thread=" + created.ID + "&tail=2", "/comments?recent=2", "/timeline"} {
		t.Run(suffix, func(t *testing.T) {
			req := withURLParam(newRequest("GET", "/api/issues/"+issueID+suffix, nil), "id", issueID)
			h := testHandler.ListComments
			if suffix == "/timeline" {
				h = testHandler.ListTimeline
			}
			w := testutil.Call(t, h, req).Want(http.StatusOK)
			var list []map[string]json.RawMessage
			if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, item := range list {
				var id string
				json.Unmarshal(item["id"], &id)
				if id == created.ID {
					var a []DescriptionAnchor
					if err := json.Unmarshal(item["description_anchors"], &a); err != nil {
						t.Fatal(err)
					}
					if len(a) != 2 || a[0] != anchor {
						t.Fatalf("read lost anchors: %s", w.Body.String())
					}
					found = true
				}
			}
			if !found {
				t.Fatalf("missing comment: %s", w.Body.String())
			}
		})
	}
	// Shared aggregate revision changes never alter the persisted source snapshot.
	dbfx.Exec(t, "UPDATE issue SET revision=revision+1,status='in_progress',description=description || E'\\n\\nUnrelated paragraph' WHERE id=$1", issueID)
	c, err := testHandler.Queries.GetComment(context.Background(), parseUUID(created.ID))
	if err != nil {
		t.Fatal(err)
	}
	if len(descriptionAnchorsFromJSON(c.DescriptionAnchors, false)) != 2 {
		t.Fatal("status update lost anchor")
	}
	// Edits preserve anchors, and tombstone responses expose no deleted quote.
	req := withURLParam(newRequest("PUT", "/api/comments/"+created.ID, map[string]any{"content": "> quiet release\n\nUpdated note"}), "commentId", created.ID)
	w := testutil.Call(t, testHandler.UpdateComment, req).Want(http.StatusOK)
	var edited CommentResponse
	json.Unmarshal(w.Body.Bytes(), &edited)
	if len(edited.DescriptionAnchors) != 2 {
		t.Fatalf("edit lost anchor: %s", w.Body.String())
	}
	dbfx.Comment(t, issueID, "Keep parent as tombstone", testutil.Cols{"parent_id": created.ID})
	deleteRequest := withURLParam(newRequest("DELETE", "/api/comments/"+created.ID, nil), "commentId", created.ID)
	testutil.Call(t, testHandler.DeleteComment, deleteRequest).Want(http.StatusNoContent)
	var deletedRaw []byte
	dbfx.QueryRow(t, "SELECT description_anchors FROM comment WHERE id=$1", created.ID).Scan(&deletedRaw)
	if string(deletedRaw) != "[]" {
		t.Fatalf("deleted comment retained source snapshot: %s", deletedRaw)
	}

	legacy := create(nil)
	if len(legacy.DescriptionAnchors) != 0 {
		t.Fatal("legacy comment gained anchor")
	}
}

func TestPublishedDescriptionAnchorValidation(t *testing.T) {
	issueID := dbfx.Issue(t, "Anchor validation")
	base := DescriptionAnchor{Version: 1, ID: "selection", IssueID: issueID, Quote: "🐟", Start: 3, SourceText: "中文 🐟 end", Prefix: "中文 ", Suffix: " end"}
	if _, err := validateDescriptionAnchors([]DescriptionAnchor{base}, issueID, "comment"); err != nil {
		t.Fatal(err)
	}
	cases := map[string]DescriptionAnchor{}
	a := base
	a.IssueID = "other"
	cases["cross-issue"] = a
	a = base
	a.Start = 4
	cases["surrogate-middle"] = a
	a = base
	a.Start = -1
	cases["negative"] = a
	a = base
	a.Prefix = "wrong"
	cases["context"] = a
	a = base
	a.Quote = strings.Repeat("x", 4001)
	cases["quote-limit"] = a
	a = base
	a.SourceText = "\x00" + a.SourceText
	cases["nul"] = a
	a = base
	a.Version = 2
	cases["future-version"] = a
	for name, a := range cases {
		t.Run(name, func(t *testing.T) {
			req := withURLParam(newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{"content": "No write", "description_anchors": []DescriptionAnchor{a}}), "id", issueID)
			testutil.Call(t, testHandler.CreateComment, req).Want(http.StatusBadRequest)
		})
	}
	if _, err := validateDescriptionAnchors([]DescriptionAnchor{base, base}, issueID, "comment"); err == nil {
		t.Fatal("duplicate id accepted")
	}
	many := make([]DescriptionAnchor, 21)
	for i := range many {
		many[i] = base
	}
	if _, err := validateDescriptionAnchors(many, issueID, "comment"); err == nil {
		t.Fatal("21 accepted")
	}
	if _, err := validateDescriptionAnchors([]DescriptionAnchor{base}, issueID, "progress_update"); err == nil {
		t.Fatal("nonplain accepted")
	}
	var count int
	dbfx.QueryRow(t, "SELECT count(*) FROM comment WHERE issue_id=$1", issueID).Scan(&count)
	if count != 0 {
		t.Fatal("rejected anchors wrote comments")
	}
}
