package mobilelink

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multica-ai/multica/server/internal/daemon/p2p"
	"github.com/multica-ai/multica/server/internal/localconversation/timing"
)

func TestMobileClientUsesAuthenticatedDeviceStreamAndClosesWebSockets(t *testing.T) {
	var mu sync.Mutex
	devices := []map[string]any{}
	registryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path == "/v1/sessions" {
			if r.Header.Get("Authorization") != "Bearer login" {
				w.WriteHeader(401)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"session_token": "session", "expires_at": time.Now().Add(time.Hour)})
			return
		}
		if r.Header.Get("Authorization") != "Bearer session" {
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/v1/devices":
			if r.Method == "POST" {
				var device map[string]any
				_ = json.NewDecoder(r.Body).Decode(&device)
				devices = append(devices, device)
				_ = json.NewEncoder(w).Encode(device)
			} else {
				_ = json.NewEncoder(w).Encode(map[string]any{"devices": devices})
			}
		case "/v1/config":
			_ = json.NewEncoder(w).Encode(map[string]any{"relay": nil})
		default:
			http.NotFound(w, r)
		}
	}))
	defer registryServer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	key, _, _ := crypto.GenerateEd25519Key(rand.Reader)
	registryClient := &registry{url: registryServer.URL, login: "login", http: registryServer.Client()}
	desktop, err := p2p.Start(ctx, p2p.Config{Key: key, Fetch: registryClient.peers, ListenAddrs: []string{"/ip4/127.0.0.1/tcp/0"}})
	if err != nil {
		t.Fatal(err)
	}
	defer desktop.Close()
	pub, _ := crypto.MarshalPublicKey(key.GetPublic())
	if err := registryClient.call(ctx, "POST", "/v1/devices", map[string]any{"public_key": base64.StdEncoding.EncodeToString(pub), "device_name": "Test Mac", "addrs": desktop.Status().ListenAddrs}, nil); err != nil {
		t.Fatal(err)
	}
	listener, stop := desktop.Listen(deviceProtocol)
	defer stop()
	var receivedMu sync.Mutex
	var received http.Header
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMu.Lock()
		received = r.Header.Clone()
		receivedMu.Unlock()
		if r.URL.Path == "/local/v2/ws" {
			ws, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer ws.Close()
			for {
				kind, data, err := ws.ReadMessage()
				if err != nil {
					return
				}
				if ws.WriteMessage(kind, data) != nil {
					return
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"version":2,"capabilities":["device-link"]}`)
	})}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()
	config, _ := json.Marshal(configuration{RegistryURL: registryServer.URL, APIURL: "https://api.example.test", Token: "login", UserID: "account-a", StoragePath: t.TempDir(), DeviceName: "Test iPhone"})
	client, err := Start(string(config))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	// Registration runs in the background; the desktop only admits a listed phone.
	eventually(t, 15*time.Second, "phone registration", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(devices) == 2
	})
	if err := desktop.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if err := client.svc.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for client.svc.Connection(desktop.ID()) == "" && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if client.svc.Connection(desktop.ID()) == "" {
		t.Fatal("test devices did not connect")
	}
	base := client.endpoint + "/devices/" + desktop.ID().String() + "/local/v2"
	request := func(token, workspace string) *http.Response {
		req, _ := http.NewRequest("GET", base+"/hello", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Workspace-ID", workspace)
		req.Header.Set("X-Multica-User-ID", "spoofed")
		req.Header.Set("X-Multica-API-Origin", "https://other.test")
		res, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	// A dial that crossed the desktop's refresh is rejected right after its handshake.
	eventually(t, 15*time.Second, "tunnel to accept requests", func() bool {
		res := request(client.token, "workspace-a")
		_ = res.Body.Close()
		return res.StatusCode == 200
	})
	for _, test := range []struct {
		token, workspace string
		status           int
	}{{"wrong", "ws", 403}, {client.token, "", 400}, {client.token, "workspace-a", 200}} {
		res := request(test.token, test.workspace)
		_ = res.Body.Close()
		if res.StatusCode != test.status {
			t.Fatalf("status %d, want %d", res.StatusCode, test.status)
		}
	}
	receivedMu.Lock()
	headers := received.Clone()
	receivedMu.Unlock()
	if headers.Get("Authorization") != "" || headers.Get("X-Multica-User-ID") != "account-a" || headers.Get("X-Multica-API-Origin") != "https://api.example.test" || headers.Get("X-Workspace-ID") != "workspace-a" {
		t.Fatalf("incorrect scope headers: %v", headers)
	}
	headers = http.Header{"Authorization": {"Bearer " + client.token}, "X-Workspace-ID": {"workspace-a"}}
	ws, _, err := websocket.DefaultDialer.Dial("ws"+base[4:]+"/ws", headers)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if err := ws.WriteMessage(websocket.TextMessage, []byte(`{"version":2,"type":"subscribe_scope"}`)); err != nil {
		t.Fatal(err)
	}
	_, data, err := ws.ReadMessage()
	if err != nil || string(data) != `{"version":2,"type":"subscribe_scope"}` {
		t.Fatalf("WebSocket tunnel: %q %v", data, err)
	}
	client.Close()
	_ = ws.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := ws.ReadMessage(); err == nil {
		t.Fatal("socket survived client close")
	}
}

// fakeRegistry answers with desktopKey's device plus another phone. POST
// /v1/devices and login checks are controllable per test.
func fakeRegistry(t *testing.T, register http.HandlerFunc, validLogin func(string) bool) (*httptest.Server, peer.ID) {
	t.Helper()
	publicKey := func() (string, peer.ID) {
		_, id := generateKey(t)
		pub, err := id.ExtractPublicKey()
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := crypto.MarshalPublicKey(pub)
		return base64.StdEncoding.EncodeToString(raw), id
	}
	desktopKey, desktopID := publicKey()
	phoneKey, _ := publicKey()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/sessions":
			if !validLogin(r.Header.Get("Authorization")) {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"session_token": "session", "expires_at": time.Now().Add(time.Hour)})
		case r.URL.Path == "/v1/devices" && r.Method == http.MethodPost:
			register(w, r)
		case r.URL.Path == "/v1/devices":
			_ = json.NewEncoder(w).Encode(map[string]any{"devices": []map[string]any{
				{"public_key": desktopKey, "device_name": "Test Mac", "daemon_id": "desktop", "profile": "default", "addrs": []string{"/ip4/127.0.0.1/tcp/1"}},
				{"public_key": phoneKey, "device_name": "Other iPhone", "daemon_id": "ios-other", "profile": "mobile", "addrs": []string{"/ip4/127.0.0.1/tcp/2"}},
			}})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"relay": nil})
		}
	}))
	t.Cleanup(server.Close)
	return server, desktopID
}

func startTestClient(t *testing.T, registryURL string) *Client {
	t.Helper()
	config, _ := json.Marshal(configuration{RegistryURL: registryURL, APIURL: "https://api.example.test", Token: "login", UserID: "account-a", StoragePath: t.TempDir(), DeviceName: "Test iPhone"})
	client, err := Start(string(config))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return client
}

func listDevices(t *testing.T, client *Client) (int, []string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, client.endpoint+"/devices", nil)
	req.Header.Set("Authorization", "Bearer "+client.token)
	res, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body struct {
		Devices []struct {
			Name string `json:"device_name"`
		} `json:"devices"`
	}
	_ = json.NewDecoder(res.Body).Decode(&body)
	names := []string{}
	for _, device := range body.Devices {
		names = append(names, device.Name)
	}
	return res.StatusCode, names
}

func TestRegistrationFailureDoesNotBlockKnownDevices(t *testing.T) {
	stuck := make(chan struct{})
	defer close(stuck)
	registry, desktopID := fakeRegistry(t, func(http.ResponseWriter, *http.Request) { <-stuck }, func(string) bool { return true })
	started := make(chan *Client, 1)
	go func() { started <- startTestClient(t, registry.URL) }()
	var client *Client
	select {
	case client = <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("Start waited for registration")
	}
	status, names := listDevices(t, client)
	if status != http.StatusOK || len(names) != 1 || names[0] != "Test Mac" {
		t.Fatalf("devices = %d %v, want only the computer", status, names)
	}
	if _, known := client.svc.Peer(desktopID); !known {
		t.Fatal("computer is not on the allowlist")
	}
}

func TestOtherPhonesAreNotDialed(t *testing.T) {
	registry, _ := fakeRegistry(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("{}")) }, func(string) bool { return true })
	client := startTestClient(t, registry.URL)
	if status, _ := listDevices(t, client); status != http.StatusOK {
		t.Fatalf("status %d", status)
	}
	for _, p := range client.svc.Peers() {
		if p.Profile == "mobile" {
			t.Fatalf("another phone is on the dial list: %+v", p)
		}
	}
}

func TestTokenUpdateRestoresRegistryAccessWithoutRestart(t *testing.T) {
	var loginMu sync.Mutex
	current := "Bearer refreshed"
	registry, _ := fakeRegistry(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("{}")) }, func(got string) bool {
		loginMu.Lock()
		defer loginMu.Unlock()
		return got == current
	})
	client := startTestClient(t, registry.URL)
	if status, _ := listDevices(t, client); status != http.StatusServiceUnavailable {
		t.Fatalf("status %d with a rejected login, want 503", status)
	}
	endpoint := client.Endpoint()
	client.UpdateToken("refreshed")
	if err := client.svc.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if status, names := listDevices(t, client); status != http.StatusOK || len(names) != 1 {
		t.Fatalf("devices after refresh = %d %v", status, names)
	}
	if client.Endpoint() != endpoint {
		t.Fatal("token update replaced the loopback endpoint")
	}
}

func TestNetworkChangedIsSafeAfterClose(t *testing.T) {
	registry, _ := fakeRegistry(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("{}")) }, func(string) bool { return true })
	client := startTestClient(t, registry.URL)
	client.NetworkChanged()
	client.Close()
	client.NetworkChanged()
}

func diagnostics(t *testing.T, client *Client, query string) (records []timing.Record, next, dropped int64) {
	t.Helper()
	var endpoint struct{ URL, Token string }
	if err := json.Unmarshal([]byte(client.Endpoint()), &endpoint); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("GET", endpoint.URL+"/diagnostics"+query, nil)
	req.Header.Set("Authorization", "Bearer "+endpoint.Token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body struct {
		Records []timing.Record `json:"records"`
		Next    int64           `json:"next"`
		Dropped int64           `json:"dropped"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body.Records, body.Next, body.Dropped
}

func TestDiagnosticsNeedTheLoopbackCredentialAndDrainWithACursor(t *testing.T) {
	registry, _ := fakeRegistry(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "{}") }, func(string) bool { return true })
	client := startTestClient(t, registry.URL)
	var endpoint struct{ URL string }
	_ = json.Unmarshal([]byte(client.Endpoint()), &endpoint)
	res, err := http.Get(endpoint.URL + "/diagnostics")
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("without the credential = %d", res.StatusCode)
	}
	records, next, _ := diagnostics(t, client, "")
	if len(records) == 0 || records[0].Stage != timing.StageLinkStart || records[0].Source != timing.SourceMobile {
		t.Fatalf("records = %+v", records)
	}
	after, cursor, _ := diagnostics(t, client, "?after="+strconv.FormatInt(next, 10))
	for _, r := range after {
		if r.Stage == timing.StageLinkStart {
			t.Fatalf("drained twice: %+v", r)
		}
	}
	if cursor < next {
		t.Fatalf("cursor went back: %d after %d", cursor, next)
	}
}

