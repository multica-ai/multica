package localhost

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/multica-ai/multica/server/internal/localconversation/computer"
)

func dialComputerHost(t *testing.T, srv *httptest.Server) *websocket.Conn {
	t.Helper()
	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/local/v2/computer-host", v2Headers("user-1"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// computerParamsFixture is a valid Computer capability whose runtime files exist.
func computerParamsFixture(t *testing.T, version, user string) map[string]any {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"Multica", "computer-host.cjs"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return map[string]any{
		"user": map[string]any{"id": user},
		"capabilities": map[string]any{"computer": map[string]any{
			"version": version,
			"prompts": map[string]any{"conversation": "conversation guide", "task": "task guide", "taskBrief": "brief"},
			"runtime": map[string]any{"executable": filepath.Join(dir, "Multica"), "entry": filepath.Join(dir, "computer-host.cjs")},
		}},
	}
}

func rpc(t *testing.T, c *websocket.Conn, id any, method string, params any) map[string]any {
	t.Helper()
	msg := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		msg["params"] = params
	}
	if id != nil {
		msg["id"] = id
	}
	if err := c.WriteJSON(msg); err != nil {
		t.Fatal(err)
	}
	if id == nil {
		return nil
	}
	var reply map[string]any
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := c.ReadJSON(&reply); err != nil {
		t.Fatal(err)
	}
	return reply
}

func initializeComputer(t *testing.T, c *websocket.Conn, params map[string]any) map[string]any {
	t.Helper()
	params["protocolVersion"] = ComputerHostProtocolVersion
	reply := rpc(t, c, 1, "initialize", params)
	rpc(t, c, nil, "notifications/initialized", nil)
	return reply
}

// waitComputer polls until the daemon side settles after a notification.
func waitComputer(t *testing.T, v *V2, want bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := v.computer.Computer(); ok == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("Computer() never became %v", want)
}

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
	c := dialComputerHost(t, srv)
	initializeComputer(t, c, computerParamsFixture(t, computer.CapabilityVersion, "u"))
	waitComputer(t, v, true)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		raw, err := v.computer.Control(ctx, "1.0.4", map[string]any{"op": "status"})
		if err == nil && string(raw) != `{"available":true}` {
			t.Errorf("reply=%s", raw)
		}
		done <- err
	}()
	var request struct {
		ID     string `json:"id"`
		Method string `json:"method"`
		Params struct {
			CapabilityVersion string         `json:"capabilityVersion"`
			Envelope          map[string]any `json:"envelope"`
		} `json:"params"`
	}
	if err := c.ReadJSON(&request); err != nil {
		t.Fatal(err)
	}
	if request.Method != "computer/control" || request.Params.CapabilityVersion != "1.0.4" || request.Params.Envelope["op"] != "status" {
		t.Fatalf("request=%+v", request)
	}
	if err := c.WriteJSON(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": json.RawMessage(`{"available":true}`)}); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestComputerHostCancellationDoesNotReplay(t *testing.T) {
	srv, _, v := newV2Fixture(t)
	c := dialComputerHost(t, srv)
	initializeComputer(t, c, computerParamsFixture(t, computer.CapabilityVersion, "u"))
	waitComputer(t, v, true)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := v.computer.Control(ctx, computer.CapabilityVersion, map[string]any{"op": "action"})
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
	params, _ := cancelled["params"].(map[string]any)
	if cancelled["method"] != "notifications/cancelled" || params["requestId"] != request["id"] {
		t.Fatal(cancelled)
	}
	if <-done == nil {
		t.Fatal("cancel succeeded")
	}
}

