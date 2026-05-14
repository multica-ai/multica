package credentials

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
)

// Bus event types — broadcast on the workspace channel. The cerebro prefix
// keeps the namespace separate from upstream `comment:created`-style events.
const (
	EventCredentialCreated      = "cerebro.credential.created"
	EventCredentialUpdated      = "cerebro.credential.updated"
	EventCredentialDeleted      = "cerebro.credential.deleted"
	EventCredentialRotated      = "cerebro.credential.rotated"
	EventCredentialBound        = "cerebro.credential.bound"
	EventCredentialUnbound      = "cerebro.credential.unbound"
	EventCredentialRevealed     = "cerebro.credential.revealed"
)

var (
	ErrCredentialNotFound   = errors.New("credential not found")
	ErrBindingNotFound      = errors.New("credential binding not found")
	ErrInvalidType          = errors.New("invalid credential type")
	ErrInvalidName          = errors.New("name is required")
	ErrInvalidValue         = errors.New("value is required")
	ErrInvalidResourceType  = errors.New("resource_type is required")
	ErrInvalidResourceID    = errors.New("resource_id is required")
	ErrInvalidMetadata      = errors.New("metadata must be a JSON object")
	ErrCredentialExists     = errors.New("a credential with this name and type already exists in this workspace")
	ErrCredentialBindingMismatch = errors.New("binding does not belong to credential")
)

// Service is the encapsulated business logic for the credential registry.
// Handlers are thin shells over this; tests exercise Service directly.
//
// Policy is the JEH-1197 gate for the five governance actions (attach,
// read_redacted, reveal, rotate, revoke). When nil, DenyAllChecker is
// used so a misconfigured deployment fails closed instead of granting
// blanket access. Create/Update are NOT in the JEH-1197 governance set
// — they continue to be gated by the upstream workspace-member
// middleware only.
type Service struct {
	Cerebro *cerebrodb.Queries
	Cipher  *Cipher
	Bus     *events.Bus
	Policy  PolicyChecker
}

func NewService(cerebro *cerebrodb.Queries, cipher *Cipher, bus *events.Bus) *Service {
	return &Service{Cerebro: cerebro, Cipher: cipher, Bus: bus, Policy: DenyAllChecker}
}

// WithPolicy returns a copy of the service with the supplied policy
// checker. Used by the wiring layer so the router can compose the
// production chain (owner + persona) without exposing field tags.
func (s *Service) WithPolicy(p PolicyChecker) *Service {
	clone := *s
	if p == nil {
		clone.Policy = DenyAllChecker
	} else {
		clone.Policy = p
	}
	return &clone
}

// AuthorizeRead is the handler-facing read gate. Used by List/Get/
// ListBindings/ListAudit before they call into the underlying loader.
// On deny: writes the audit row and returns *PolicyDeniedError. On
// allow: returns nil and DOES NOT write an audit row — allow-side read
// rows would balloon the table.
//
// credID may be zero (List). credType may be empty (List/audit
// summaries that don't carry a single type).
func (s *Service) AuthorizeRead(ctx context.Context, workspaceID, credentialID pgtype.UUID, credentialType Type, actorType string, actorID pgtype.UUID) error {
	return s.enforce(ctx, workspaceID, credentialID, credentialType, PermReadRedacted, actorType, actorID, "")
}

// enforce runs the policy check for a governance action, writes the
// audit row (allow on most actions; deny is always written), and
// returns nil on allow or *PolicyDeniedError on deny.
//
// allowAuditAction tells enforce which action string to record on an
// allow row. Pass "" to skip the allow-side audit write — used by the
// read path where success rows would balloon the table.
func (s *Service) enforce(
	ctx context.Context,
	workspaceID, credentialID pgtype.UUID,
	credentialType Type,
	perm Permission,
	actorType string,
	actorID pgtype.UUID,
	allowAuditAction Action,
) error {
	checker := s.Policy
	if checker == nil {
		checker = DenyAllChecker
	}
	dec := checker.Check(ctx, PolicyRequest{
		WorkspaceID:    workspaceID,
		CredentialID:   credentialID,
		CredentialType: credentialType,
		Permission:     perm,
		ActorType:      actorType,
		ActorID:        actorID,
	})
	if !dec.Allowed {
		s.auditDeny(ctx, workspaceID, credentialID, perm, actorType, actorID, dec.Reason)
		return &PolicyDeniedError{Permission: perm, Decision: dec}
	}
	if allowAuditAction != "" {
		s.auditAllow(ctx, workspaceID, credentialID, allowAuditAction, actorType, actorID, dec.Reason, nil)
	}
	return nil
}

