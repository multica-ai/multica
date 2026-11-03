package mobilelink

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"

	"github.com/multica-ai/multica/server/internal/daemon/mobilediag"
	"github.com/multica-ai/multica/server/internal/daemon/p2p"
)

type logTestRig struct {
	computer *p2p.Service
	client   *Client
	logs     *Logs

	mu      sync.Mutex
	devices []map[string]any
	deleted []string
}

// startPhoneAndComputer connects a phone link to a computer the way the app starts
// one: at login, with no conversation open and no WebSocket to the computer.
func startPhoneAndComputer(t *testing.T) *logTestRig {
	t.Helper()
	rig := &logTestRig{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rig.mu.Lock()
		defer rig.mu.Unlock()
		switch {
		case r.URL.Path == "/v1/sessions":
			_ = json.NewEncoder(w).Encode(map[string]any{"session_token": "session", "expires_at": time.Now().Add(time.Hour)})
		case r.URL.Path == "/v1/devices" && r.Method == http.MethodPost:
			var device map[string]any
			_ = json.NewDecoder(r.Body).Decode(&device)
			rig.devices = append(rig.devices, device)
			_ = json.NewEncoder(w).Encode(device)
		case r.URL.Path == "/v1/devices" && r.Method == http.MethodDelete:
			rig.deleted = append(rig.deleted, r.URL.Query().Get("daemon_id"))
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/v1/devices":
			_ = json.NewEncoder(w).Encode(map[string]any{"devices": rig.devices})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"relay": nil})
		}
	}))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	key, _, _ := crypto.GenerateEd25519Key(rand.Reader)
	reg := &registry{url: server.URL, login: "login", http: server.Client()}
	computer, err := p2p.Start(ctx, p2p.Config{Key: key, Fetch: reg.peers, ListenAddrs: []string{"/ip4/127.0.0.1/tcp/0"}, RefreshInterval: 200 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = computer.Close() })
	pub, _ := crypto.MarshalPublicKey(key.GetPublic())
	if err := reg.call(ctx, "POST", "/v1/devices", map[string]any{"daemon_id": "computer", "profile": "default", "public_key": base64.StdEncoding.EncodeToString(pub), "device_name": "Test Mac", "addrs": computer.AdvertisedAddrs()}, nil); err != nil {
		t.Fatal(err)
	}
	storage := t.TempDir()
	logs, err := OpenLogs(storage)
	if err != nil {
		t.Fatal(err)
	}
	config, _ := json.Marshal(configuration{RegistryURL: server.URL, APIURL: "https://api.example.test", Token: "login", UserID: "account-a", StoragePath: storage, DeviceName: "Multica iOS"})
	client, err := Start(string(config))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	rig.computer, rig.client, rig.logs = computer, client, logs
	return rig
}

func waitForPhone(t *testing.T, computer *p2p.Service) mobilediag.Phone {
	t.Helper()
	var found mobilediag.Phone
	eventually(t, 20*time.Second, "the computer to reach the phone", func() bool {
		for _, phone := range mobilediag.Phones(computer) {
			if phone.Connected {
				found = phone
				return true
			}
		}
		return false
	})
	return found
}

func TestComputerReadsThePhonesLogsWithNoConversationOpen(t *testing.T) {
	rig := startPhoneAndComputer(t)
	rig.logs.AppendLines(mobilediag.SourceApp, `{"at":"`+time.Now().UTC().Format(time.RFC3339Nano)+`","source":"app","level":"warning","message":"[ws] socket closed"}`)
	phone := waitForPhone(t, rig.computer)
	if want := "ios-" + rig.client.svc.ID().String(); phone.ID != want {
		t.Fatalf("phone id = %q, want %q", phone.ID, want)
	}
	var out bytes.Buffer
	fetched, err := mobilediag.Fetch(context.Background(), rig.computer, phone, 60, &out)
	if err != nil {
		t.Fatal(err)
	}
	if !fetched.Complete || fetched.Summary.Sources[mobilediag.SourceApp].Records != 1 || !strings.Contains(out.String(), "socket closed") {
		t.Fatalf("fetched %+v: %s", fetched, out.String())
	}
	// The link's own marks are what the app could not report while it had no connection.
	if fetched.Summary.Sources[mobilediag.SourceMarks].Records == 0 {
		t.Fatalf("no link marks arrived: %+v", fetched.Summary)
	}
}

func TestPhoneRefusesReadsWhenItsUserTurnedThemOff(t *testing.T) {
	rig := startPhoneAndComputer(t)
	phone := waitForPhone(t, rig.computer)
	rig.logs.SetRemoteReadAllowed(false)
	var out bytes.Buffer
	if _, err := mobilediag.Fetch(context.Background(), rig.computer, phone, 60, &out); !errors.Is(err, mobilediag.ErrRemoteReadDisabled) {
		t.Fatalf("err = %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("a refused read wrote %q", out.String())
	}
	rig.logs.SetRemoteReadAllowed(true)
	if fetched, err := mobilediag.Fetch(context.Background(), rig.computer, phone, 60, &out); err != nil || !fetched.Complete {
		t.Fatalf("fetched %+v, %v", fetched, err)
	}
}

func TestSignOutTakesThePhoneOffTheDeviceList(t *testing.T) {
	rig := startPhoneAndComputer(t)
	eventually(t, 10*time.Second, "registration", func() bool {
		rig.mu.Lock()
		defer rig.mu.Unlock()
		return len(rig.devices) > 1
	})
	rig.client.Unregister()
	rig.mu.Lock()
	defer rig.mu.Unlock()
	if want := "ios-" + rig.client.svc.ID().String(); len(rig.deleted) != 1 || rig.deleted[0] != want {
		t.Fatalf("deleted = %v, want %s", rig.deleted, want)
	}
}
