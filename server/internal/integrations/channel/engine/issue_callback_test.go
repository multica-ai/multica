package engine

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type fakeCallbackRegistrar struct {
	mu  sync.Mutex
	got []IssueCallback
	err error
}

func (f *fakeCallbackRegistrar) RegisterIssueCallback(_ context.Context, cb IssueCallback) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, cb)
	return f.err
}

func harnessWithCallbacks(t *testing.T, reg IssueCallbackRegistrar) *harness {
	t.Helper()
	h := newHarness(t)
	h.router.Register(channel.TypeFeishu, ResolverSet{
		Installation: h.inst, Identity: h.ident, Dedup: h.dedup, Session: h.binder,
		Audit: h.audit, Replier: h.replier, Typing: h.typing, Media: h.media,
		IssueCallbacks: reg, OriginType: "lark_chat",
	})
	return h
}

func TestRouter_IssueCommandRegistersCallbackForTheCommandMessage(t *testing.T) {
	reg := &fakeCallbackRegistrar{}
	h := harnessWithCallbacks(t, reg)
	issueID := uuidFromString(t, "77777777-7777-7777-7777-777777777777")
	h.binder.appendResult = AppendResult{DedupMarked: true, IssueCommand: &IssueCommand{Title: "Fix login"}}
	h.issues.result = service.IssueCreateResult{Issue: db.Issue{ID: issueID, Number: 42, Title: "Fix login"}}
	msg := p2pMessage(t)
	msg.Source.ChatType = channel.ChatTypeGroup
	msg.Source.ThreadID = "omt_topic"
	msg.AddressedToBot = true

	if err := h.router.Handle(context.Background(), msg); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	inst := activeResolved(t)
	want := IssueCallback{
		WorkspaceID: inst.WorkspaceID, IssueID: issueID, InstallationID: inst.ID,
		ChannelType: channel.TypeFeishu, ChatID: "oc_chat", ChatType: channel.ChatTypeGroup,
		ThreadID: "omt_topic", AnchorMessageID: "om-1", RegisteredBy: h.ident.id.UserID,
	}
	if len(reg.got) != 1 || reg.got[0] != want {
		t.Fatalf("registered = %+v, want [%+v]", reg.got, want)
	}
}

func TestRouter_IssueCommandCallbackFailureStillCreatesIssue(t *testing.T) {
	reg := &fakeCallbackRegistrar{err: errors.New("db down")}
	h := harnessWithCallbacks(t, reg)
	h.binder.appendResult = AppendResult{DedupMarked: true, IssueCommand: &IssueCommand{Title: "Fix login"}}
	h.issues.result = service.IssueCreateResult{Issue: db.Issue{ID: uuidFromString(t, "77777777-7777-7777-7777-777777777777"), Number: 42}}
	if err := h.router.Handle(context.Background(), p2pMessage(t)); err != nil {
		t.Fatalf("a failed registration must not fail the command: %v", err)
	}
	if !h.issues.called || len(reg.got) != 1 {
		t.Fatalf("issue created = %v, registrations = %d", h.issues.called, len(reg.got))
	}
}

func TestRouter_IssueCommandDuplicateRegistersNoCallback(t *testing.T) {
	reg := &fakeCallbackRegistrar{}
	h := harnessWithCallbacks(t, reg)
	h.binder.appendResult = AppendResult{DedupMarked: true, IssueCommand: &IssueCommand{Title: "Existing"}}
	duplicate := db.Issue{ID: uuidFromString(t, "88888888-8888-4888-8888-888888888888"), Number: 44}
	h.issues.result = service.IssueCreateResult{DuplicateIssue: &duplicate}
	h.issues.err = service.ErrActiveDuplicate
	if err := h.router.Handle(context.Background(), p2pMessage(t)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(reg.got) != 0 {
		t.Fatalf("duplicate registered a callback: %+v", reg.got)
	}
}

func TestIssueCallbackForRequiresReplyAddress(t *testing.T) {
	inst := activeResolved(t)
	issueID := uuidFromString(t, "77777777-7777-7777-7777-777777777777")
	sender := uuidFromString(t, "44444444-4444-4444-4444-444444444444")
	msg := p2pMessage(t)
	msg.MessageID = ""
	if _, ok := issueCallbackFor(inst, msg, issueID, sender); ok {
		t.Fatal("callback registered without a message to reply to")
	}
	msg = p2pMessage(t)
	msg.Source.ChatID = ""
	if _, ok := issueCallbackFor(inst, msg, issueID, sender); ok {
		t.Fatal("callback registered without a chat")
	}
}
