package middleware

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/auth"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// EnrollmentTxStarter is the database transaction boundary used to fence a
// daemon request against concurrent rotation/revocation. No auth cache is used.
type EnrollmentTxStarter interface {
	Begin(context.Context) (pgx.Tx, error)
}

// Enrollment fences hold one connection while downstream handlers use the pool.
// Leave at least half the pool available to complete those protected operations.
func NewEnrollmentTxStarter(starter EnrollmentTxStarter) EnrollmentTxStarter {
	pool, ok := starter.(*pgxpool.Pool)
	if !ok || pool == nil {
		return nil
	}
	return &enrollmentTxBudget{starter: starter, slots: make(chan struct{}, pool.Config().MaxConns/2)}
}

type enrollmentTxBudget struct {
	starter EnrollmentTxStarter
	slots   chan struct{}
}

func (b *enrollmentTxBudget) Begin(ctx context.Context) (pgx.Tx, error) {
	select {
	case b.slots <- struct{}{}:
	default:
		return nil, errors.New("daemon enrollment database capacity unavailable")
	}
	tx, err := b.starter.Begin(ctx)
	if err != nil {
		<-b.slots
		return nil, err
	}
	return &enrollmentBudgetTx{Tx: tx, release: func() { <-b.slots }}, nil
}

type enrollmentBudgetTx struct {
	pgx.Tx
	release func()
	once    sync.Once
}

func (tx *enrollmentBudgetTx) Commit(ctx context.Context) error {
	err := tx.Tx.Commit(ctx)
	tx.once.Do(tx.release)
	return err
}

func (tx *enrollmentBudgetTx) Rollback(ctx context.Context) error {
	err := tx.Tx.Rollback(ctx)
	tx.once.Do(tx.release)
	return err
}

type enrollmentContextKey int

const (
	enrollmentHashKey enrollmentContextKey = iota
	enrollmentOwnerKey
)

func DaemonEnrollmentHashFromContext(ctx context.Context) string {
	value, _ := ctx.Value(enrollmentHashKey).(string)
	return value
}
func DaemonEnrollmentOwnerFromContext(ctx context.Context) string {
	value, _ := ctx.Value(enrollmentOwnerKey).(string)
	return value
}

// serveEnrolledDaemon keeps the enrollment row locked during an HTTP operation.
// The WebSocket handler returns after synchronously registering the connection,
// so the lock covers its upgrade/registration without spanning the connection
// lifetime. Subsequent heartbeats and RPCs acquire their own authorization lock.
func serveEnrolledDaemon(w http.ResponseWriter, r *http.Request, next http.Handler, queries *db.Queries, starter EnrollmentTxStarter, raw string) {
	if queries == nil || starter == nil {
		writeError(w, http.StatusServiceUnavailable, "daemon enrollment unavailable")
		return
	}
	tx, err := starter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "daemon enrollment unavailable")
		return
	}
	defer tx.Rollback(context.Background())
	hash := auth.HashToken(raw)
	var derived *db.DaemonToken
	if len(raw) > 4 && raw[:4] == "mds_" {
		row, err := queries.WithTx(tx).GetDaemonTokenByHash(r.Context(), hash)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusUnauthorized, "invalid derived daemon credential")
			} else {
				writeError(w, http.StatusServiceUnavailable, "daemon enrollment unavailable")
			}
			return
		}
		derived = &row
		if !derived.EnrollmentTokenHash.Valid {
			writeError(w, http.StatusUnauthorized, "invalid derived daemon credential")
			return
		}
		hash = derived.EnrollmentTokenHash.String
	}
	enrollment, err := queries.WithTx(tx).LockLiveDaemonEnrollment(r.Context(), hash)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusUnauthorized, "invalid daemon enrollment credential")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "daemon enrollment unavailable")
		return
	}
	if !enrollment.ExpiresAt.Valid || !enrollment.ExpiresAt.Time.After(time.Now()) {
		writeError(w, http.StatusUnauthorized, "invalid daemon enrollment credential")
		return
	}
	if derived != nil && (derived.WorkspaceID != enrollment.WorkspaceID || derived.DaemonID != enrollment.DaemonID || derived.OwnerID != enrollment.CreatorID) {
		writeError(w, http.StatusUnauthorized, "invalid derived daemon credential")
		return
	}
	ctx := WithDaemonTokenOwnerID(r.Context(), uuidToString(enrollment.CreatorID))
	ctx = context.WithValue(ctx, ctxKeyDaemonWorkspaceID, uuidToString(enrollment.WorkspaceID))
	ctx = context.WithValue(ctx, ctxKeyDaemonID, enrollment.DaemonID)
	ctx = context.WithValue(ctx, ctxKeyDaemonAuthPath, "daemon_enrollment")
	ctx = context.WithValue(ctx, enrollmentHashKey, hash)
	ctx = context.WithValue(ctx, enrollmentOwnerKey, uuidToString(enrollment.CreatorID))
	// Never propagate a caller-supplied user identity from a service credential.
	r.Header.Del("X-User-ID")
	next.ServeHTTP(w, r.WithContext(ctx))
	// No enrollment writes are made in this transaction. Rollback releases the
	// authorization lock only after the downstream operation has returned.
}

// WithDaemonEnrollmentHash preserves the authoritative credential fence when
// dispatching an authenticated WebSocket RPC through an HTTP handler.
func WithDaemonEnrollmentHash(ctx context.Context, hash string) context.Context {
	return context.WithValue(ctx, enrollmentHashKey, hash)
}
