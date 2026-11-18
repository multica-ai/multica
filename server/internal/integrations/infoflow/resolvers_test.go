package infoflow

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

func TestInfoflowSessionRouting(t *testing.T) {
	msg := channel.InboundMessage{Source: channel.Source{ChatID: "g:42"}}
	key, config := infoflowSessionRouting(msg)
	if key != "g:42" {
		t.Errorf("binding key = %q, want g:42", key)
	}
	if config != nil {
		t.Errorf("binding config = %v, want nil", config)
	}
}

func TestNewInfoFlowResolverSet_RequiredFields(t *testing.T) {
	// A nil *db.Queries is fine here: we only assert the bundle is wired, not
	// that its resolvers can reach the database.
	set := NewInfoFlowResolverSet(nil, nil, nil)
	if set.Installation == nil || set.Identity == nil || set.Dedup == nil ||
		set.Session == nil || set.Audit == nil {
		t.Fatalf("required resolvers missing: %+v", set)
	}
	if set.OriginType != "infoflow_chat" {
		t.Errorf("OriginType = %q, want infoflow_chat", set.OriginType)
	}
	if set.Replier != nil {
		t.Errorf("Replier = %v, want nil", set.Replier)
	}
}

func TestDecodeRawEvent_Errors(t *testing.T) {
	if _, err := decodeRawEvent(channel.InboundMessage{Raw: []byte(`not-json`)}); err == nil {
		t.Error("expected an error for invalid raw")
	}
	if _, err := decodeRawEvent(channel.InboundMessage{Raw: []byte(`{"message_id":"m"}`)}); err == nil {
		t.Error("expected an error when app_id is missing")
	}
}
