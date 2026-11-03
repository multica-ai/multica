package localhost

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/multica-ai/multica/server/internal/localconversation/timing"
)

func rendererHeaders(origin string) http.Header {
	h := v2Headers("user-1")
	h.Set("Authorization", "Bearer "+RendererToken(v2Token))
	if origin != "" {
		h.Set("Origin", origin)
	}
	return h
}

func TestV2EntryAcceptsEachCredentialFromItsOwnOrigins(t *testing.T) {
	t.Parallel()

	srv, _ := newV2Server(t)
	cases := []struct {
		name   string
		header http.Header
		want   int
	}{
		{"main without Origin", v2Headers("user-1"), http.StatusOK},
		{"main from a page", func() http.Header { h := v2Headers("user-1"); h.Set("Origin", "file://"); return h }(), http.StatusForbidden},
		{"renderer fetch without Origin", rendererHeaders(""), http.StatusOK},
		{"renderer from the packaged page", rendererHeaders("file://"), http.StatusOK},
		{"renderer from the dev server", rendererHeaders("http://localhost:5173"), http.StatusOK},
		{"renderer token from another site", rendererHeaders("https://example.com"), http.StatusForbidden},
		{"renderer token on a loopback path", rendererHeaders("http://localhost:5173/app"), http.StatusForbidden},
	}
	for _, c := range cases {
		if status, _ := v2Do(t, srv, http.MethodGet, "/local/v2/sessions", nil, c.header); status != c.want {
			t.Errorf("%s = %d, want %d", c.name, status, c.want)
		}
	}
}

func TestV2EntryRejectsNonLoopbackHosts(t *testing.T) {
	t.Parallel()

	srv, _ := newV2Server(t)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/local/v2/sessions", nil)
	req.Header = rendererHeaders("")
	req.Host = "attacker.example:80"
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("rebound host = %d", res.StatusCode)
	}
}

func TestV2PolicyKeepsMainOnlyRoutesFromTheRenderer(t *testing.T) {
	t.Parallel()

	srv, _ := newV2Server(t)
	renderer := rendererHeaders("file://")
	if status, _ := v2Do(t, srv, http.MethodPost, "/local/v2/sessions", map[string]any{"id": "conv-1", "provider": "claude"}, renderer); status != http.StatusCreated {
		t.Fatalf("renderer create = %d", status)
	}
	denied := []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/local/v2/drain", nil},
		{http.MethodPost, "/local/v2/undrain", nil},
		{http.MethodPost, "/local/v2/stop-providers", nil},
		{http.MethodPost, "/local/v2/folder-projects", map[string]any{"path": "/"}},
		{http.MethodPost, "/local/v2/preview/roots", map[string]any{"path": "/"}},
		{http.MethodPost, "/local/v2/sessions/conv-1/commands", map[string]any{"type": "set_directory", "cwd": "/"}},
	}
	for _, d := range denied {
		status, body := v2Do(t, srv, d.method, d.path, d.body, renderer)
		if status != http.StatusForbidden || body["error"].(map[string]any)["kind"] != "forbidden" {
			t.Errorf("renderer %s %s = %d %v", d.method, d.path, status, body)
		}
	}
	if status, _ := v2Do(t, srv, http.MethodPost, "/local/v2/folder-projects", map[string]any{"path": t.TempDir()}, v2Headers("user-1")); status != http.StatusOK && status != http.StatusCreated {
		t.Fatalf("main add folder = %d", status)
	}
}

func wsURL(srv string, query url.Values) string {
	return "ws" + strings.TrimPrefix(srv, "http") + "/local/v2/ws?" + query.Encode()
}