func TestComputerHostHandshakeStateMachine(t *testing.T) {
	srv, _, v := newV2Fixture(t)
	v.SetDaemonIdentity(func() DaemonIdentity { return DaemonIdentity{Version: "0.7.2", UserID: "u"} })
	c := dialComputerHost(t, srv)
	if reply := rpc(t, c, 1, "computer/anything", nil); reply["error"].(map[string]any)["message"] != "not_initialized" {
		t.Fatalf("call before initialize = %v", reply)
	}
	if reply := rpc(t, c, 2, "initialize", map[string]any{"protocolVersion": "1.1.0"}); reply["error"].(map[string]any)["message"] != "protocol_incompatible" {
		t.Fatalf("incompatible protocol = %v", reply)
	}
	if _, ok := v.computer.Computer(); ok {
		t.Fatal("capability without a handshake")
	}
	params := computerParamsFixture(t, "1.0.9", "u")
	params["protocolVersion"] = "1.0.3"
	reply := rpc(t, c, 3, "initialize", params)
	result, _ := reply["result"].(map[string]any)
	status := result["capabilities"].(map[string]any)["computer"].(map[string]any)
	if status["status"] != "enabled" || result["user"].(map[string]any)["id"] != "u" || result["serverInfo"].(map[string]any)["version"] != "0.7.2" {
		t.Fatalf("initialize result = %v", reply)
	}
	if _, ok := v.computer.Computer(); ok {
		t.Fatal("capability offered before notifications/initialized")
	}
	if reply := rpc(t, c, 4, "initialize", params); reply["error"].(map[string]any)["message"] != "already_initialized" {
		t.Fatalf("second initialize = %v", reply)
	}
	rpc(t, c, nil, "notifications/initialized", nil)
	waitComputer(t, v, true)
	if capability, _ := v.computer.Computer(); capability.Version != "1.0.9" || capability.UserID != "u" || capability.Prompts.Task != "task guide" {
		t.Fatalf("capability = %+v", capability)
	}
	if reply := rpc(t, c, 5, "computer/unknown", nil); reply["error"].(map[string]any)["message"] != "method_not_found" {
		t.Fatalf("unknown method = %v", reply)
	}
}

func TestComputerHostRejectsIncompatibleOrInvalidCapabilities(t *testing.T) {
	for name, mutate := range map[string]func(map[string]any){
		"minor version": func(p map[string]any) {
			p["capabilities"].(map[string]any)["computer"].(map[string]any)["version"] = "1.1.0"
		},
		"empty prompt": func(p map[string]any) {
			p["capabilities"].(map[string]any)["computer"].(map[string]any)["prompts"].(map[string]any)["task"] = ""
		},
		"missing runtime": func(p map[string]any) {
			p["capabilities"].(map[string]any)["computer"].(map[string]any)["runtime"].(map[string]any)["entry"] = "/missing/computer-host.cjs"
		},
	} {
		t.Run(name, func(t *testing.T) {
			srv, _, v := newV2Fixture(t)
			c := dialComputerHost(t, srv)
			params := computerParamsFixture(t, computer.CapabilityVersion, "u")
			mutate(params)
			reply := initializeComputer(t, c, params)
			status := reply["result"].(map[string]any)["capabilities"].(map[string]any)["computer"].(map[string]any)
			if status["status"] == "enabled" {
				t.Fatalf("accepted: %v", status)
			}
			// Let notifications/initialized land before asserting nothing is offered.
			rpc(t, c, 9, "computer/unknown", nil)
			if _, ok := v.computer.Computer(); ok {
				t.Fatal("unusable capability offered")
			}
		})
	}
}

func TestComputerHostClientUpdateReplacesAccount(t *testing.T) {
	srv, _, v := newV2Fixture(t)
	c := dialComputerHost(t, srv)
	params := computerParamsFixture(t, computer.CapabilityVersion, "u")
	initializeComputer(t, c, params)
	waitComputer(t, v, true)
	params["user"] = map[string]any{"id": nil}
	rpc(t, c, nil, "notifications/client/updated", params)
	deadline := time.Now().Add(3 * time.Second)
	for {
		if capability, _ := v.computer.Computer(); capability.UserID == "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("signed-out Desktop kept its account")
		}
		time.Sleep(10 * time.Millisecond)
	}
	delete(params, "capabilities")
	rpc(t, c, nil, "notifications/client/updated", params)
	waitComputer(t, v, false)
}
