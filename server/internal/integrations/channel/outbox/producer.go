package outbox

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Request is one row to enqueue. It exists so callers describe a delivery in
// domain terms instead of assembling generated query params, and so the
// business key stays visible at every call site.
type Request struct {
	InstallationID pgtype.UUID
	WorkspaceID    pgtype.UUID

	// ChatSessionID is optional. When set, the row is fenced on the session
	// still being bound to this installation and still active — both at claim
	// time and again immediately before send.
	ChatSessionID pgtype.UUID

	// SourceKind and SourceID form the business key that makes enqueue
	// idempotent. SourceID must identify the business result itself (a task
	// id, say), never the attempt: a per-attempt id would let the realtime
	// producer and the reconciler both insert and deliver the same reply.
	SourceKind string
	SourceID   string

	TargetChatID   string
	TargetChatType int16

	// MsgType and Payload are opaque to this package; the channel's Sender
	// renders them at send time.
	MsgType string
	Payload []byte
}

// ProducerStore is the generated-query surface the Producer needs.
type ProducerStore interface {
	EnqueueChannelOutbound(ctx context.Context, arg db.EnqueueChannelOutboundParams) (db.ChannelOutboundQueue, error)
}

// Producer enqueues rows and nudges a local consumer. Any replica may hold
// one; the queue is what decouples producing a reply from being the replica
// that can write it.
type Producer struct {
	channelType string
	q           ProducerStore
	wake        *WakeRegistry
	metrics     Metrics
}

// NewProducer builds a producer for one channel type. wake is optional.
func NewProducer(channelType string, q ProducerStore, wake *WakeRegistry, metrics Metrics) (*Producer, error) {
	if strings.TrimSpace(channelType) == "" {
		return nil, errors.New("outbox: channel type is required")
	}
	if q == nil {
		return nil, errors.New("outbox: queries are required")
	}
	if metrics == nil {
		metrics = NoopMetrics()
	}
	return &Producer{channelType: channelType, q: q, wake: wake, metrics: metrics}, nil
}

// Enqueue inserts one row and wakes the installation's consumer.
//
// It reports whether the row was freshly inserted. false means the business
// key already existed and this call delivered nothing new — the expected
// outcome when the realtime path and the reconciler race, and the reason
// neither needs to coordinate with the other.
//
// path is the Metrics attribution bucket (EnqueuePathRealtime or
// EnqueuePathReconcile).
func (p *Producer) Enqueue(ctx context.Context, req Request, path string) (bool, error) {
	if !req.InstallationID.Valid || !req.WorkspaceID.Valid {
		return false, errors.New("outbox: installation and workspace ids are required")
	}
	if strings.TrimSpace(req.SourceKind) == "" || strings.TrimSpace(req.SourceID) == "" {
		return false, errors.New("outbox: source kind and id are required")
	}
	if strings.TrimSpace(req.TargetChatID) == "" {
		return false, errors.New("outbox: target chat id is required")
	}

	_, err := p.q.EnqueueChannelOutbound(ctx, db.EnqueueChannelOutboundParams{
		InstallationID: req.InstallationID,
		WorkspaceID:    req.WorkspaceID,
		ChannelType:    p.channelType,
		ChatSessionID:  req.ChatSessionID,
		SourceKind:     req.SourceKind,
		SourceID:       req.SourceID,
		TargetChatID:   req.TargetChatID,
		TargetChatType: req.TargetChatType,
		MsgType:        req.MsgType,
		Payload:        req.Payload,
	})
	switch {
	case err == nil:
		p.metrics.RecordEnqueued(p.channelType, path, req.SourceKind)
	case errors.Is(err, pgx.ErrNoRows):
		// ON CONFLICT DO NOTHING returned no row: this business key is already
		// queued or delivered. Still wake — the row that won the race may be
		// sitting unclaimed on this replica.
		p.wakeInstallation(req.InstallationID)
		return false, nil
	default:
		return false, err
	}

	p.wakeInstallation(req.InstallationID)
	return true, nil
}

func (p *Producer) wakeInstallation(id pgtype.UUID) {
	if p.wake == nil {
		return
	}
	p.wake.Wake(util.UUIDToString(id))
}

// ChannelType reports the channel this producer enqueues for.
func (p *Producer) ChannelType() string { return p.channelType }

// LogValue keeps a producer cheap to log without leaking its store.
func (p *Producer) LogValue() slog.Value {
	return slog.GroupValue(slog.String("channel_type", p.channelType))
}
