package wecom

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The vendored reference doc shows generate / query_result fields at the top
// level of the response body, but live WeCom traffic wraps both in `data`.
// These tests pin the wrapped shape, which the doc-derived implementation
// silently failed to decode: Generate returned "missing scode / auth_url" for
// every real install attempt.

func newTestProvider(t *testing.T, handler http.HandlerFunc) (*HTTPProvider, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	p, err := NewHTTPProvider(HTTPProviderConfig{
		BaseURL:    srv.URL,
		SourceID:   "multica",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewHTTPProvider: %v", err)
	}
	return p, srv
}

func TestHTTPProviderGenerateReadsDataEnvelope(t *testing.T) {
	var gotPath, gotSource string
	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotSource = r.URL.Query().Get("source")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"scode":"ie2dSh0INb6YK9YQ",` +
			`"auth_url":"https://work.weixin.qq.com/ai/qc/c?s=ie2dSh0INb6YK9YQ"}}`))
	})

	got, err := p.Generate(context.Background())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if got.Scode != "ie2dSh0INb6YK9YQ" {
		t.Errorf("Scode = %q", got.Scode)
	}
	if got.AuthURL != "https://work.weixin.qq.com/ai/qc/c?s=ie2dSh0INb6YK9YQ" {
		t.Errorf("AuthURL = %q", got.AuthURL)
	}
	if gotPath != "/ai/qc/generate" {
		t.Errorf("path = %q", gotPath)
	}
	if gotSource != "multica" {
		t.Errorf("source = %q", gotSource)
	}
}

func TestHTTPProviderGenerateRejectsEmptyEnvelope(t *testing.T) {
	p, _ := newTestProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{}}`))
	})

	if _, err := p.Generate(context.Background()); err == nil {
		t.Fatal("expected an error when data carries no scode / auth_url")
	}
}

func TestHTTPProviderQueryResultReadsDataEnvelope(t *testing.T) {
	var gotScode, gotSource string
	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		gotScode = r.URL.Query().Get("scode")
		gotSource = r.URL.Query().Get("source")
		_, _ = w.Write([]byte(`{"data":{"status":"success",` +
			`"bot_info":{"botid":"bot-1","secret":"sec-1"}}}`))
	})

	got, err := p.QueryResult(context.Background(), "sc-1")
	if err != nil {
		t.Fatalf("QueryResult: %v", err)
	}
	if got.Status != QueryStatusSuccess {
		t.Errorf("Status = %q", got.Status)
	}
	if got.BotInfo == nil || got.BotInfo.BotID != "bot-1" || got.BotInfo.Secret != "sec-1" {
		t.Errorf("BotInfo = %+v", got.BotInfo)
	}
	if gotScode != "sc-1" || gotSource != "multica" {
		t.Errorf("scode = %q, source = %q", gotScode, gotSource)
	}
}

func TestHTTPProviderQueryResultPendingInDataEnvelope(t *testing.T) {
	p, _ := newTestProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"status":"pending"}}`))
	})

	got, err := p.QueryResult(context.Background(), "sc-1")
	if err != nil {
		t.Fatalf("QueryResult: %v", err)
	}
	if got.Status != QueryStatusPending {
		t.Errorf("Status = %q", got.Status)
	}
	if got.BotInfo != nil {
		t.Errorf("BotInfo = %+v, want nil", got.BotInfo)
	}
}

// A body without the envelope is named as such, so the next wire change is
// diagnosable instead of arriving as "missing scode / auth_url".
func TestHTTPProviderRejectsBodyWithoutDataObject(t *testing.T) {
	p, _ := newTestProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"scode":"sc-1","auth_url":"https://example.test/qr"}`))
	})

	_, err := p.Generate(context.Background())
	if err == nil {
		t.Fatal("expected an error when the body carries no data object")
	}
	if !strings.Contains(err.Error(), "no data object") {
		t.Errorf("error = %q, want it to name the missing data object", err)
	}
}

func TestHTTPProviderQueryResultSuccessWithoutBotInfo(t *testing.T) {
	p, _ := newTestProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"status":"success"}}`))
	})

	if _, err := p.QueryResult(context.Background(), "sc-1"); err == nil {
		t.Fatal("expected an error when success carries no bot_info")
	}
}