// CreateInput is the validated input to Create. The handler does JSON
// decoding; Service does business-rule validation.
type CreateInput struct {
	WorkspaceID pgtype.UUID
	Type        Type
	Name        string
	Description string
	Value       string
	Metadata    json.RawMessage
	ExpiresAt   *time.Time
	ActorType   string
	ActorID     pgtype.UUID
}

// UpdateInput captures the mutable metadata fields. nil pointers mean
// "leave untouched" (PATCH semantics).
type UpdateInput struct {
	Name        *string
	Description *string
	Metadata    json.RawMessage
	ExpiresAt   **time.Time // ptr-to-ptr so we can distinguish "unset" from "clear"
}

func (s *Service) Create(ctx context.Context, in CreateInput) (cerebrodb.CerebroCredential, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	in.Value = strings.TrimSpace(in.Value)
	if !in.Type.Valid() {
		return cerebrodb.CerebroCredential{}, ErrInvalidType
	}
	if in.Name == "" {
		return cerebrodb.CerebroCredential{}, ErrInvalidName
	}
	if in.Value == "" {
		return cerebrodb.CerebroCredential{}, ErrInvalidValue
	}
	metadata, err := normalizeMetadata(in.Metadata)
	if err != nil {
		return cerebrodb.CerebroCredential{}, err
	}
	if s.Cipher == nil {
		return cerebrodb.CerebroCredential{}, ErrCipherMissing
	}
	encrypted, err := s.Cipher.Encrypt([]byte(in.Value))
	if err != nil {
		return cerebrodb.CerebroCredential{}, err
	}
	hint := MaskValue(in.Value)

	row, err := s.Cerebro.CreateCerebroCredential(ctx, cerebrodb.CreateCerebroCredentialParams{
		WorkspaceID:    in.WorkspaceID,
		Type:           string(in.Type),
		Name:           in.Name,
		Description:    in.Description,
		EncryptedValue: encrypted,
		ValueHint:      hint,
		Metadata:       metadata,
		ExpiresAt:      timeToTimestamptz(in.ExpiresAt),
		CreatedByType:  in.ActorType,
		CreatedByID:    in.ActorID,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return cerebrodb.CerebroCredential{}, ErrCredentialExists
		}
		return cerebrodb.CerebroCredential{}, err
	}
	s.auditAllow(ctx, row.WorkspaceID, row.ID, ActionCreate, in.ActorType, in.ActorID, "", nil)
	s.publish(EventCredentialCreated, row.WorkspaceID, in.ActorType, in.ActorID, credentialResponseFromModel(row))
	return row, nil
}

func (s *Service) Get(ctx context.Context, workspaceID, credentialID pgtype.UUID) (cerebrodb.CerebroCredential, error) {
	row, err := s.Cerebro.GetCerebroCredential(ctx, credentialID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return cerebrodb.CerebroCredential{}, ErrCredentialNotFound
		}
		return cerebrodb.CerebroCredential{}, err
	}
	if !uuidEqual(row.WorkspaceID, workspaceID) {
		return cerebrodb.CerebroCredential{}, ErrCredentialNotFound
	}
	return row, nil
}

func (s *Service) List(ctx context.Context, workspaceID pgtype.UUID) ([]cerebrodb.CerebroCredential, error) {
	return s.Cerebro.ListCerebroCredentials(ctx, workspaceID)
}

func (s *Service) Update(ctx context.Context, workspaceID, credentialID pgtype.UUID, actorType string, actorID pgtype.UUID, in UpdateInput) (cerebrodb.CerebroCredential, error) {
	existing, err := s.Get(ctx, workspaceID, credentialID)
	if err != nil {
		return cerebrodb.CerebroCredential{}, err
	}

	name := existing.Name
	if in.Name != nil {
		trimmed := strings.TrimSpace(*in.Name)
		if trimmed == "" {
			return cerebrodb.CerebroCredential{}, ErrInvalidName
		}
		name = trimmed
	}
	description := existing.Description
	if in.Description != nil {
		description = strings.TrimSpace(*in.Description)
	}
	metadata := []byte(existing.Metadata)
	if len(in.Metadata) > 0 {
		m, err := normalizeMetadata(in.Metadata)
		if err != nil {
			return cerebrodb.CerebroCredential{}, err
		}
		metadata = m
	}
	expiresAt := existing.ExpiresAt
	if in.ExpiresAt != nil {
		expiresAt = timeToTimestamptz(*in.ExpiresAt)
	}

	row, err := s.Cerebro.UpdateCerebroCredentialMetadata(ctx, cerebrodb.UpdateCerebroCredentialMetadataParams{
		ID:          credentialID,
		Name:        name,
		Description: description,
		Metadata:    metadata,
		ExpiresAt:   expiresAt,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return cerebrodb.CerebroCredential{}, ErrCredentialExists
		}
		return cerebrodb.CerebroCredential{}, err
	}
	s.auditAllow(ctx, row.WorkspaceID, row.ID, ActionUpdate, actorType, actorID, "", nil)
	s.publish(EventCredentialUpdated, row.WorkspaceID, actorType, actorID, credentialResponseFromModel(row))
	return row, nil
}