func TestV2WebSocketTicketCarriesScopeAndIsSingleUse(t *testing.T) {
	t.Parallel()

	srv, _ := newV2Server(t)
	status, body := v2Do(t, srv, http.MethodPost, "/local/v2/ws-ticket", nil, rendererHeaders(""))
	if status != http.StatusOK {
		t.Fatalf("ticket = %d %v", status, body)
	}
	ticket, _ := body["ticket"].(string)
	page := http.Header{"Origin": {"file://"}}
	conn, res, err := websocket.DefaultDialer.Dial(wsURL(srv.URL, url.Values{"ticket": {ticket}}), page)
	if err != nil {
		t.Fatalf("dial = %v %v", err, res)
	}
	defer conn.Close()
	if err := conn.WriteJSON(map[string]any{"version": V2Version, "type": "subscribe_scope", "after_scope_seq": 0}); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var msg map[string]any
	if err := conn.ReadJSON(&msg); err != nil || msg["status"] != "subscribed" {
		t.Fatalf("scope subscription over a ticket = %v %v", msg, err)
	}
	if _, res, err := websocket.DefaultDialer.Dial(wsURL(srv.URL, url.Values{"ticket": {ticket}}), page); err == nil || res.StatusCode != http.StatusForbidden {
		t.Fatalf("reused ticket = %v %v", err, res)
	}
	if _, res, err := websocket.DefaultDialer.Dial(wsURL(srv.URL, url.Values{"ticket": {"unknown"}}), page); err == nil || res.StatusCode != http.StatusForbidden {
		t.Fatalf("unknown ticket = %v %v", err, res)
	}
}

func TestV2WebSocketTicketExpires(t *testing.T) {
	t.Parallel()

	now := time.Now()
	ts := tickets{items: map[string]ticket{}, now: func() time.Time { return now }}
	id := ts.issue(callerRenderer, requestScope{}, "")
	now = now.Add(ticketTTL + time.Second)
	if _, ok := ts.take(id); ok {
		t.Fatal("expired ticket was accepted")
	}
}

func TestV2FileKeyStreamsOnlyFileRoutes(t *testing.T) {
	t.Parallel()

	srv, _ := newV2Server(t)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/local/v2/attachments", strings.NewReader("hello"))
	req.Header = rendererHeaders("")
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("X-File-Name", "a.txt")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var uploaded struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(res.Body).Decode(&uploaded)
	res.Body.Close()
	if res.StatusCode != http.StatusCreated || uploaded.ID == "" {
		t.Fatalf("upload = %d %+v", res.StatusCode, uploaded)
	}
	q := url.Values{"file_key": {FileKey(v2Token)}, "api_origin": {"https://api.multica.ai"}, "user_id": {"user-1"}, "workspace_id": {"ws-1"}}
	get := func(path string, q url.Values) int {
		res, err := http.Get(srv.URL + path + "?" + q.Encode())
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if s := get("/local/v2/sessions", q); s != http.StatusForbidden {
		t.Errorf("file key on a JSON route = %d", s)
	}
	wrong := url.Values{}
	for k, v := range q {
		wrong[k] = v
	}
	wrong.Set("file_key", RendererToken(v2Token))
	if s := get("/local/v2/attachments/missing", wrong); s != http.StatusForbidden {
		t.Errorf("wrong file key = %d", s)
	}
	if s := get("/local/v2/attachments/"+uploaded.ID, q); s != http.StatusOK {
		t.Errorf("file key on a file route = %d", s)
	}
	other := url.Values{}
	for k, v := range q {
		other[k] = v
	}
	other.Set("user_id", "someone-else")
	if s := get("/local/v2/attachments/missing", other); s != http.StatusForbidden {
		t.Errorf("file key for another account = %d", s)
	}
}

// Desktop's main process derives the same credentials; see
// apps/desktop/src/main/local-conversation-v2.test.ts.
func TestV2RendererCredentialsMatchDesktop(t *testing.T) {
	t.Parallel()

	if got := RendererToken("t1"); got != "a3bbe791d1c50e8cd0afe1d4872f001ce6d986f07404c22d9254a3f2c0de8331" {
		t.Errorf("renderer token = %s", got)
	}
	if got := FileKey("t1"); got != "64bde9d6264771573838ef0ad3a2d3a1f80f25f6a9fda3220160d0e854cdd843" {
		t.Errorf("file key = %s", got)
	}
}

func TestOnlyAnotherDeviceIsATurnsOrigin(t *testing.T) {
	remote := caller{kind: callerRemote, peer: "phone-peer", clientKind: timing.ClientMobile}
	if got := remote.origin(); got.PeerID != "phone-peer" || got.ClientKind != timing.ClientMobile {
		t.Fatalf("remote origin = %+v", got)
	}
	for _, kind := range []callerKind{callerMain, callerRenderer} {
		if got := (caller{kind: kind, peer: "ignored"}).origin(); got.PeerID != "" || got.ClientKind != "" {
			t.Fatalf("local caller %d has origin %+v", kind, got)
		}
	}
}