func TestDiagnosticsSurviveAStoppedLinkAndSayWhyItStopped(t *testing.T) {
	registry, _ := fakeRegistry(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "{}") }, func(string) bool { return true })
	storage := t.TempDir()
	start := func() *Client {
		config, _ := json.Marshal(map[string]string{"registryUrl": registry.URL, "apiUrl": "https://api.multica.test", "token": "login", "userId": "user-1", "storagePath": storage, "deviceName": "Phone"})
		client, err := Start(string(config))
		if err != nil {
			t.Fatal(err)
		}
		return client
	}
	first := start()
	first.CloseInBackground()
	second := start()
	t.Cleanup(second.Close)
	records, _, _ := diagnostics(t, second, "")
	stopped := false
	for _, r := range records {
		if r.Stage == timing.StageLinkStop && r.Reason == timing.ReasonBackground {
			stopped = true
		}
	}
	if !stopped {
		t.Fatalf("no background stop in %+v", records)
	}
}

func TestAcknowledgedDiagnosticsAreNotReadAgainAfterAColdStart(t *testing.T) {
	registry, _ := fakeRegistry(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "{}") }, func(string) bool { return true })
	storage := t.TempDir()
	start := func() *Client {
		config, _ := json.Marshal(map[string]string{"registryUrl": registry.URL, "apiUrl": "https://api.multica.test", "token": "login", "userId": "user-1", "storagePath": storage, "deviceName": "Phone"})
		client, err := Start(string(config))
		if err != nil {
			t.Fatal(err)
		}
		return client
	}
	post := func(client *Client, body string, token string) int {
		t.Helper()
		req, _ := http.NewRequest("POST", client.endpoint+"/diagnostics/ack", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		return res.StatusCode
	}
	first := start()
	_, next, _ := diagnostics(t, first, "")
	if got := post(first, `{"next":1}`, "wrong"); got != http.StatusForbidden {
		t.Fatalf("without the credential = %d", got)
	}
	if got := post(first, `{"next":-1}`, first.token); got != http.StatusBadRequest {
		t.Fatalf("a negative position = %d", got)
	}
	if got := post(first, fmt.Sprintf(`{"next":%d}`, next), first.token); got != http.StatusNoContent {
		t.Fatalf("acknowledge = %d", got)
	}
	first.Close()
	second := start()
	t.Cleanup(second.Close)
	records, _, _ := diagnostics(t, second, "?after=0")
	starts := 0
	for _, r := range records {
		if r.Stage == timing.StageLinkStart {
			starts++
		}
	}
	if starts != 1 {
		t.Fatalf("read %d link starts after a cold start: %+v", starts, records)
	}
}

func TestPhoneRequestsCarryTheirClientKind(t *testing.T) {
	f := startMigrationFixture(t)
	res := f.get(t, "/kind")
	defer res.Body.Close()
	if got := res.Header.Get("X-Seen-Client-Kind"); got != timing.ClientMobile {
		t.Fatalf("client kind = %q", got)
	}
}
