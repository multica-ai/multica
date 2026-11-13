package wecom

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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

// The scode is a bearer credential: per install.go, whoever holds it can finish
// creating the bot. It travels in the query string, and *url.Error.Error()
// prints the URL it failed on — query string included. install_worker.go logs
// these at Warn from inside the poll loop, so one sustained upstream hiccup
// wrote a live, redeemable scode to the log over and over. Everything else
// about this credential is careful: it is sealed at rest, NULLed on both
// terminal paths, and never returned by any endpoint.
func TestHTTPProviderTransportErrorsNeverCarryTheScode(t *testing.T) {
	const scode = "SUPER-SECRET-SCODE-abc123"
	// Accept the connection and hang up without answering: WeCom hiccuping, an
	// LB dropping the connection, a DNS blip. net/http surfaces this as
	// *url.Error{Op: "Get", URL: <full url>, Err: EOF}.
	hangup := func(w http.ResponseWriter, _ *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		_ = conn.Close()
	}

	t.Run("query_result", func(t *testing.T) {
		p, _ := newTestProvider(t, hangup)
		_, err := p.QueryResult(context.Background(), scode)
		if err == nil {
			t.Fatal("expected a transport error")
		}
		if strings.Contains(err.Error(), scode) {
			t.Errorf("error leaks the scode: %q", err)
		}
		if strings.Contains(err.Error(), "?") {
			t.Errorf("error carries a query string, so the next credential put there leaks too: %q", err)
		}
		// Still has to be diagnosable: which call, against which endpoint, why.
		if !strings.Contains(err.Error(), "/ai/qc/query_result") {
			t.Errorf("error = %q, want it to name the endpoint that failed", err)
		}
	})

	t.Run("generate", func(t *testing.T) {
		p, _ := newTestProvider(t, hangup)
		_, err := p.Generate(context.Background())
		if err == nil {
			t.Fatal("expected a transport error")
		}
		if strings.Contains(err.Error(), "?") {
			t.Errorf("error carries a query string: %q", err)
		}
		if !strings.Contains(err.Error(), "/ai/qc/generate") {
			t.Errorf("error = %q, want it to name the endpoint that failed", err)
		}
	})

	// A timeout is the likelier trigger than a hangup — a wedged upstream, which
	// is also the case that repeats every poll interval. net/http reports it as
	// *url.Error too, so it must be scrubbed by the same path.
	t.Run("upstream timeout", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			<-r.Context().Done() // hold until the client gives up
		}))
		t.Cleanup(srv.Close)
		p, err := NewHTTPProvider(HTTPProviderConfig{
			BaseURL:    srv.URL,
			SourceID:   "multica",
			HTTPClient: srv.Client(),
			Timeout:    50 * time.Millisecond,
		})
		if err != nil {
			t.Fatalf("NewHTTPProvider: %v", err)
		}

		_, err = p.QueryResult(context.Background(), scode)
		if err == nil {
			t.Fatal("expected a timeout error")
		}
		if strings.Contains(err.Error(), scode) {
			t.Errorf("timeout error leaks the scode: %q", err)
		}
	})
}
