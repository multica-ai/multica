package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/multica-ai/multica/server/pkg/agent"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The server activates successfully but the first HTTP reply is lost at a proxy.
// Idempotent activation may be retried; the instruction itself must be sent once.
func TestHermesGuidanceActivationResponseLost(t *testing.T) {
	var active atomic.Bool
	var attempts, delivered atomic.Int32
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	d := taskSupplementTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/ack") {
			cancel()
			w.Write([]byte(`{}`))
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["active_turn_guidance"] != nil {
			active.Store(true)
			if attempts.Add(1) == 1 {
				http.Error(w, "proxy lost activation response", 502)
				return
			}
			w.Write([]byte(`{"supplement_capability":"task-supplement-v1"}`))
			return
		}
		w.Write([]byte(`{"comment_id":"m","content":"literal","author_type":"member"}`))
	})
	session := &agent.Session{
		SupplementRequiresNegotiation: true,
		SupplementReady:               func() bool { return true },
		Supplement:                    func(context.Context, string) error { delivered.Add(1); return nil },
	}
	d.runTaskSupplementLoop(ctx, session, "task", nil, d.logger)
	if active.Load() && delivered.Load() != 1 {
		t.Fatalf("server capability active, provider still ready, but loop exited: activations=%d deliveries=%d ctx=%v", attempts.Load(), delivered.Load(), ctx.Err())
	}
}

func TestHermesGuidanceActivationDefinitiveRefusal(t *testing.T) {
	for _, status := range []int{200, 400, 401, 403, 404, 409, 412} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var attempts atomic.Int32
			d := taskSupplementTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {
				attempts.Add(1)
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{}`))
			})
			session := &agent.Session{SupplementRequiresNegotiation: true, SupplementReady: func() bool { return true }, Supplement: func(context.Context, string) error { t.Fatal("rejected negotiation claimed input"); return nil }}
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			d.runTaskSupplementLoop(ctx, session, "task", nil, d.logger)
			if attempts.Load() != 1 || ctx.Err() != nil {
				t.Fatalf("refusal retried: attempts=%d ctx=%v", attempts.Load(), ctx.Err())
			}
		})
	}
}

func TestHermesGuidanceActivationRechecksReady(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var attempts atomic.Int32
	var ready atomic.Bool
	ready.Store(true)
	d := taskSupplementTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		ready.Store(false)
		http.Error(w, "lost reply", 502)
	})
	session := &agent.Session{SupplementRequiresNegotiation: true, SupplementReady: func() bool {
		if !ready.Load() {
			cancel()
		}
		return ready.Load()
	}, Supplement: func(context.Context, string) error { t.Fatal("unready provider received input"); return nil }}
	d.runTaskSupplementLoop(ctx, session, "task", nil, d.logger)
	if attempts.Load() != 1 {
		t.Fatalf("activation repeated after turn ended: %d", attempts.Load())
	}
}
