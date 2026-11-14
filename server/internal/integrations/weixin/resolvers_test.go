package weixin

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

type captureMediaSession struct {
	chatSession
	input engine.BindMediaInput
}

func (s *captureMediaSession) BindMediaRefs(_ context.Context, in engine.BindMediaInput) error {
	s.input = in
	return nil
}

func TestMediaBindingPreservesFailureNoticeAndIssueDestination(t *testing.T) {
	session := &captureMediaSession{}
	binder := &sessionBinder{session: session}
	params := engine.BindMediaParams{
		IssueID:              dbid.NewV7(),
		IssueDescriptionBase: pgtype.Text{String: "Original description", Valid: true},
		IssueCommandText:     "/issue Review image",
		Body:                 "[Image]\n[WeChat attachment unavailable: file 1; send it again or upload it in Multica.]",
	}
	if _, err := binder.BindMedia(context.Background(), params); err != nil {
		t.Fatal(err)
	}
	if session.input.Body != params.Body || session.input.IssueID != params.IssueID || session.input.IssueDescriptionBase != params.IssueDescriptionBase || session.input.IssueCommandText != params.IssueCommandText {
		t.Fatal("media binding dropped the failure notice or /issue destination")
	}
}
