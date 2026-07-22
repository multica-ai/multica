package dingtalk

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenConnection_ReturnsDialURL(t *testing.T) {
	var gotBody openConnectionRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != connectionsOpenPath {
			t.Errorf("path = %q, want %q", r.URL.Path, connectionsOpenPath)
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"endpoint":"wss://stream.example/ws","ticket":"tkt-123"}`))
	}))
	defer srv.Close()

	got, err := openConnection(context.Background(), srv.Client(), srv.URL, "key-1", "secret-1")
	if err != nil {
		t.Fatalf("openConnection: %v", err)
	}
	if got != "wss://stream.example/ws?ticket=tkt-123" {
		t.Errorf("dial url = %q", got)
	}
	if gotBody.ClientID != "key-1" || gotBody.ClientSecret != "secret-1" {
		t.Errorf("credentials not sent: %+v", gotBody)
	}
	if len(gotBody.Subscriptions) != 3 {
		t.Fatalf("want 3 subscriptions, got %d", len(gotBody.Subscriptions))
	}
	if gotBody.Subscriptions[2].Type != frameTypeCallback || gotBody.Subscriptions[2].Topic != botMessageTopic {
		t.Errorf("callback subscription wrong: %+v", gotBody.Subscriptions[2])
	}
}

func TestOpenConnection_ErrorsOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":"unauthorized"}`))
	}))
	defer srv.Close()
	_, err := openConnection(context.Background(), srv.Client(), srv.URL, "k", "s")
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("want 401 error, got %v", err)
	}
}

func TestOpenConnection_ErrorsOnEmptyEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"endpoint":"","ticket":""}`))
	}))
	defer srv.Close()
	_, err := openConnection(context.Background(), srv.Client(), srv.URL, "k", "s")
	if err == nil {
		t.Fatal("want error on empty endpoint/ticket")
	}
}
