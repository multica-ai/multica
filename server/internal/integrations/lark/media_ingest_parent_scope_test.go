package lark

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

func TestFeishuMediaResolver_GroupReplyWithOwnFileDoesNotAttachParent(t *testing.T) {
	sender := &fakeSender{
		getMessageByID: map[string][]LarkMessage{
			"om_parent": {{
				MessageID:   "om_parent",
				MessageType: "file",
				Content:     `{"file_key":"parent_file","file_name":"parent.zip"}`,
			}},
		},
		downloadedByKey: map[string]DownloadedResource{
			"own_file": {
				Data:        []byte("own"),
				ContentType: "application/zip",
				Filename:    "own.zip",
				SizeBytes:   3,
			},
			"parent_file": {
				Data:        []byte("parent"),
				ContentType: "application/zip",
				Filename:    "parent.zip",
				SizeBytes:   6,
			},
		},
	}
	resolver := NewFeishuMediaResolver(
		sender,
		fakeCreds{secret: "plain"},
		&fakeMediaStorage{},
		&fakeMediaLedger{},
		newDiscardLogger(),
	)
	trigger := InboundMessage{
		MessageID:   "om_trigger",
		MessageType: "file",
		ChatType:    ChatTypeGroup,
		ChatID:      "oc_group",
		ParentID:    "om_parent",
		Body:        "[File]",
		Content:     `{"file_key":"own_file","file_name":"own.zip"}`,
	}

	got := resolver.ResolveMedia(
		context.Background(),
		testMediaInstallation(t),
		engine.ResolvedIdentity{},
		uuidFromString(t, "22222222-2222-2222-2222-222222222222"),
		uuidFromString(t, "33333333-3333-4333-8333-333333333333"),
		channelMessageFromLark(trigger),
	)

	if len(sender.getMessageCalls) != 0 {
		t.Fatalf("parent fetch calls = %v, want none when trigger has its own media", sender.getMessageCalls)
	}
	if len(sender.downloadCalls) != 1 || sender.downloadCalls[0].FileKey != "own_file" {
		t.Fatalf("download calls = %+v, want only own_file", sender.downloadCalls)
	}
	if len(got.MediaRefs) != 1 || got.MediaRefs[0].Filename != "own.zip" {
		t.Fatalf("media refs = %+v, want only own.zip", got.MediaRefs)
	}
}
