package lark

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

// A post referencing the same image_key twice must yield ONE resource: the
// object key is derived from (message, type, key), so a duplicate would upload
// to the same key twice — a later failure could destroy the object an earlier
// success produced (dangling attachment) and a later success would produce two
// attachment rows for one object.
func TestMediaResourcesFromPost_DeduplicatesRepeatedKeys(t *testing.T) {
	raw := `{"content":[[{"tag":"img","image_key":"img_dup"},{"tag":"text","text":"x"}],` +
		`[{"tag":"img","image_key":"img_dup"},{"tag":"media","file_key":"file_dup"}],` +
		`[{"tag":"media","file_key":"file_dup"},{"tag":"img","image_key":"img_other"}]]}`
	got := mediaResourcesFromPost(InboundMessage{MessageID: "om_dup", MessageType: "post", Content: raw})
	if len(got) != 3 {
		t.Fatalf("resources = %d (%+v), want 3 unique", len(got), got)
	}
	seen := map[string]int{}
	for _, r := range got {
		seen[r.fetchType+"\x00"+r.key]++
	}
	for id, n := range seen {
		if n != 1 {
			t.Fatalf("resource %q appears %d times, want once", id, n)
		}
	}
}

// End-to-end shape of the same hazard: a duplicated span must upload once and
// produce exactly one ref, so no second attempt can clobber the first object.
func TestFeishuMediaResolver_DuplicatePostSpanUploadsOnce(t *testing.T) {
	sender := &fakeSender{downloaded: DownloadedResource{Data: []byte{1, 2, 3}, ContentType: "image/png", SizeBytes: 3}}
	storage := &fakeMediaStorage{}
	ledger := &fakeMediaLedger{}
	resolver := NewFeishuMediaResolver(sender, fakeCreds{secret: "plain"}, storage, ledger, newDiscardLogger())
	raw := `{"content":[[{"tag":"img","image_key":"img_same"}],[{"tag":"img","image_key":"img_same"}]]}`
	lm := InboundMessage{MessageID: "om_same", MessageType: "post", Body: flattenPostContent(raw), Content: raw}

	got := resolver.ResolveMedia(context.Background(), testMediaInstallation(t), engine.ResolvedIdentity{},
		uuidFromString(t, "22222222-2222-2222-2222-222222222222"), uuidFromString(t, "33333333-3333-4333-8333-333333333333"),
		channelMessageFromLark(lm))

	if len(sender.downloadCalls) != 1 || len(storage.uploads) != 1 {
		t.Fatalf("downloads=%d uploads=%d, want 1/1 for a duplicated span", len(sender.downloadCalls), len(storage.uploads))
	}
	if len(got.MediaRefs) != 1 {
		t.Fatalf("refs = %+v, want exactly one", got.MediaRefs)
	}
	if len(ledger.records) != 1 {
		t.Fatalf("intent records = %d, want 1", len(ledger.records))
	}
}
