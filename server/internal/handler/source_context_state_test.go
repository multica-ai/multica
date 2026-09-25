package handler

import (
	"reflect"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
)

func sourceContextStateFixture() service.SourceContextSnapshot {
	description := "Captured description"
	return service.SourceContextSnapshot{
		SourceIssue: service.SourceContextIssueSnapshot{
			ID: "issue-1", Identifier: "MUL-1", Number: 1, Title: "Captured title", Description: &description,
			Attachments: []service.SourceContextAttachment{{
				ID: "clone-1", SourceAttachmentID: "attachment-1", Filename: "issue.txt", ContentType: "text/plain", SizeBytes: 12,
			}},
		},
		CommentThread: []service.SourceContextCommentSnapshot{{
			ID: "comment-1", Type: "comment", Content: "Captured comment",
			Author: service.SourceContextAuthor{Type: "member", ID: "member-1", Name: "Alice"},
			Attachments: []service.SourceContextAttachment{{
				ID: "clone-2", SourceAttachmentID: "attachment-2", Filename: "comment.txt", ContentType: "text/plain", SizeBytes: 13,
			}},
		}},
		AnchorCommentID: "comment-1",
	}
}

func TestSourceContextChangeReasonsSeparateContentFromLiveIdentityMetadata(t *testing.T) {
	captured := sourceContextStateFixture()
	current := sourceContextStateFixture()
	current.SourceIssue.Attachments[0].ID = "attachment-1"
	current.SourceIssue.Attachments[0].SourceAttachmentID = ""
	current.CommentThread[0].Attachments[0].ID = "attachment-2"
	current.CommentThread[0].Attachments[0].SourceAttachmentID = ""
	current.CommentThread[0].Author.Name = "Alice Renamed"

	if got := sourceContextIssueChangeReasons(captured.SourceIssue, current.SourceIssue); len(got) != 0 {
		t.Fatalf("identity-only issue reasons = %v, want none", got)
	}
	if got := sourceContextThreadChangeReasons(captured, current); len(got) != 0 {
		t.Fatalf("author-rename path reasons = %v, want none", got)
	}

	tests := []struct {
		name   string
		mutate func(*service.SourceContextSnapshot)
		want   []string
	}{
		{"issue title", func(value *service.SourceContextSnapshot) { value.SourceIssue.Title = "Changed" }, []string{sourceContextChangeIssueTitle}},
		{"issue description", func(value *service.SourceContextSnapshot) {
			changed := "Changed"
			value.SourceIssue.Description = &changed
		}, []string{sourceContextChangeIssueDescription}},
		{"issue attachment inventory only", func(value *service.SourceContextSnapshot) { value.SourceIssue.Attachments = nil }, []string{}},
		{"comment structure", func(value *service.SourceContextSnapshot) {
			parent := "other"
			value.CommentThread[0].ParentID = &parent
		}, []string{sourceContextChangeCommentThread}},
		{"comment structure and content", func(value *service.SourceContextSnapshot) {
			parent := "other"
			value.CommentThread[0].ParentID = &parent
			value.CommentThread[0].Content = "Changed"
		}, []string{sourceContextChangeCommentThread}},
		{"comment content", func(value *service.SourceContextSnapshot) { value.CommentThread[0].Content = "Changed" }, []string{sourceContextChangeCommentThread}},
		{"comment attachments", func(value *service.SourceContextSnapshot) { value.CommentThread[0].Attachments = nil }, []string{sourceContextChangeCommentThread}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := sourceContextStateFixture()
			test.mutate(&value)
			got := sourceContextIssueChangeReasons(captured.SourceIssue, value.SourceIssue)
			got = append(got, sourceContextThreadChangeReasons(captured, value)...)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("reasons = %v, want %v", got, test.want)
			}
		})
	}

	contentChanged := sourceContextStateFixture()
	contentChanged.CommentThread[0].Content = "Changed"
	if got := sourceContextThreadChangeDetails(captured, contentChanged).ChangedCommentIDs; !reflect.DeepEqual(got, []string{"comment-1"}) {
		t.Fatalf("changed comment ids = %v, want comment-1", got)
	}

}

