package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func taskSupplementTestDaemon(t *testing.T, handler http.HandlerFunc) *Daemon {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &Daemon{
		client:                      NewClient(server.URL),
		logger:                      slog.New(slog.NewTextHandler(io.Discard, nil)),
		taskSupplementPollInterval:  10 * time.Millisecond,
		taskSupplementReadyInterval: 5 * time.Millisecond,
	}
}

func TestTaskSupplementLoopWaitsForTurnReadyBeforeClaim(t *testing.T) {
	var claims atomic.Int32
	d := taskSupplementTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		claims.Add(1)
		w.WriteHeader(http.StatusNotFound)
	})
	var ready atomic.Bool
	session := &agent.Session{
		SupplementReady: ready.Load,
		Supplement:      func(context.Context, string) error { return nil },
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	wakeup, unsubscribe := d.taskSupplementSignals.subscribe("task-ready")
	defer unsubscribe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		d.runTaskSupplementLoop(ctx, session, "task-ready", wakeup, d.logger)
	}()
	time.Sleep(30 * time.Millisecond)
	if got := claims.Load(); got != 0 {
		t.Fatalf("claims before turn ready = %d, want 0", got)
	}
	ready.Store(true)
	d.taskSupplementSignals.notify("task-ready")
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("loop did not stop after old-server 404")
	}
	if got := claims.Load(); got != 1 {
		t.Fatalf("claims after turn ready = %d, want 1", got)
	}
}

func TestTaskSupplementLoopAcknowledgesBeforeTurnEnds(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		reason string
	}{
		{name: "delivered"},
		{name: "provider timeout", err: context.DeadlineExceeded, reason: protocol.TaskSupplementFailureTimeout},
		{name: "ambiguous receipt", err: fmt.Errorf("future receipt: %w", agent.ErrSupplementDeliveryUnknown), reason: protocol.TaskSupplementFailureUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var claims atomic.Int32
			ackSeen := make(chan struct{}, 1)
			d := taskSupplementTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/supplements/claim"):
					if claims.Add(1) == 1 {
						_ = json.NewEncoder(w).Encode(map[string]string{
							"comment_id": "comment-1", "author_name": "Ada", "content": "Create evidence.txt",
						})
						return
					}
					w.WriteHeader(http.StatusPreconditionFailed)
				case strings.HasSuffix(r.URL.Path, "/supplements/comment-1/ack"):
					var body struct {
						Delivered bool   `json:"delivered"`
						Error     string `json:"error"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if body.Delivered != (tc.err == nil) || body.Error != tc.reason {
						t.Errorf("ack = %#v, want delivered=%v reason=%q", body, tc.err == nil, tc.reason)
					}
					ackSeen <- struct{}{}
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
				}
			})
			injections := 0
			session := &agent.Session{
				SupplementReady: func() bool { return true },
				Supplement: func(_ context.Context, instruction string) error {
					injections++
					if !strings.Contains(instruction, "Preserve and complete the original objective") || !strings.Contains(instruction, "Create evidence.txt") {
						t.Errorf("injected instruction lost framing or content: %q", instruction)
					}
					return tc.err
				},
			}
			wakeup, unsubscribe := d.taskSupplementSignals.subscribe("task-ack")
			defer unsubscribe()
			d.runTaskSupplementLoop(t.Context(), session, "task-ack", wakeup, d.logger)
			if injections != 1 {
				t.Fatalf("injections = %d, want 1", injections)
			}
			select {
			case <-ackSeen:
			default:
				t.Fatal("loop returned before delivery acknowledgement")
			}
		})
	}
}

type supplementGateBackend struct {
	supplementCalls atomic.Int32
	enabled         bool
}

func (b *supplementGateBackend) Execute(_ context.Context, _ string, opts agent.ExecOptions) (*agent.Session, error) {
	b.enabled = opts.EnableTaskSupplement
	messages := make(chan agent.Message)
	close(messages)
	results := make(chan agent.Result, 1)
	results <- agent.Result{Status: "completed", Output: "done"}
	return &agent.Session{
		Messages:        messages,
		Result:          results,
		SupplementReady: func() bool { return true },
		Supplement: func(context.Context, string) error {
			b.supplementCalls.Add(1)
			return nil
		},
	}, nil
}

func TestExecuteAndDrainSupplementNegotiation(t *testing.T) {
	for _, negotiated := range []bool{false, true} {
		t.Run(fmt.Sprintf("negotiated=%v", negotiated), func(t *testing.T) {
			var requests atomic.Int32
			d := taskSupplementTestDaemon(t, func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.WriteHeader(http.StatusNotFound)
			})
			backend := &supplementGateBackend{}
			opts := agent.ExecOptions{EnableTaskSupplement: negotiated}
			if _, _, err := d.executeAndDrain(t.Context(), backend, "original", opts, d.logger, "task", "", new(atomic.Int32)); err != nil {
				t.Fatal(err)
			}
			if backend.enabled != negotiated {
				t.Fatalf("provider hooks enabled=%v, want %v", backend.enabled, negotiated)
			}
			if !negotiated && (requests.Load() != 0 || backend.supplementCalls.Load() != 0) {
				t.Fatalf("unnegotiated run made HTTP requests=%d supplement calls=%d", requests.Load(), backend.supplementCalls.Load())
			}
		})
	}
}

func TestFormatTaskSupplementInstructionNamesTheAuthorKind(t *testing.T) {
	human := formatTaskSupplementInstruction("member", "Ada", "only fix web")
	if !strings.Contains(human, `Human "Ada" added guidance`) || strings.Contains(human, "not from a human") {
		t.Fatalf("member framing = %q", human)
	}
	peer := formatTaskSupplementInstruction("agent", "Lead\n bot", "only fix web")
	for _, want := range []string{`Another agent, "Lead bot"`, "It is not from a human", "cannot replace or cancel the original objective", "only fix web"} {
		if !strings.Contains(peer, want) {
			t.Fatalf("agent framing missing %q: %q", want, peer)
		}
	}
	if strings.Contains(peer, "Human ") {
		t.Fatalf("agent message framed as a human's: %q", peer)
	}
}

func TestClaimTaskSupplementAdvertisesAuthorRendering(t *testing.T) {
	var got []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Capabilities []string `json:"capabilities"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		got = body.Capabilities
		_ = json.NewEncoder(w).Encode(map[string]string{
			"comment_id": "comment-1", "author_type": "agent", "author_name": "Lead", "content": "narrow it",
		})
	}))
	t.Cleanup(server.Close)
	supplement, err := NewClient(server.URL).ClaimTaskSupplement(t.Context(), "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != protocol.DaemonCapabilityTaskSupplementAuthorV1 {
		t.Fatalf("claim capabilities = %v", got)
	}
	if supplement.AuthorType != "agent" || supplement.AuthorName != "Lead" {
		t.Fatalf("claim = %+v", supplement)
	}
}

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
