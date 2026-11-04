package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/pkg/agent"
)

type supplementRepairBackend struct {
	repairingBackend
	ack <-chan struct{}
}

func (b *supplementRepairBackend) Execute(ctx context.Context, prompt string, opts agent.ExecOptions) (*agent.Session, error) {
	if _, err := b.repairingBackend.Execute(ctx, prompt, opts); err != nil {
		return nil, err
	}
	messages := make(chan agent.Message)
	results := make(chan agent.Result, 1)
	go func() {
		if opts.EnableTaskSupplement {
			select {
			case <-b.ack:
			case <-ctx.Done():
			}
		}
		close(messages)
		results <- agent.Result{Status: "completed", Output: "repair done; the requested additional answer is 42"}
	}()
	return &agent.Session{
		Messages: messages, Result: results,
		SupplementReady: func() bool { return true },
		Supplement:      func(context.Context, string) error { return nil },
	}, nil
}

// MUL-8139 review: the repair turn's reply is discarded, so it must not claim
// an additional message — that would ack it as delivered and lose the answer.
// Left unclaimed, the server settles it as turn_ended, which the receipt shows.
func TestRepairWorktreeDeliveryDoesNotConsumeAdditionalMessages(t *testing.T) {
	_, wt := squashedWorktree(t)
	ack := make(chan struct{})
	var delivered atomic.Bool
	var claims atomic.Int32
	d := taskSupplementTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/supplements/claim"):
			if claims.Add(1) == 1 {
				_ = json.NewEncoder(w).Encode(map[string]string{"comment_id": "review-comment", "author_name": "User", "content": "Also answer my additional question"})
			} else {
				w.WriteHeader(http.StatusPreconditionFailed)
			}
		case strings.HasSuffix(r.URL.Path, "/supplements/review-comment/ack"):
			var body struct {
				Delivered bool `json:"delivered"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			delivered.Store(body.Delivered)
			close(ack)
		default:
			w.WriteHeader(http.StatusOK)
		}
	})
	problem := wt.CheckDelivery()
	backend := &supplementRepairBackend{repairingBackend: repairingBackend{worktree: wt, problem: problem, comply: true}, ack: ack}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	result, _ := d.repairWorktreeDelivery(ctx, backend, agent.ExecOptions{EnableTaskSupplement: true}, agent.Result{Status: "completed", Output: "original reply"}, 0, problem, &execenv.Environment{LocalWorktree: wt}, slog.Default(), "review-task", new(atomic.Int32))
	if backend.runErr != nil {
		t.Fatal(backend.runErr)
	}
	if _, err := wt.Finalize(slog.Default()); err != nil {
		t.Fatal(err)
	}
	if backend.gotOpts.EnableTaskSupplement {
		t.Error("the repair turn was started with additional-message intake enabled")
	}
	if n := claims.Load(); n != 0 {
		t.Errorf("the repair turn claimed %d additional message(s)", n)
	}
	if delivered.Load() && !strings.Contains(result.Output, "42") {
		t.Fatalf("additional guidance acknowledged delivered during housekeeping, but its answer was discarded: output=%q", result.Output)
	}
}
