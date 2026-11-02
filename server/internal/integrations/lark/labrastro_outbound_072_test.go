package lark

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type conversationMediaQueries struct {
	*referenceLinkQueryFake
	authorizationErr error
	authorizations   int
}

func (q *conversationMediaQueries) AuthorizeConversationTask(context.Context, pgtype.UUID, pgtype.UUID) error {
	q.authorizations++
	return q.authorizationErr
}

func TestExternalConversationReplyKeepsTextOnlyBoundary(t *testing.T) {
	for _, body := range []string{"", "See [issue](mention://issue/" + issueUUID + ")", "<at user_id=\"all\">all</at>"} {
		t.Run(body, func(t *testing.T) {
			p, base, api := newMediaTestPatcher(t)
			base.binding.Config = []byte(`{"conversation":{"id":"grant","authorized_by":"member"}}`)
			q := &conversationMediaQueries{referenceLinkQueryFake: &referenceLinkQueryFake{fakePatcherQueries: base}}
			p.queries = q
			p.cfg.AppURL = "https://internal.example"
			p.SetAttachments(fakeObjectStore{objects: map[string][]byte{"chart.png": pngBytes}})
			p.spawn = func(func()) { t.Fatal("external conversation spawned attachment delivery") }
			base.attachments = []db.Attachment{imageRow(t, "aaaa2222-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "chart.png")}
			task := uuidFromString(t, "ee777777-ee77-ee77-ee77-eeeeeeeeeeee")
			message := uuidFromString(t, "dddd4444-dddd-dddd-dddd-dddddddddddd")
			payload := newChatDoneEvent(t, task, base.binding.ChatSessionID, message, body).Payload
			if err := p.sendChatReply(context.Background(), testCreds(), base.binding, "", base.installation.WorkspaceID, payload); err != nil {
				t.Fatal(err)
			}
			if len(base.attachmentLookups) != 0 || q.workspaceReads != 0 || len(api.images) != 0 || len(api.files) != 0 {
				t.Fatal("external reply accessed attachments or internal link metadata")
			}
			if body == "" && len(api.textSent)+len(api.mdCardSent) != 0 {
				t.Fatal("attachment-only external reply produced a message")
			}
			if strings.Contains(body, "<") && (len(api.textSent) != 1 || !api.textSent[0].Literal || api.textSent[0].Text != body) {
				t.Fatal("untrusted markup lost literal rendering")
			}
		})
	}
}

func TestMediaCredentialsRecheckFrozenConversationGrant(t *testing.T) {
	p, base, _ := newMediaTestPatcher(t)
	q := &conversationMediaQueries{referenceLinkQueryFake: &referenceLinkQueryFake{fakePatcherQueries: base}}
	p.queries = q
	task := uuidFromString(t, "ee777777-ee77-ee77-ee77-eeeeeeeeeeee")
	base.task = db.AgentTaskQueue{ID: task, AgentID: base.installation.AgentID, ConversationRootTaskID: task}
	ctx, finish, acquired := p.claimMediaDelivery(context.Background(), task, base.binding)
	if !acquired {
		t.Fatal("failed to acquire fixture lease")
	}
	defer finish()
	if _, err := p.currentMediaCredentials(ctx, testCreds(), base.binding, base.installation.WorkspaceID); err != nil {
		t.Fatal(err)
	}
	q.authorizationErr = channel.ErrConversationDenied
	if _, err := p.currentMediaCredentials(ctx, testCreds(), base.binding, base.installation.WorkspaceID); !errors.Is(err, channel.ErrConversationDenied) {
		t.Fatalf("revoked grant retained delivery authority: %v", err)
	}
	if q.authorizations != 2 {
		t.Fatalf("authorization calls=%d, want one before each external write", q.authorizations)
	}
}
