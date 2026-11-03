package p2p

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/protocol"
)

// A computer reads from a phone over the connection the two already have, which
// may only be the relay and may have been dialed by either of them.
func TestRelayedConnectionCarriesAStreamOpenedByEitherSide(t *testing.T) {
	const proto protocol.ID = "/multica/reverse-test/1"
	a, b, _ := circuitOnlyPair(t)
	for _, pair := range []struct {
		name             string
		listener, caller *Service
	}{{"b opens to a", a, b}, {"a opens to b", b, a}} {
		t.Run(pair.name, func(t *testing.T) {
			listener, stop := pair.listener.Listen(proto)
			defer stop()
			server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, "answered")
			})}
			go func() { _ = server.Serve(listener) }()
			defer server.Close()
			tunnel := pair.caller.NewTunnel(pair.listener.ID(), proto, &http.Transport{DisableKeepAlives: true})
			defer tunnel.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://device/logs", nil)
			res, err := (&http.Client{Transport: tunnel}).Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			body, _ := io.ReadAll(res.Body)
			if string(body) != "answered" {
				t.Fatalf("body = %q", body)
			}
			if got := pair.caller.Connection(pair.listener.ID()); got != ConnectionRelayed {
				t.Fatalf("connection = %q, want it to stay on the relay", got)
			}
		})
	}
}
