package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResolveDrawioViewerURL(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		disabled bool
		want     string
		wantErr  bool
	}{
		{name: "unset uses the public viewer", raw: "", want: DefaultDrawioViewerURL},
		{name: "blank uses the public viewer", raw: "   ", want: DefaultDrawioViewerURL},
		{name: "self-hosted origin", raw: "https://drawio.internal.example", want: "https://drawio.internal.example"},
		{name: "path and trailing slash", raw: " https://tools.example.com/drawio/ ", want: "https://tools.example.com/drawio"},
		{name: "plain http on an intranet", raw: "http://10.0.0.5:8080", want: "http://10.0.0.5:8080"},
		{name: "disabled wins over a valid URL", raw: "https://drawio.internal.example", disabled: true, want: ""},
		{name: "disabled with nothing set", disabled: true, want: ""},
		{name: "relative URL", raw: "/drawio", wantErr: true},
		{name: "non-http scheme", raw: "javascript:alert(1)", wantErr: true},
		{name: "file scheme", raw: "file:///tmp/drawio", wantErr: true},
		{name: "credentials", raw: "https://user:pass@drawio.example", wantErr: true},
		{name: "query", raw: "https://drawio.example/?lightbox=1", wantErr: true},
		{name: "fragment", raw: "https://drawio.example/#R", wantErr: true},
		{name: "unparseable", raw: "https://drawio.example/%zz", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveDrawioViewerURL(tc.raw, tc.disabled)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got %q", got)
				}
				if got != "" {
					t.Fatalf("an invalid URL must turn the preview off, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGetConfigExposesDrawioViewerURL(t *testing.T) {
	origCfg := testHandler.cfg
	t.Cleanup(func() { testHandler.cfg = origCfg })

	fetch := func() map[string]json.RawMessage {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
		w := httptest.NewRecorder()
		testHandler.GetConfig(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GetConfig: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var cfg map[string]json.RawMessage
		if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
			t.Fatalf("decode config: %v", err)
		}
		return cfg
	}

	testHandler.cfg.DrawioViewerURL = ""
	if _, ok := fetch()["drawio_viewer_url"]; ok {
		t.Fatal("drawio_viewer_url must be omitted when the preview is off")
	}

	testHandler.cfg.DrawioViewerURL = "https://drawio.internal.example"
	raw, ok := fetch()["drawio_viewer_url"]
	if !ok {
		t.Fatal("drawio_viewer_url must be present when the preview is on")
	}
	if string(raw) != `"https://drawio.internal.example"` {
		t.Fatalf("drawio_viewer_url: got %s", raw)
	}
}