// Rotate replaces the encrypted value of an existing credential. value_hint
// is regenerated from the new plaintext; last_rotated_at is bumped so the
// rotation policy module can drive its schedule off this column.
func (s *Service) Rotate(ctx context.Context, workspaceID, credentialID pgtype.UUID, actorType string, actorID pgtype.UUID, newValue string) (cerebrodb.CerebroCredential, error) {
	existing, err := s.Get(ctx, workspaceID, credentialID)
	if err != nil {
		return cerebrodb.CerebroCredential{}, err
	}
	if err := s.enforce(ctx, existing.WorkspaceID, existing.ID, Type(existing.Type), PermRotate, actorType, actorID, ""); err != nil {
		return cerebrodb.CerebroCredential{}, err
	}
	newValue = strings.TrimSpace(newValue)
	if newValue == "" {
		return cerebrodb.CerebroCredential{}, ErrInvalidValue
	}
	if s.Cipher == nil {
		return cerebrodb.CerebroCredential{}, ErrCipherMissing
	}
	encrypted, err := s.Cipher.Encrypt([]byte(newValue))
	if err != nil {
		return cerebrodb.CerebroCredential{}, err
	}
	row, err := s.Cerebro.RotateCerebroCredentialValue(ctx, cerebrodb.RotateCerebroCredentialValueParams{
		ID:             existing.ID,
		EncryptedValue: encrypted,
		ValueHint:      MaskValue(newValue),
	})
	if err != nil {
		return cerebrodb.CerebroCredential{}, err
	}
	s.auditAllow(ctx, row.WorkspaceID, row.ID, ActionRotate, actorType, actorID, "", nil)
	s.publish(EventCredentialRotated, row.WorkspaceID, actorType, actorID, credentialResponseFromModel(row))
	return row, nil
}

func (s *Service) Delete(ctx context.Context, workspaceID, credentialID pgtype.UUID, actorType string, actorID pgtype.UUID) (cerebrodb.CerebroCredential, error) {
	existing, err := s.Get(ctx, workspaceID, credentialID)
	if err != nil {
		return cerebrodb.CerebroCredential{}, err
	}
	if err := s.enforce(ctx, existing.WorkspaceID, existing.ID, Type(existing.Type), PermRevoke, actorType, actorID, ""); err != nil {
		return cerebrodb.CerebroCredential{}, err
	}
	if err := s.Cerebro.DeleteCerebroCredential(ctx, existing.ID); err != nil {
		return cerebrodb.CerebroCredential{}, err
	}
	s.auditAllow(ctx, existing.WorkspaceID, existing.ID, ActionDelete, actorType, actorID, "", nil)
	s.publish(EventCredentialDeleted, existing.WorkspaceID, actorType, actorID, credentialResponseFromModel(existing))
	return existing, nil
}

// Reveal decrypts and returns the plaintext value. Every call is appended
// to cerebro_credential_audit before the plaintext is returned — if the
// audit write fails we abort so a reveal never goes unrecorded.
func (s *Service) Reveal(ctx context.Context, workspaceID, credentialID pgtype.UUID, actorType string, actorID pgtype.UUID) (cerebrodb.CerebroCredential, string, error) {
	row, err := s.Get(ctx, workspaceID, credentialID)
	if err != nil {
		return cerebrodb.CerebroCredential{}, "", err
	}
	if err := s.enforce(ctx, row.WorkspaceID, row.ID, Type(row.Type), PermReveal, actorType, actorID, ""); err != nil {
		return cerebrodb.CerebroCredential{}, "", err
	}
	if s.Cipher == nil {
		return cerebrodb.CerebroCredential{}, "", ErrCipherMissing
	}
	plaintext, err := s.Cipher.Decrypt(row.EncryptedValue)
	if err != nil {
		return cerebrodb.CerebroCredential{}, "", err
	}
	// Reveal audit row is written synchronously — if it fails we abort
	// instead of returning plaintext. Allowed=true so we use auditAllow's
	// underlying schema bindings (result='allow', reason='') via a direct
	// RecordCerebroCredentialAudit call to surface the error.
	if _, err := s.Cerebro.RecordCerebroCredentialAudit(ctx, cerebrodb.RecordCerebroCredentialAuditParams{
		WorkspaceID:  row.WorkspaceID,
		CredentialID: row.ID,
		Action:       string(ActionReveal),
		ActorType:    actorType,
		ActorID:      actorID,
		Metadata:     []byte("{}"),
		Result:       AuditResultAllow,
		Reason:       "",
	}); err != nil {
		return cerebrodb.CerebroCredential{}, "", err
	}
	s.publish(EventCredentialRevealed, row.WorkspaceID, actorType, actorID, map[string]string{
		"credential_id": util.UUIDToString(row.ID),
	})
	return row, string(plaintext), nil
}