func TestSourceContextAttachmentReferencesCompareBySourceIdentity(t *testing.T) {
	const sourceID = "11111111-2222-4333-8444-555555555555"
	const cloneID = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	stableURL := func(id string) string { return "/api/attachments/" + id + "/download" }
	captured := sourceContextStateFixture()
	current := sourceContextStateFixture()
	captured.SourceIssue.Attachments = []service.SourceContextAttachment{{
		ID: cloneID, SourceAttachmentID: sourceID, Filename: "issue.txt",
	}}
	current.SourceIssue.Attachments = []service.SourceContextAttachment{{
		ID: sourceID, Filename: "issue.txt",
	}}
	capturedDescription := "Body\n\n!file[issue.txt](" + stableURL(cloneID) + ")"
	currentDescription := "Body\n\n!file[issue.txt](https://cdn.test/workspaces/ws/" + sourceID + ".txt)"
	captured.SourceIssue.Description = &capturedDescription
	current.SourceIssue.Description = &currentDescription
	captured.CommentThread[0].Attachments = []service.SourceContextAttachment{{
		ID: cloneID, SourceAttachmentID: sourceID, Filename: "comment.txt",
	}}
	current.CommentThread[0].Attachments = []service.SourceContextAttachment{{
		ID: sourceID, Filename: "comment.txt",
	}}
	captured.CommentThread[0].Content = "See ![image](" + stableURL(cloneID) + ")"
	current.CommentThread[0].Content = "See ![image](https://cdn.test/workspaces/ws/" + sourceID + ".png)"

	if got := sourceContextIssueChangeReasons(captured.SourceIssue, current.SourceIssue); len(got) != 0 {
		t.Fatalf("equivalent issue attachment references reasons = %v, want none", got)
	}
	if got := sourceContextThreadChangeReasons(captured, current); len(got) != 0 {
		t.Fatalf("equivalent comment attachment references reasons = %v, want none", got)
	}

	current.CommentThread[0].Content = "See ![renamed](https://cdn.test/workspaces/ws/" + sourceID + ".png)"
	if got := sourceContextThreadChangeReasons(captured, current); !reflect.DeepEqual(got, []string{sourceContextChangeCommentThread}) {
		t.Fatalf("attachment label edit reasons = %v, want comment thread", got)
	}
}

func TestSourceContextChangeReasonsTreatEveryCommentAsPartOfTheThread(t *testing.T) {
	fixture := func() (service.SourceContextSnapshot, service.SourceContextSnapshot) {
		captured := sourceContextStateFixture()
		selected := captured.CommentThread[0]
		history := selected
		history.ID = "comment-history"
		history.Content = "Earlier thread comment"
		captured.CommentThread = []service.SourceContextCommentSnapshot{history, selected}
		current := captured
		current.CommentThread = append([]service.SourceContextCommentSnapshot(nil), captured.CommentThread...)
		return captured, current
	}

	tests := []struct {
		name   string
		mutate func(*service.SourceContextSnapshot)
		want   []string
	}{
		{
			name: "other comment content",
			mutate: func(value *service.SourceContextSnapshot) {
				value.CommentThread[0].Content = "Edited thread comment"
			},
			want: []string{sourceContextChangeCommentThread},
		},
		{
			name: "other comment attachments",
			mutate: func(value *service.SourceContextSnapshot) {
				value.CommentThread[0].Attachments = nil
			},
			want: []string{sourceContextChangeCommentThread},
		},
		{
			name: "anchor comment content",
			mutate: func(value *service.SourceContextSnapshot) {
				value.CommentThread[1].Content = "Edited selected comment"
			},
			want: []string{sourceContextChangeCommentThread},
		},
		{
			name: "anchor comment attachments",
			mutate: func(value *service.SourceContextSnapshot) {
				value.CommentThread[1].Attachments = nil
			},
			want: []string{sourceContextChangeCommentThread},
		},
		{
			name: "multiple comments",
			mutate: func(value *service.SourceContextSnapshot) {
				value.CommentThread[0].Content = "Edited thread comment"
				value.CommentThread[1].Content = "Edited selected comment"
			},
			want: []string{sourceContextChangeCommentThread},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			captured, current := fixture()
			test.mutate(&current)
			if got := sourceContextThreadChangeReasons(captured, current); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("reasons = %v, want %v", got, test.want)
			}
		})
	}
}

func TestSourceContextThreadChangeDetailsClassifyAddedChangedAndRemovedComments(t *testing.T) {
	captured := sourceContextStateFixture()
	second := captured.CommentThread[0]
	second.ID = "comment-2"
	second.Content = "Removed comment"
	third := captured.CommentThread[0]
	third.ID = "comment-3"
	third.Content = "Unchanged comment"
	captured.CommentThread = []service.SourceContextCommentSnapshot{captured.CommentThread[0], second, third}

	changed := captured.CommentThread[0]
	changed.Content = "Changed comment"
	added := captured.CommentThread[0]
	added.ID = "comment-4"
	added.Content = "Added comment"
	current := captured
	current.CommentThread = []service.SourceContextCommentSnapshot{changed, added, third}

	details := sourceContextThreadChangeDetails(captured, current)
	if !reflect.DeepEqual(details.ChangedCommentIDs, []string{"comment-1"}) {
		t.Fatalf("changed comment ids = %v, want comment-1", details.ChangedCommentIDs)
	}
	if len(details.AddedComments) != 1 || details.AddedComments[0].ID != "comment-4" {
		t.Fatalf("added comments = %#v, want comment-4", details.AddedComments)
	}
	if !reflect.DeepEqual(details.RemovedCommentIDs, []string{"comment-2"}) {
		t.Fatalf("removed comment ids = %v, want comment-2", details.RemovedCommentIDs)
	}
}

