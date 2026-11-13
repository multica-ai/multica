package telegram

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestOutboundRevocationStopsExistingStream(t *testing.T) {
	bot := &auditBot{}
	o, q, clock, e := review8545Setup(t, bot)
	o.handleTaskMessage(telegramPartialEvent(e.TaskID, "Checking."))
	q.routing.installation.Status = "revoked"
	clock.advance(4 * time.Second)
	o.handleTaskMessage(telegramPartialEvent(e.TaskID, " PRIVATE_AFTER_REVOKE"))
	bot.mu.Lock()
	defer bot.mu.Unlock()
	for _, body := range bot.messages {
		if strings.Contains(body, "PRIVATE_AFTER_REVOKE") {
			t.Fatalf("revoked installation received streamed content: %q; calls=%v", body, bot.methods)
		}
	}
}

func TestOutboundRevocationStopsHeartbeat(t *testing.T) {
	h := newProgressHarness(t, channel.ChatTypeP2P)
	h.launch()
	h.o.q.(*fakeTelegramOutboundQueries).installation.Status = "revoked"
	h.skew.Add(int64(75 * time.Second))
	h.openEditSlot()
	h.o.mu.Lock()
	st := h.o.streams[h.taskID]
	gen := st.flushGen
	h.o.mu.Unlock()
	h.o.flushStream(st.target, st, gen)
	if got := h.sent(); len(got) != 1 {
		t.Fatalf("heartbeat sent after revocation: %+v", got)
	}
	h.o.mu.Lock()
	defer h.o.mu.Unlock()
	if len(h.o.streams) != 0 {
		t.Fatal("revoked stream still holds its scheduler and timers")
	}
}

func TestOutboundRevocationStopsDeferredText(t *testing.T) {
	h := newProgressHarness(t, channel.ChatTypeP2P)
	h.text("我")
	h.o.handleTaskMessage(events.Event{
		TaskID: h.taskID, Type: protocol.EventTaskMessage,
		Payload: protocol.TaskMessagePayload{Type: "text", Content: "会检查这些文件。"},
	})
	h.o.q.(*fakeTelegramOutboundQueries).installation.Status = "revoked"
	deadline := time.Now().Add(3 * time.Second)
	for {
		h.o.mu.Lock()
		st := h.o.streams[h.taskID]
		h.o.mu.Unlock()
		if st == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("deferred push did not stop the revoked stream")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := h.sent(); len(got) != 1 {
		t.Fatalf("deferred text sent after revocation: %+v", got)
	}
}

type unavailableStreamInstallation struct{ outboundQueries }

func (q unavailableStreamInstallation) GetChannelInstallation(context.Context, db.GetChannelInstallationParams) (db.ChannelInstallation, error) {
	return db.ChannelInstallation{}, errors.New("installation lookup unavailable")
}

func TestOutboundStreamFailsClosedWhenInstallationCannotBeChecked(t *testing.T) {
	h := newProgressHarness(t, channel.ChatTypeP2P)
	h.text("Checking.")
	h.o.q = unavailableStreamInstallation{h.o.q}
	h.tool("Read", map[string]any{"file_path": "private.txt"})
	if got := h.sent(); len(got) != 1 {
		t.Fatalf("stream used cached credentials after lookup failed: %+v", got)
	}
}