// ListAudit returns the most recent N audit rows for a credential. The
// caller is responsible for capping limit at a sensible value (the handler
// uses 100).
func (s *Service) ListAudit(ctx context.Context, workspaceID, credentialID pgtype.UUID, limit int32) ([]cerebrodb.CerebroCredentialAudit, error) {
	if _, err := s.Get(ctx, workspaceID, credentialID); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	return s.Cerebro.ListCerebroCredentialAudit(ctx, cerebrodb.ListCerebroCredentialAuditParams{
		CredentialID: credentialID,
		Limit:        limit,
	})
}

// CreateBinding ties a credential to a resource. resource_type is free-form
// on purpose (workspace, project, agent, repo, runtime, ...) — the rotation
// policy module is what decides which resources participate.
func (s *Service) CreateBinding(ctx context.Context, workspaceID, credentialID pgtype.UUID, resourceType string, resourceID pgtype.UUID, actorType string, actorID pgtype.UUID) (cerebrodb.CerebroCredentialBinding, error) {
	resourceType = strings.TrimSpace(resourceType)
	if resourceType == "" {
		return cerebrodb.CerebroCredentialBinding{}, ErrInvalidResourceType
	}
	if !resourceID.Valid {
		return cerebrodb.CerebroCredentialBinding{}, ErrInvalidResourceID
	}
	cred, err := s.Get(ctx, workspaceID, credentialID)
	if err != nil {
		return cerebrodb.CerebroCredentialBinding{}, err
	}
	if err := s.enforce(ctx, cred.WorkspaceID, cred.ID, Type(cred.Type), PermAttach, actorType, actorID, ""); err != nil {
		return cerebrodb.CerebroCredentialBinding{}, err
	}
	row, err := s.Cerebro.CreateCerebroCredentialBinding(ctx, cerebrodb.CreateCerebroCredentialBindingParams{
		CredentialID:  cred.ID,
		ResourceType:  resourceType,
		ResourceID:    resourceID,
		CreatedByType: actorType,
		CreatedByID:   actorID,
	})
	if err != nil {
		return cerebrodb.CerebroCredentialBinding{}, err
	}
	s.auditAllow(ctx, cred.WorkspaceID, cred.ID, ActionBind, actorType, actorID, "", mustJSON(map[string]any{
		"binding_id":    util.UUIDToString(row.ID),
		"resource_type": row.ResourceType,
		"resource_id":   util.UUIDToString(row.ResourceID),
	}))
	s.publish(EventCredentialBound, cred.WorkspaceID, actorType, actorID, bindingResponseFromModel(row))
	return row, nil
}

func (s *Service) ListBindings(ctx context.Context, workspaceID, credentialID pgtype.UUID) ([]cerebrodb.CerebroCredentialBinding, error) {
	if _, err := s.Get(ctx, workspaceID, credentialID); err != nil {
		return nil, err
	}
	return s.Cerebro.ListCerebroCredentialBindings(ctx, credentialID)
}

