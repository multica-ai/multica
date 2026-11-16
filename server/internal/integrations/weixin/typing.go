package weixin

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const typingKeepalive = 5 * time.Second
const typingCallTimeout = 3 * time.Second
const typingTicketTTL = 5 * time.Minute

type typingStore interface {
	IsWeixinTypingActive(context.Context, db.IsWeixinTypingActiveParams) (bool, error)
	SettleWeixinTypingInputs(context.Context, db.SettleWeixinTypingInputsParams) error
	GetWeixinAccountState(context.Context, db.GetWeixinAccountStateParams) (db.WeixinAccountState, error)
}

type typingRun struct {
	installation pgtype.UUID
	cancel       context.CancelFunc
	wake         chan struct{}
	done         chan struct{}
}

type cachedTypingTicket struct {
	user, token, ticket string
	expires             time.Time
}

// Only the ingesting connector starts keepalives. Durable task state also clears
// them when a different API replica receives completion, cancellation or retry.
type TypingNotifier struct {
	q        typingStore
	box      *secretbox.Box
	client   *http.Client
	logger   *slog.Logger
	interval time.Duration
	mu       sync.Mutex
	ctx      context.Context
	runs     map[pgtype.UUID]*typingRun
	tickets  map[pgtype.UUID]cachedTypingTicket
	wg       sync.WaitGroup
}

func NewTypingNotifier(q typingStore, box *secretbox.Box, logger *slog.Logger) *TypingNotifier {
	if logger == nil {
		logger = slog.Default()
	}
	return &TypingNotifier{q: q, box: box, logger: logger, interval: typingKeepalive, runs: make(map[pgtype.UUID]*typingRun), tickets: make(map[pgtype.UUID]cachedTypingTicket)}
}

func (n *TypingNotifier) Start(ctx context.Context) {
	n.mu.Lock()
	n.ctx = ctx
	n.mu.Unlock()
}

func (n *TypingNotifier) wait() {
	// Shutdown cancels ctx first; synchronize with an ingest already adding work.
	n.mu.Lock()
	n.mu.Unlock()
	n.wg.Wait()
}

func (n *TypingNotifier) OnIngested(_ context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, sessionID, _ pgtype.UUID) {
	row, ok := inst.Platform.(db.ChannelInstallation)
	if !ok || !sessionID.Valid {
		return
	}
	creds, err := decodeCredentials(row.Config, n.box)
	if err != nil || msg.Source.SenderID != creds.UserID {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.ctx == nil || n.ctx.Err() != nil {
		return
	}
	if run := n.runs[sessionID]; run != nil {
		select {
		case run.wake <- struct{}{}:
		default:
		}
		return
	}
	ctx, cancel := context.WithCancel(n.ctx)
	run := &typingRun{installation: inst.ID, cancel: cancel, wake: make(chan struct{}, 1), done: make(chan struct{})}
	n.runs[sessionID] = run
	n.wg.Add(1)
	go n.keepalive(ctx, sessionID, row, creds, run)
}

func (n *TypingNotifier) OnSettled(ctx context.Context, sessionID pgtype.UUID, scope engine.TypingSettlement) {
	if err := n.q.SettleWeixinTypingInputs(ctx, db.SettleWeixinTypingInputsParams{ThroughMessageID: scope.ThroughMessageID, ChatSessionID: sessionID, WorkspaceID: scope.WorkspaceID, InstallationID: scope.InstallationID, ContextRevision: pgtype.Int8{Int64: scope.ContextRevision, Valid: true}}); err != nil {
		return
	}
	n.Wake(sessionID)
}

func (n *TypingNotifier) Wake(sessionID pgtype.UUID) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if run := n.runs[sessionID]; run != nil {
		select {
		case run.wake <- struct{}{}:
		default:
		}
	}
}

func (n *TypingNotifier) StopInstallation(ctx context.Context, id pgtype.UUID) {
	n.mu.Lock()
	var done []<-chan struct{}
	for _, run := range n.runs {
		if run.installation == id {
			run.cancel()
			done = append(done, run.done)
		}
	}
	delete(n.tickets, id)
	n.mu.Unlock()
	for _, ch := range done {
		select {
		case <-ch:
		case <-ctx.Done():
			return
		}
	}
}

func (n *TypingNotifier) keepalive(ctx context.Context, sessionID pgtype.UUID, inst db.ChannelInstallation, creds credentials, run *typingRun) {
	defer n.wg.Done()
	defer close(run.done)
	defer run.cancel()
	defer func() {
		n.mu.Lock()
		if n.runs[sessionID] == run {
			delete(n.runs, sessionID)
		}
		n.mu.Unlock()
	}()
	api := newAPI(creds.BaseURL, creds.Token, n.client)
	ticket := ""
	started := false
	defer func() {
		if ticket != "" {
			clear, cancel := context.WithTimeout(context.WithoutCancel(ctx), typingCallTimeout)
			defer cancel()
			if err := api.sendTyping(clear, creds.UserID, ticket, 2); err == nil {
				n.logger.Debug("weixin: typing stopped", "session_id", sessionID)
			}
		}
	}()
	ticker := time.NewTicker(n.interval)
	defer ticker.Stop()
	for {
		call, cancel := context.WithTimeout(ctx, typingCallTimeout)
		active, err := n.q.IsWeixinTypingActive(call, db.IsWeixinTypingActiveParams{InstallationID: inst.ID, ChatSessionID: sessionID, Config: inst.Config})
		if err != nil || !active {
			cancel()
			return
		}
		if ticket == "" {
			ticket = n.ticket(call, inst.ID, creds, api)
		}
		if ticket == "" {
			cancel()
			return
		}
		err = api.sendTyping(call, creds.UserID, ticket, 1)
		cancel()
		// Typing is cosmetic: a transient send failure must not end keepalives.
		if err == nil && !started {
			n.logger.DebugContext(ctx, "weixin: typing started", "session_id", sessionID)
			started = true
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-run.wake:
		}
	}
}

func (n *TypingNotifier) ticket(ctx context.Context, id pgtype.UUID, creds credentials, api *apiClient) string {
	n.mu.Lock()
	cached := n.tickets[id]
	n.mu.Unlock()
	if cached.user == creds.UserID && cached.token == creds.Token && time.Now().Before(cached.expires) {
		return cached.ticket
	}
	state, err := n.q.GetWeixinAccountState(ctx, db.GetWeixinAccountStateParams{InstallationID: id, BotID: creds.AppID})
	if err != nil {
		return ""
	}
	contextToken, _ := n.box.Open(state.ContextTokenEncrypted)
	ticket, err := api.typingTicket(ctx, creds.UserID, string(contextToken))
	if err != nil {
		return ""
	}
	n.mu.Lock()
	// The ticket is cosmetic; a full cache simply leaves this result uncached.
	if len(n.tickets) >= 1024 {
		for key, entry := range n.tickets {
			if time.Now().After(entry.expires) {
				delete(n.tickets, key)
			}
		}
	}
	if len(n.tickets) < 1024 {
		n.tickets[id] = cachedTypingTicket{user: creds.UserID, token: creds.Token, ticket: ticket, expires: time.Now().Add(typingTicketTTL)}
	}
	n.mu.Unlock()
	return ticket
}