func TestSourceContextIssueDescriptionSeparatesBodyFromAttachmentReferences(t *testing.T) {
	const firstID = "11111111-2222-4333-8444-555555555555"
	const secondID = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	pointer := func(value string) *string { return &value }
	attachment := func(id, filename string) service.SourceContextAttachment {
		return service.SourceContextAttachment{ID: id, Filename: filename, ContentType: "text/plain", SizeBytes: 12}
	}
	issue := func(description string, attachments ...service.SourceContextAttachment) service.SourceContextIssueSnapshot {
		return service.SourceContextIssueSnapshot{Title: "Title", Description: pointer(description), Attachments: attachments}
	}
	stableURL := func(id string) string { return "/api/attachments/" + id + "/download" }

	tests := []struct {
		name            string
		captured        service.SourceContextIssueSnapshot
		current         service.SourceContextIssueSnapshot
		wantReasons     []string
		wantAttachments []sourceContextDescriptionAttachmentChange
	}{
		{
			name:        "removing an image node is an attachment-reference change only",
			captured:    issue("Body\n\n![github.png]("+stableURL(firstID)+")", attachment(firstID, "github.png")),
			current:     issue("Body", attachment(firstID, "github.png")),
			wantReasons: []string{sourceContextChangeIssueDescriptionAttachments},
			wantAttachments: []sourceContextDescriptionAttachmentChange{{
				Kind: "removed", AttachmentID: firstID, Filename: "github.png",
			}},
		},
		{
			name:        "adding a file card is an attachment-reference change only",
			captured:    issue("Body", attachment(firstID, "github.png"), attachment(secondID, "report.pdf")),
			current:     issue("Body\n\n!file[report.pdf]("+stableURL(secondID)+")", attachment(firstID, "github.png"), attachment(secondID, "report.pdf")),
			wantReasons: []string{sourceContextChangeIssueDescriptionAttachments},
			wantAttachments: []sourceContextDescriptionAttachmentChange{{
				Kind: "added", AttachmentID: secondID, Filename: "report.pdf",
			}},
		},
		{
			name:            "ordinary body edit remains an issue-description change",
			captured:        issue("Old\n\n![github.png]("+stableURL(firstID)+")", attachment(firstID, "github.png")),
			current:         issue("New\n\n![github.png]("+stableURL(firstID)+")", attachment(firstID, "github.png")),
			wantReasons:     []string{sourceContextChangeIssueDescription},
			wantAttachments: []sourceContextDescriptionAttachmentChange{},
		},
		{
			name:            "external image remains ordinary description content",
			captured:        issue("Body\n\n![external](https://example.test/image.png)"),
			current:         issue("Body"),
			wantReasons:     []string{sourceContextChangeIssueDescription},
			wantAttachments: []sourceContextDescriptionAttachmentChange{},
		},
		{
			name:            "database attachment inventory change without markdown change is ignored",
			captured:        issue("Body", attachment(firstID, "github.png")),
			current:         issue("Body"),
			wantReasons:     []string{},
			wantAttachments: []sourceContextDescriptionAttachmentChange{},
		},
		{
			name:        "attachment label rename is explicit",
			captured:    issue("!file[old.pdf]("+stableURL(firstID)+")", attachment(firstID, "old.pdf")),
			current:     issue("!file[new.pdf]("+stableURL(firstID)+")", attachment(firstID, "old.pdf")),
			wantReasons: []string{sourceContextChangeIssueDescriptionAttachments},
			wantAttachments: []sourceContextDescriptionAttachmentChange{{
				Kind: "replaced", AttachmentID: firstID, Filename: "new.pdf", PreviousFilename: "old.pdf",
			}},
		},
		{
			name:        "legacy public storage URL is matched through captured attachment id",
			captured:    issue("Body\n\n![github.png](https://cdn.test/workspaces/ws/"+firstID+".png)", attachment(firstID, "github.png")),
			current:     issue("Body", attachment(firstID, "github.png")),
			wantReasons: []string{sourceContextChangeIssueDescriptionAttachments},
			wantAttachments: []sourceContextDescriptionAttachmentChange{{
				Kind: "removed", AttachmentID: firstID, Filename: "github.png",
			}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := sourceContextIssueChangeDetails(test.captured, test.current)
			if !reflect.DeepEqual(got.Reasons, test.wantReasons) {
				t.Fatalf("reasons = %v, want %v", got.Reasons, test.wantReasons)
			}
			if !reflect.DeepEqual(got.DescriptionAttachmentChanges, test.wantAttachments) {
				t.Fatalf("attachment changes = %#v, want %#v", got.DescriptionAttachmentChanges, test.wantAttachments)
			}
		})
	}
}