func (s *Service) DeleteBinding(ctx context.Context, workspaceID, credentialID, bindingID pgtype.UUID, actorType string, actorID pgtype.UUID) (cerebrodb.CerebroCredentialBinding, error) {
	cred, err := s.Get(ctx, workspaceID, credentialID)
	if err != nil {
		return cerebrodb.CerebroCredentialBinding{}, err
	}
	if err := s.enforce(ctx, cred.WorkspaceID, cred.ID, Type(cred.Type), PermAttach, actorType, actorID, ""); err != nil {
		return cerebrodb.CerebroCredentialBinding{}, err
	}
	binding, err := s.Cerebro.GetCerebroCredentialBinding(ctx, bindingID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return cerebrodb.CerebroCredentialBinding{}, ErrBindingNotFound
		}
		return cerebrodb.CerebroCredentialBinding{}, err
	}
	if !uuidEqual(binding.CredentialID, cred.ID) {
		return cerebrodb.CerebroCredentialBinding{}, ErrCredentialBindingMismatch
	}
	if err := s.Cerebro.DeleteCerebroCredentialBinding(ctx, binding.ID); err != nil {
		return cerebrodb.CerebroCredentialBinding{}, err
	}
	s.auditAllow(ctx, cred.WorkspaceID, cred.ID, ActionUnbind, actorType, actorID, "", mustJSON(map[string]any{
		"binding_id":    util.UUIDToString(binding.ID),
		"resource_type": binding.ResourceType,
		"resource_id":   util.UUIDToString(binding.ResourceID),
	}))
	s.publish(EventCredentialUnbound, cred.WorkspaceID, actorType, actorID, bindingResponseFromModel(binding))
	return binding, nil
}

// auditAllow writes the allow-side audit row for a completed action.
// We swallow the error silently because the action itself has already
// happened — a missing audit row is worse than a half-applied change.
// Reveal uses RecordCerebroCredentialAudit directly so the error is
// surfaced and the plaintext is not returned without a trail.
func (s *Service) auditAllow(ctx context.Context, workspaceID, credentialID pgtype.UUID, action Action, actorType string, actorID pgtype.UUID, reason string, metadata []byte) {
	if len(metadata) == 0 {
		metadata = []byte("{}")
	}
	_, _ = s.Cerebro.RecordCerebroCredentialAudit(ctx, cerebrodb.RecordCerebroCredentialAuditParams{
		WorkspaceID:  workspaceID,
		CredentialID: credentialID,
		Action:       string(action),
		ActorType:    actorType,
		ActorID:      actorID,
		Metadata:     metadata,
		Result:       AuditResultAllow,
		Reason:       reason,
	})
}

// auditDeny records a policy-denied attempt. action is the credential
// audit action enum value derived from the permission. We use a
// dedicated helper (instead of inlining at every call site) so the
// permission-to-action mapping lives in exactly one place and the
// schema's CHECK constraint stays satisfied.
func (s *Service) auditDeny(ctx context.Context, workspaceID, credentialID pgtype.UUID, perm Permission, actorType string, actorID pgtype.UUID, reason string) {
	_, _ = s.Cerebro.RecordCerebroCredentialAudit(ctx, cerebrodb.RecordCerebroCredentialAuditParams{
		WorkspaceID:  workspaceID,
		CredentialID: credentialID,
		Action:       string(permissionToAuditAction(perm)),
		ActorType:    actorType,
		ActorID:      actorID,
		Metadata:     []byte("{}"),
		Result:       AuditResultDeny,
		Reason:       reason,
	})
}

// permissionToAuditAction maps the JEH-1197 permission names to the
// schema's action enum values. read_redacted is the only one that
// doesn't have a 1:1 enum value — it maps to the new 'read' action
// added by migration 9025.
func permissionToAuditAction(p Permission) Action {
	switch p {
	case PermAttach:
		return ActionBind
	case PermReadRedacted:
		return ActionRead
	case PermReveal:
		return ActionReveal
	case PermRotate:
		return ActionRotate
	case PermRevoke:
		return ActionDelete
	}
	return ActionRead
}

func (s *Service) publish(eventType string, workspaceID pgtype.UUID, actorType string, actorID pgtype.UUID, payload any) {
	if s.Bus == nil {
		return
	}
	s.Bus.Publish(events.Event{
		Type:        eventType,
		WorkspaceID: util.UUIDToString(workspaceID),
		ActorType:   actorType,
		ActorID:     util.UUIDToString(actorID),
		Payload:     payload,
	})
}

func normalizeMetadata(raw json.RawMessage) ([]byte, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return []byte("{}"), nil
	}
	if !strings.HasPrefix(trimmed, "{") {
		return nil, ErrInvalidMetadata
	}
	var probe map[string]any
	if err := json.Unmarshal([]byte(trimmed), &probe); err != nil {
		return nil, ErrInvalidMetadata
	}
	return []byte(trimmed), nil
}

func mustJSON(v map[string]any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return b
}

func timeToTimestamptz(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}

func uuidEqual(a, b pgtype.UUID) bool {
	if !a.Valid || !b.Valid {
		return false
	}
	return a.Bytes == b.Bytes
}

func isUniqueViolation(err error) bool {
	const uniqueViolation = "23505"
	type pgErr interface {
		SQLState() string
	}
	var pe pgErr
	if errors.As(err, &pe) {
		return pe.SQLState() == uniqueViolation
	}
	return false
}
