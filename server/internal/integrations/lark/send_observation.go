package lark

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const sendObservationTimeLayout = "2006-01-02T15:04:05.000000000Z"

// SendObservation describes one completed text/card API call, not delivery,
// readership, attachment completion, or connection health. No provider text or
// destination is retained. Fixed-width UTC timestamps preserve completion order
// when JSONB updates race, including completions within one microsecond.
type SendObservation struct {
	ObservedAt string `json:"observed_at"`
	Result     string `json:"result"`
	Reason     string `json:"reason"`
}

type SendObserver interface {
	RecordSendObservation(context.Context, Installation, SendObservation) error
}

type sendObservationBatchKey struct{}

type sendObservationBatch struct {
	installation Installation
	observations []SendObservation
}

func classifySendObservation(at time.Time, messageID string, err error) SendObservation {
	o := SendObservation{ObservedAt: at.UTC().Format(sendObservationTimeLayout), Result: "uncertain", Reason: "unconfirmed"}
	if err == nil && messageID != "" {
		o.Result, o.Reason = "api_confirmed", "none"
		return o
	}
	var unconfirmed *mediaSendUnconfirmed
	if errors.As(err, &unconfirmed) {
		return o
	}
	var statusErr *larkAPIStatusError
	if errors.As(err, &statusErr) && statusErr.StatusCode != 400 && statusErr.StatusCode != 403 {
		return o
	}
	// Only documented IM send/reply refusals are definitive. In particular,
	// code 0 (missing message_id), 230049 (sending), transport errors and
	// unknown future errors cannot establish that nothing was sent.
	// https://open.feishu.cn/document/server-docs/im-v1/message/create
	// https://open.feishu.cn/document/server-docs/im-v1/message/reply
	switch larkErrorCode(err) {
	case 230002:
		o.Reason = "bot_not_in_chat"
	case 230006:
		o.Reason = "bot_disabled"
	case 230013:
		o.Reason = "app_unavailable"
	case 230027:
		o.Reason = "missing_permission"
	case 230035:
		o.Reason = "chat_permission"
	default:
		return o
	}
	o.Result = "rejected"
	return o
}

func (p *Patcher) observeSend(ctx context.Context, send func() (string, error)) (string, error) {
	messageID, err := send()
	// Capture completion before persistence: a delayed write must not replace
	// a newer call's observation. Keep the exact generation loaded before send.
	completedAt := p.cfg.Now()
	batch, ok := ctx.Value(sendObservationBatchKey{}).(*sendObservationBatch)
	if !ok || p.cfg.SendObserver == nil {
		return messageID, err
	}
	batch.observations = append(batch.observations, classifySendObservation(completedAt, messageID, err))
	return messageID, err
}

func (p *Patcher) recordSendObservations(ctx context.Context, batch *sendObservationBatch) {
	// Persist after the reply path returns so a blocked diagnostic write
	// cannot consume the send/fallback deadline or delay attachment handoff.
	for _, observation := range batch.observations {
		recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 250*time.Millisecond)
		err := p.cfg.SendObserver.RecordSendObservation(recordCtx, batch.installation, observation)
		cancel()
		if err != nil {
			// Provider/storage text is unnecessary and may contain secrets.
			p.cfg.Logger.Warn("lark: text/card send observation was not recorded")
		}
	}
}

func (s *InstallationService) RecordSendObservation(ctx context.Context, inst Installation, observation SendObservation) error {
	raw, err := json.Marshal(observation)
	if err != nil {
		return err
	}
	return s.queries.Queries.RecordFeishuSendObservation(ctx, db.RecordFeishuSendObservationParams{
		ID: inst.ID, WorkspaceID: inst.WorkspaceID, AppID: inst.AppID,
		BindingGeneration: inst.BindingGeneration, Observation: raw,
	})
}
