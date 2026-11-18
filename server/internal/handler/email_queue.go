package handler

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

const (
	// emailQueueCapacity and emailSendConcurrency bound how many relay
	// conversations one process can have outstanding. A public relay costs ~7
	// round trips per message, so an unbounded fan-out turns a slow relay into
	// thousands of stuck goroutines; a rejected send is recoverable because the
	// client can request another code after its cooldown.
	emailQueueCapacity   = 64
	emailSendConcurrency = 4
	// emailSendTimeout bounds one send including the PLAIN→LOGIN reconnect,
	// which opens a second connection. It is deliberately longer than a healthy
	// session: exceeding it means the relay stopped answering, and the alternative
	// to giving up is holding the connection open indefinitely.
	emailSendTimeout = 15 * time.Second
)

// ErrEmailQueueFull reports that no queue slot was available. The email was not
// sent and will not be retried.
var ErrEmailQueueFull = errors.New("email send queue is full")

const (
	emailKindVerificationCode = "verification_code"
	emailKindInvitation       = "invitation"

	emailResultSuccess = "success"
	emailResultFailure = "failure"
)

type emailJob struct {
	kind      string
	to        string
	requestID string
	send      func(ctx context.Context) error
}

// emailSender is the send surface EmailQueue drives. *service.EmailService
// satisfies it; a test can install a stub through EmailQueue.sender.
type emailSender interface {
	SendVerificationCode(ctx context.Context, to, code string) error
	SendInvitationEmail(ctx context.Context, to, inviterName, workspaceName, invitationID string) error
}

// EmailQueue moves email delivery off the request path. Each send runs under its
// own deadline instead of the request's, so the handler can answer as soon as the
// job is queued and a wedged relay cannot extend a login request.
type EmailQueue struct {
	h      *Handler
	sender emailSender
	jobs   chan emailJob
}

func NewEmailQueue(h *Handler) *EmailQueue {
	return &EmailQueue{
		h:    h,
		jobs: make(chan emailJob, emailQueueCapacity),
	}
}

// SendVerificationCode queues the login-code email. A non-nil error means the
// code row exists but nothing was handed to the relay, which the caller must
// surface rather than answer 200.
func (q *EmailQueue) SendVerificationCode(email, code, requestID string) error {
	svc, err := q.service()
	if err != nil {
		return err
	}
	return q.submit(emailJob{
		kind:      emailKindVerificationCode,
		to:        email,
		requestID: requestID,
		send:      func(ctx context.Context) error { return svc.SendVerificationCode(ctx, email, code) },
	})
}

// SendInvitation queues a workspace invitation email. Invitation delivery stays
// best-effort: the invite itself is visible in the product, so saturation is
// logged instead of returned to the caller.
func (q *EmailQueue) SendInvitation(email, inviterName, workspaceName, invitationID string) {
	svc, err := q.service()
	if err != nil {
		slog.Warn("invitation email not queued", "email", email, "error", err)
		return
	}
	if err := q.submit(emailJob{
		kind:      emailKindInvitation,
		to:        email,
		requestID: invitationID,
		send: func(ctx context.Context) error {
			return svc.SendInvitationEmail(ctx, email, inviterName, workspaceName, invitationID)
		},
	}); err != nil {
		slog.Warn("invitation email not queued", "email", email, "invitation_id", invitationID, "error", err)
	}
}

func (q *EmailQueue) service() (emailSender, error) {
	if q == nil || q.h == nil {
		return nil, errors.New("email queue is not running")
	}
	if q.sender != nil {
		return q.sender, nil
	}
	if q.h.EmailService == nil {
		return nil, errors.New("email service is not configured")
	}
	return q.h.EmailService, nil
}

func (q *EmailQueue) submit(job emailJob) error {
	select {
	case q.jobs <- job:
		return nil
	default:
		return ErrEmailQueueFull
	}
}

// Run drains the queue until ctx is cancelled. Sends already handed to a worker
// keep running to their own deadline; jobs still queued are dropped, since the
// client asking again is the recovery path.
func (q *EmailQueue) Run(ctx context.Context) {
	if q == nil {
		return
	}

	var wg sync.WaitGroup
	wg.Add(emailSendConcurrency)
	for range emailSendConcurrency {
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case job := <-q.jobs:
					q.process(ctx, job)
				}
			}
		}()
	}
	wg.Wait()
}

func (q *EmailQueue) process(ctx context.Context, job emailJob) {
	sendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), emailSendTimeout)
	defer cancel()

	start := time.Now()
	err := job.send(sendCtx)
	elapsed := time.Since(start)

	result := emailResultSuccess
	if err != nil {
		result = emailResultFailure
		// The request already answered 200, so this line is the only record that
		// the code was stored and never delivered.
		slog.Error("email send failed",
			"kind", job.kind,
			"email", job.to,
			"duration", elapsed.String(),
			"error", err,
			"request_id", job.requestID,
		)
	}
	q.h.Metrics.RecordEmailSend(job.kind, result, elapsed.Seconds())
}
