package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

type fakePreviewServices struct {
	calls        []string
	prepareError error
	revoked      bool
}

func (f *fakePreviewServices) Agent(_ context.Context, scope int64, conv, op, host string, port int) (any, error) {
	f.calls = append(f.calls, fmt.Sprintf("%d:%s:%s:%s:%d", scope, conv, op, host, port))
	return map[string]bool{"ok": true}, nil
}
func (f *fakePreviewServices) Prepare(_ context.Context, scope int64, conv, raw string) error {
	f.calls = append(f.calls, fmt.Sprintf("%d:%s:prepare:%s", scope, conv, raw))
	return f.prepareError
}
func (f *fakePreviewServices) Revoke() { f.revoked = true }

func TestServicePreviewToolsBindTheCurrentConversationAndDedupeWrites(t *testing.T) {
	f := newSideFixture(t)
	preview := &fakePreviewServices{}
	f.svc.SetPreviewServices(preview)
	args := `{"port":3000,"request_id":"register"}`
	for range 2 {
		if _, err := f.call(t, "service_preview_register", args); err != nil {
			t.Fatal(err)
		}
	}
	if len(preview.calls) != 1 || preview.calls[0] != fmt.Sprintf("%d:conv-1:register:localhost:3000", f.scopeID) {
		t.Fatalf("incorrect binding/duplicate: %v", preview.calls)
	}
	if _, err := f.call(t, "service_preview_register", `{"port":5173,"request_id":"register"}`); err == nil {
		t.Fatal("conflicting receipt accepted")
	}
	if _, err := f.call(t, "service_preview_register", `{"port":3000,"conversation_id":"another","request_id":"foreign"}`); err == nil {
		t.Fatal("caller chose another conversation")
	}
	for _, args := range []string{`{"port":0,"request_id":"bad"}`, `{"port":65536,"request_id":"bad"}`, `{"port":3000}`} {
		if _, err := f.call(t, "service_preview_register", args); err == nil {
			t.Fatalf("invalid arguments accepted: %s", args)
		}
	}
	if !f.tools["service_preview_list"].ReadOnly || f.tools["service_preview_register"].ReadOnly {
		t.Fatal("incorrect tool effect annotations")
	}
	if !strings.Contains(systemPrompt("workspace", false, ""), "call service_preview_register") {
		t.Fatal("providers were not instructed to register started services")
	}
}

func TestSideBrowserOpenPreparesTheServiceBeforeOpeningAndReportsUnreadyPorts(t *testing.T) {
	f := newSideFixture(t)
	preview := &fakePreviewServices{}
	f.svc.SetPreviewServices(preview)
	if _, err := f.call(t, "side_browser_open", `{"url":"http://localhost:3000/","request_id":"open"}`); err != nil {
		t.Fatal(err)
	}
	if len(preview.calls) != 1 || !strings.Contains(preview.calls[0], "conv-1:prepare:http://localhost:3000/") {
		t.Fatalf("no automatic preparation: %v", preview.calls)
	}
	preview.prepareError = fmt.Errorf("target_unreachable")
	f.mu.Lock()
	count := len(f.hosts)
	f.mu.Unlock()
	if _, err := f.call(t, "side_browser_open", `{"url":"http://localhost:5173/","request_id":"unready"}`); err == nil {
		t.Fatal("opened before service was ready")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.hosts) != count {
		t.Fatal("unready service reached the desktop")
	}
}
