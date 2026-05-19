package main

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/channel/facade"
	"github.com/multica-ai/multica/server/internal/channel/facadeimpl"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestChannelAttachmentService_CreateIssueAttachment(t *testing.T) {
	requirePool(t)

	ctx := context.Background()
	queries := db.New(testPool)
	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	svc := facadeimpl.NewAttachmentService(queries)

	att, err := svc.UploadIssueAttachment(ctx, facade.UploadIssueAttachmentReq{
		WorkspaceID:  parseUUID(testWorkspaceID),
		IssueID:      parseUUID(issueID),
		UploaderID:   parseUUID(testUserID),
		UploaderType: "member",
		Filename:     "channel-note.png",
		URL:          "https://cdn.example.com/channel-note.png",
		ContentType:  "image/png",
		SizeBytes:    42,
	})
	if err != nil {
		t.Fatalf("UploadIssueAttachment: %v", err)
	}

	got, err := queries.GetAttachment(ctx, db.GetAttachmentParams{
		ID:          att.ID,
		WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("GetAttachment: %v", err)
	}
	if util.UUIDToString(got.IssueID) != issueID {
		t.Fatalf("issue_id = %s, want %s", util.UUIDToString(got.IssueID), issueID)
	}
	if util.UUIDToString(got.UploaderID) != testUserID {
		t.Fatalf("uploader_id = %s, want %s", util.UUIDToString(got.UploaderID), testUserID)
	}
	if got.UploaderType != "member" || got.Filename != "channel-note.png" || got.Url != "https://cdn.example.com/channel-note.png" || got.ContentType != "image/png" || got.SizeBytes != 42 {
		t.Fatalf("attachment fields not preserved: %+v", got)
	}
}
