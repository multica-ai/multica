package daemon

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multica-ai/multica/server/internal/daemon/localhost"
	"github.com/multica-ai/multica/server/internal/daemon/p2p"
	"github.com/multica-ai/multica/server/internal/localconversation/store"
	ma "github.com/multiformats/go-multiaddr"
)

func TestDeviceLinkNavigationReadsTheHostsWindow(t *testing.T) {
	keyA, idA := newLinkKey(t)
	keyB, idB := newLinkKey(t)
	account := &linkAccount{peers: []p2p.Peer{{ID: idA, DeviceName: "Viewer"}, {ID: idB, DeviceName: "Host"}}}
	viewer := newLinkDevice(t, account, keyA, "viewer-token")
	host := newLinkDevice(t, account, keyB, "host-token")
	var addresses []ma.Multiaddr
	for _, address := range host.p2p.Status().ListenAddrs {
		addresses = append(addresses, ma.StringCast(address))
	}
	viewer.p2p.AddAddrs(peer.AddrInfo{ID: idB, Addrs: addresses})
	if err := host.link.setEnabled(true); err != nil {
		t.Fatal(err)
	}
	base := "/local/devices/" + idB.String() + "/local/v2"
	token := localhost.RendererToken(viewer.token)
	if status, body := viewer.do(t, token, http.MethodPost, base+"/sessions", map[string]any{"id": "nav-p2p", "provider": "claude"}); status != 201 {
		t.Fatalf("create=%d %v", status, body)
	}
	writer := host.svc.Store().NewWriter(store.WriterOptions{})
	defer writer.Close()
	for i := 0; i < 35; i++ {
		id := fmt.Sprintf("t%d", i)
		batch := store.Batch{ConversationID: "nav-p2p", StartTurn: &store.NewTurn{ID: id, StartedAt: time.Now()}, Messages: []store.NewMessage{{ID: "m" + id, TurnID: id, Role: "user", Content: id, CreatedAt: time.Now()}}, CompleteTurn: &store.TurnCompletion{TurnID: id, Status: "completed", CompletedAt: time.Now()}}
		if _, err := writer.Submit(context.Background(), batch); err != nil {
			t.Fatal(err)
		}
	}
	status, count := viewer.do(t, token, http.MethodGet, base+"/sessions/nav-p2p/navigation", nil)
	if status != 200 || count["node_count"] != float64(35) || len(count["turns"].([]any)) != 0 {
		t.Fatalf("count=%d %v", status, count)
	}
	status, page := viewer.do(t, token, http.MethodGet, base+"/sessions/nav-p2p/navigation?node_index=4&before=2&after=3", nil)
	if status != 200 || page["range"].(map[string]any)["start_index"] != float64(2) || len(page["turns"].([]any)) != 6 {
		t.Fatalf("range=%d %v", status, page)
	}
	status, metadata := viewer.do(t, token, http.MethodGet, base+"/sessions/nav-p2p/navigation?node_index=4&include_body=false", nil)
	if status != 200 || len(metadata["turns"].([]any)) != 0 || len(metadata["nodes"].([]any)) == 0 || metadata["directory_range"].(map[string]any)["has_after"] != true || metadata["range"] != nil {
		t.Fatalf("metadata=%d %v", status, metadata)
	}
	status, _ = viewer.do(t, token, http.MethodGet, base+"/sessions/nav-p2p/navigation?store_generation=old", nil)
	if status != 409 {
		t.Fatalf("generation=%d", status)
	}
}
