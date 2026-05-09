// Package deploy defines the multi-tenant adapter contract for Ship Hub
// Phase 6. Phases 1-5 baked GitHub Actions in as the only deploy
// mechanism; this package extracts the surface area an adapter has to
// implement so adding a new provider is a single file plus an init().
//
// The contract is deliberately small: webhook ingestion is the primary
// path, with optional polling and rollback. Adapters that can't satisfy
// an optional method return ErrPollNotSupported / ErrRollbackNotSupported
// rather than panicking — callers (the periodic poller, the rollback
// endpoint) check explicitly with SupportsRollback() / a sentinel error
// match so a misconfiguration becomes a clear UI message instead of a
// 500.
//
// All adapter logic lives downstream in pkg/deploy/adapters/. Each
// adapter file calls Register(...) from an init() so the binary's
// transitive imports decide which adapters are available — useful for
// the desktop daemon, which may want a smaller subset.
package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// Sentinel errors callers can match with errors.Is. Defined as concrete
// values rather than string-only because the adapter-error path runs
// across HTTP boundaries (handler → service → adapter), and a
// well-typed error keeps the matching cheap.
var (
	// ErrPollNotSupported means the adapter has no pull-based current-
	// state read. Returned from PollCurrent on adapters whose providers
	// don't expose a stable "what's deployed right now" API (e.g. our
	// generic webhook adapter — the user's payload mapping doesn't
	// imply a poll endpoint).
	ErrPollNotSupported = errors.New("deploy: adapter does not support polling")

	// ErrRollbackNotSupported is the rollback-side mate. Some providers
	// (Render, Fly today) require manual UI / CLI intervention; we
	// surface the limitation explicitly rather than silently failing.
	ErrRollbackNotSupported = errors.New("deploy: adapter does not support rollback")

	// ErrIrrelevantPayload signals OnWebhook that the payload was
	// authentic but uninteresting (e.g. a Vercel deployment.created event
	// for a different project). Returning a nil event with no error has
	// the same effect, but the explicit error helps the caller tell
	// "we ignored this on purpose" from "the adapter returned nil for
	// some other reason". Treated as success at the HTTP layer.
	ErrIrrelevantPayload = errors.New("deploy: payload not relevant to this environment")

	// ErrSignatureInvalid is the canonical signature-mismatch error —
	// adapters wrap their provider-specific verification errors with
	// this so the receiver can map every adapter to a 401.
	ErrSignatureInvalid = errors.New("deploy: signature mismatch")

	// ErrUnknownAdapter is returned by the registry when a kind isn't
	// registered. Match with errors.Is at the handler boundary to write
	// a 400 with a clear message.
	ErrUnknownAdapter = errors.New("deploy: unknown adapter kind")
)

// Environment is the slice of deploy_environment + deploy_adapter_config
// that adapters need at runtime. We pass a struct rather than the raw
// sqlc rows so a future schema reshape doesn't ripple into every adapter
// — and so tests don't need a database to instantiate one.
type Environment struct {
	// ID is the deploy_environment.id. Adapters use it to scope db
	// writes (e.g. updating the env's current_sha after a poll).
	ID pgtype.UUID
	// WorkspaceID is the parent workspace. Carried so adapters can emit
	// workspace-scoped events without an extra lookup.
	WorkspaceID pgtype.UUID
	// AdapterKind echoes deploy_environment.adapter_kind. Useful when
	// the same adapter struct serves multiple kinds (e.g. "github_actions"
	// and a future "github_app" variant).
	AdapterKind string
	// Config is the decrypted adapter-specific JSON blob. Adapters cast
	// it to their own struct in OnWebhook / PollCurrent / Rollback.
	// Empty when the env is using the default adapter and no row exists
	// in deploy_adapter_config.
	Config json.RawMessage
	// WebhookSecret is the decrypted inbound-webhook signing secret.
	// Empty for adapters that don't use signed webhooks (fly.io) or for
	// envs that haven't generated one yet.
	WebhookSecret string
	// TargetBranch is the env's target branch. Some adapters use it to
	// filter webhook events that fire for unrelated branches.
	TargetBranch string
	// Name is the env name. Used by adapters whose webhook payload
	// includes an "environment" field that selects the row.
	Name string
}

// DeployEvent is the canonical "something happened" envelope adapters
// return from OnWebhook. The receiver translates it into a deploy row
// upsert + a parent-env current_sha bump if Status is succeeded.
type DeployEvent struct {
	// Status is the canonical Multica deploy_status value
	// (pending|in_progress|succeeded|failed|rolled_back).
	Status string
	// SHA is the commit being deployed. Always required.
	SHA string
	// Ref is the branch (or tag) being deployed. Falls back to the env's
	// target_branch when the provider doesn't supply one.
	Ref string
	// LogURL is the provider's log/UI link for this deploy. Optional.
	LogURL string
	// ErrorMsg is non-empty for failed deploys. Populated from the
	// provider's error message verbatim — the UI displays it without
	// further parsing.
	ErrorMsg string
	// OccurredAt is when the provider says the event happened. The
	// receiver passes it through unchanged so cross-adapter timelines
	// don't drift due to receiver-side clock skew.
	OccurredAt time.Time
}

// DeployState mirrors what an adapter's poll result looks like — a
// snapshot of the env's current state. Reduced surface (vs DeployEvent)
// because polling answers "what's running" not "what just happened".
type DeployState struct {
	CurrentSHA string
	DeployedAt time.Time
	LogURL     string
}

// Adapter is the contract every Ship Hub deploy provider implements.
// Methods are documented at the package level; this declaration is the
// single source of truth for the surface.
type Adapter interface {
	// Name returns the canonical adapter kind string. Must match the
	// value persisted in deploy_environment.adapter_kind. Lower-snake-
	// case to keep URL-routability simple.
	Name() string

	// OnWebhook processes an inbound webhook payload from this provider.
	// Returns the deploy event to insert/update, or nil if the payload
	// is irrelevant (the receiver records a no-op delivery row).
	OnWebhook(ctx context.Context, env *Environment, raw json.RawMessage) (*DeployEvent, error)

	// VerifySignature checks the request's authenticity using the env's
	// stored secret. Wrap any underlying error with ErrSignatureInvalid
	// so the receiver can map every adapter to a 401 without growing a
	// per-adapter error switch.
	VerifySignature(env *Environment, headers http.Header, body []byte) error

	// PollCurrent returns the current deploy state by polling the
	// provider's API. Returns ErrPollNotSupported when the adapter
	// doesn't implement it.
	PollCurrent(ctx context.Context, env *Environment) (*DeployState, error)

	// SupportsPoll returns true when PollCurrent is meaningful for this
	// adapter. Used by the periodic poller goroutine to skip envs whose
	// adapter has no poll path — saves us a round trip per tick.
	SupportsPoll() bool

	// SupportsRollback mirrors SupportsPoll for the rollback dispatch.
	SupportsRollback() bool

	// Rollback instructs the provider to redeploy a prior SHA. Returns
	// ErrRollbackNotSupported when SupportsRollback is false (the
	// handler short-circuits before ever reaching this, but defensively
	// returning the sentinel keeps invariants correct).
	Rollback(ctx context.Context, env *Environment, targetSHA string) error
}
