// Package mask wires JEH-1079 data-classification × actor on the cerebro
// side. It assembles the Persona /v1/check request — including the
// resource's overall classification and the static field-level
// classifications for the resource kind — and turns the returned
// masked_fields list into a function the handler can apply to a JSON
// response object before writing it on the wire.
//
// Two flavours of classification meet here:
//
//	per-row    — issue.classification / comment.classification /
//	             attachment.classification, set when the row was
//	             created or promoted by the owner. Default 'internal'.
//	per-field  — static map for kinds whose sensitivity is inherent to
//	             the field (member.email is always pii, member.name is
//	             internal). Lives in fields.go.
//
// Persona is the single source of truth for "may this actor see field X
// of this resource"; this package never makes that judgement locally.
// When persona is unreachable the layer fails CLOSED — the caller sees
// the same "all fields redacted" outcome as a deny.
package mask

import (
	"context"
	"errors"
	"log/slog"

	persona "github.com/hvejsel/firtal-persona/sdk/go"
)

// DefaultClassification is the safe default applied at write time and
// when a row's classification column is somehow empty. Matches the
// migration's DEFAULT clause and the agreed scope precision.
const DefaultClassification = "internal"

// Decision is what the handler acts on. Allowed=false means "do not
// render this resource"; Allowed=true with a non-empty MaskedFields
// means "render but strip these fields before serialising".
type Decision struct {
	Allowed      bool
	MaskedFields []string
	DecisionID   string
	Reason       string
}

// FullAccess is the canned response handlers use when persona is not
// configured at all (env vars empty). Treats every read as full access,
// no redaction — matches the pre-JEH-1079 baseline so a workspace that
// has not opted into persona keeps working unchanged.
func FullAccess() Decision {
	return Decision{Allowed: true}
}

// CheckClient is the minimal SDK surface this package needs. Lets tests
// pass a stub without spinning up an HTTP server.
type CheckClient interface {
	CheckResourceWithMasking(
		ctx context.Context,
		subjectActorID, action, kind, id, classification string,
		fieldClassifications map[string]string,
		extraAttrs map[string]any,
	) (*persona.CheckResult, error)
}

// Checker is the package's single dependency. Construct with NewChecker
// — handlers store it on the Handler struct and call Decide() per read.
type Checker struct {
	client CheckClient
	log    *slog.Logger
}

// NewChecker wraps a persona SDK client. Pass nil log to use
// slog.Default.
func NewChecker(client CheckClient, log *slog.Logger) *Checker {
	if log == nil {
		log = slog.Default()
	}
	return &Checker{client: client, log: log}
}

// Decide asks persona "may actor read this resource, and if so what
// fields must be redacted". rowClassification is the resource's
// per-row label (use DefaultClassification when no per-row column
// exists for the kind). fieldClasses is the per-field static map; pass
// nil when the kind has no per-field labels.
//
// Fail-closed: any transport / decode error becomes Allowed=false.
// Callers MUST treat Decision.Allowed=false as "don't render".
func (c *Checker) Decide(
	ctx context.Context,
	actorID, action, kind, resourceID, rowClassification string,
	fieldClasses map[string]string,
) Decision {
	if c == nil || c.client == nil {
		// No persona configured = no redaction; existing behaviour.
		return FullAccess()
	}
	if rowClassification == "" {
		rowClassification = DefaultClassification
	}
	res, err := c.client.CheckResourceWithMasking(
		ctx, actorID, action, kind, resourceID, rowClassification, fieldClasses, nil,
	)
	if err != nil {
		c.log.Warn("mask: persona check failed, failing closed",
			"err", err, "kind", kind, "resource_id", resourceID, "actor_id", actorID)
		return Decision{Allowed: false, Reason: "persona unreachable"}
	}
	if res == nil {
		return Decision{Allowed: false, Reason: "persona returned nil result"}
	}
	return Decision{
		Allowed:      res.Allowed,
		MaskedFields: res.MaskedFields,
		DecisionID:   res.DecisionID,
		Reason:       res.Reason,
	}
}

// ErrNotConfigured signals the workspace runs without persona — handlers
// can short-circuit instead of building decision plumbing.
var ErrNotConfigured = errors.New("persona not configured")
