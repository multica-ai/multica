package sharecrm

import (
	"encoding/json"
	"net/netip"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

func TestHasMedia_FromRawImages(t *testing.T) {
	raw, _ := json.Marshal(sharecrmRawEvent{
		AppID: "app-1",
		Images: []botImageRef{{
			URL:      "https://img.example/sign",
			Filename: "a.jpg",
		}},
	})
	r := &sharecrmMediaResolver{}
	if !r.HasMedia(channel.InboundMessage{Raw: raw}) {
		t.Fatal("HasMedia=false")
	}
}

func TestHasMedia_Empty(t *testing.T) {
	raw, _ := json.Marshal(sharecrmRawEvent{AppID: "app-1"})
	r := &sharecrmMediaResolver{}
	if r.HasMedia(channel.InboundMessage{Raw: raw}) {
		t.Fatal("HasMedia=true")
	}
}

func TestIsPublicShareCRMMediaAddr(t *testing.T) {
	if isPublicShareCRMMediaAddr(netip.MustParseAddr("127.0.0.1")) {
		t.Fatal("loopback allowed")
	}
	if isPublicShareCRMMediaAddr(netip.MustParseAddr("10.0.0.1")) {
		t.Fatal("private allowed")
	}
	if isPublicShareCRMMediaAddr(netip.MustParseAddr("100.64.1.1")) {
		t.Fatal("cgnat allowed")
	}
	if !isPublicShareCRMMediaAddr(netip.MustParseAddr("8.8.8.8")) {
		t.Fatal("public blocked")
	}
}
