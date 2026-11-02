package localhost

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestComputerHostRequiresMainAndNeverUsesViewerRouting(t *testing.T) {
	srv, _, v := newV2Fixture(t)
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/local/v2/computer-host"
	h := v2Headers("user-1")
	h.Set("Authorization", "Bearer "+RendererToken(v2Token))
	if c, r, err := websocket.DefaultDialer.Dial(url, h); err == nil {
		c.Close()
		t.Fatal("renderer connected")
	} else if r.StatusCode != http.StatusForbidden {
		t.Fatal(r.StatusCode)
	}
	h = v2Headers("user-1")
	c, _, err := websocket.DefaultDialer.Dial(url, h)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		raw, err := v.computer.request(ctx, 1, "conversation", "computer.control", map[string]any{"op": "status"})
		if err == nil && string(raw) != `{"available":true}` {
			t.Errorf("reply=%s", raw)
		}
		done <- err
	}()
	var message map[string]any
	if err := c.ReadJSON(&message); err != nil {
		t.Fatal(err)
	}
	if message["type"] != "request" {
		t.Fatal(message)
	}
	if err := c.WriteJSON(map[string]any{"id": message["id"], "result": json.RawMessage(`{"available":true}`)}); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
func TestComputerHostCancellationDoesNotReplay(t *testing.T) {
	srv, _, v := newV2Fixture(t)
	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/local/v2/computer-host", v2Headers("user-1"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := v.computer.request(ctx, 1, "c", "computer.control", map[string]any{"op": "action"})
		done <- err
	}()
	var request, cancelled map[string]any
	if err := c.ReadJSON(&request); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := c.ReadJSON(&cancelled); err != nil {
		t.Fatal(err)
	}
	if cancelled["type"] != "cancel" || cancelled["id"] != request["id"] {
		t.Fatal(cancelled)
	}
	if <-done == nil {
		t.Fatal("cancel succeeded")
	}
}
