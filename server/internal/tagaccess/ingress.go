package tagaccess

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"time"
)

const AuthorityEnvelopeSchemaVersion = 1

type AuthorityEnvelopeAuthentication struct {
	KeyID string `json:"keyId"`
	MAC   []byte `json:"mac"`
}

// AuthorityEnvelope is the authenticated, transport-neutral VIBES delivery
// accepted by Multica. It contains projection facts, never VIBES authority
// rules or a Multica-native Workspace/member mutation.
type AuthorityEnvelope struct {
	SchemaVersion          uint64                          `json:"schemaVersion"`
	DeliveryID             string                          `json:"deliveryId"`
	CorrelationID          string                          `json:"correlationId"`
	Delivery               ProjectionDelivery              `json:"delivery"`
	ConnectionCloseTargets []ConnectionCloseTarget         `json:"connectionCloseTargets"`
	Authentication         AuthorityEnvelopeAuthentication `json:"authentication"`
}

type canonicalProjection struct {
	EventID              string `json:"eventId"`
	VIBESUserID          string `json:"vibesUserId"`
	WorkspaceID          string `json:"workspaceId"`
	Role                 Role   `json:"role"`
	Status               Status `json:"status"`
	AccountEpoch         uint64 `json:"accountEpoch"`
	MembershipGeneration uint64 `json:"membershipGeneration"`
	AuthorityVersion     uint64 `json:"authorityVersion"`
}

type canonicalDelivery struct {
	Kind                     DeliveryKind          `json:"kind"`
	BaselineAuthorityVersion uint64                `json:"baselineAuthorityVersion"`
	AuthorityAssertionID     string                `json:"authorityAssertionId"`
	Projections              []canonicalProjection `json:"projections"`
}

type canonicalCloseTarget struct {
	Scope          ConnectionCloseScope `json:"scope"`
	WorkspaceID    string               `json:"workspaceId"`
	VIBESUserID    string               `json:"vibesUserId"`
	VIBESSessionID string               `json:"vibesSessionId"`
}

type canonicalAuthentication struct {
	KeyID string `json:"keyId"`
}

type canonicalEnvelope struct {
	SchemaVersion          uint64                   `json:"schemaVersion"`
	DeliveryID             string                   `json:"deliveryId"`
	CorrelationID          string                   `json:"correlationId"`
	Delivery               canonicalDelivery        `json:"delivery"`
	ConnectionCloseTargets []canonicalCloseTarget   `json:"connectionCloseTargets"`
	Authentication         *canonicalAuthentication `json:"authentication,omitempty"`
}

type DurableApplyReceipt struct {
	DeliveryID       string      `json:"deliveryId"`
	CorrelationID    string      `json:"correlationId"`
	WorkspaceID      string      `json:"workspaceId"`
	AuthorityVersion uint64      `json:"authorityVersion"`
	PayloadDigest    string      `json:"payloadDigest"`
	Result           ApplyResult `json:"result"`
}

type ConnectionCloseStatus string

const (
	ConnectionCloseNotRequired ConnectionCloseStatus = "not_required"
	ConnectionClosePending     ConnectionCloseStatus = "pending"
	ConnectionCloseCompleted   ConnectionCloseStatus = "completed"
)

type ConnectionCloseStage struct {
	Status      ConnectionCloseStatus `json:"status"`
	ReceiptID   string                `json:"receiptId,omitempty"`
	CompletedAt *time.Time            `json:"completedAt,omitempty"`
}

type ConnectionCloseScope string

const (
	ConnectionCloseSession          ConnectionCloseScope = "session"
	ConnectionCloseSessionWorkspace ConnectionCloseScope = "session_workspace"
	ConnectionCloseAccount          ConnectionCloseScope = "account"
	ConnectionCloseMembership       ConnectionCloseScope = "membership"
	ConnectionCloseWorkspace        ConnectionCloseScope = "workspace"
)

type ConnectionCloseTarget struct {
	Scope          ConnectionCloseScope `json:"scope"`
	WorkspaceID    string               `json:"workspaceId"`
	VIBESUserID    string               `json:"vibesUserId,omitempty"`
	VIBESSessionID string               `json:"vibesSessionId,omitempty"`
}

type ConnectionCloseCommand struct {
	Source                     ConnectionCloseSource   `json:"source"`
	DeliveryID                 string                  `json:"deliveryId"`
	CorrelationID              string                  `json:"correlationId"`
	WorkspaceID                string                  `json:"workspaceId,omitempty"`
	AuthorityVersion           uint64                  `json:"authorityVersion,omitempty"`
	IdentityRestrictionVersion uint64                  `json:"identityRestrictionVersion,omitempty"`
	SessionWorkspaceGeneration uint64                  `json:"sessionWorkspaceGeneration,omitempty"`
	TargetDigest               string                  `json:"targetDigest"`
	Targets                    []ConnectionCloseTarget `json:"targets"`
}

type ConnectionCloseSource string

const (
	ConnectionCloseWorkspaceProjection          ConnectionCloseSource = "workspace_projection"
	ConnectionCloseIdentityRestriction          ConnectionCloseSource = "identity_restriction"
	ConnectionCloseSessionWorkspaceSupersession ConnectionCloseSource = "session_workspace_supersession"
)

type ConnectionCloseReceipt struct {
	ReceiptID                  string                `json:"receiptId"`
	Source                     ConnectionCloseSource `json:"source"`
	DeliveryID                 string                `json:"deliveryId"`
	CorrelationID              string                `json:"correlationId"`
	WorkspaceID                string                `json:"workspaceId,omitempty"`
	AuthorityVersion           uint64                `json:"authorityVersion,omitempty"`
	IdentityRestrictionVersion uint64                `json:"identityRestrictionVersion,omitempty"`
	SessionWorkspaceGeneration uint64                `json:"sessionWorkspaceGeneration,omitempty"`
	TargetDigest               string                `json:"targetDigest"`
	CompletedAt                time.Time             `json:"completedAt"`
}

// ConnectionClosePort is implemented by #290. Implementations must be
// idempotent for DeliveryID plus TargetDigest and may return completed only
// after the exact targeted closes are durably recorded. #288 never fabricates
// that receipt.
type ConnectionClosePort interface {
	CloseConnections(context.Context, ConnectionCloseCommand) (ConnectionCloseReceipt, error)
}

type TwoStageReceipt struct {
	Apply           DurableApplyReceipt  `json:"apply"`
	ConnectionClose ConnectionCloseStage `json:"connectionClose"`
	Cleanup         CleanupStage         `json:"cleanup"`
}

type AuthorityIngress struct {
	gate        *Gate
	keys        map[string][]byte
	closePort   ConnectionClosePort
	cleanupPort CleanupPort
}

// AuthenticatedAccess constructs the production-safe pair: callers use Gate
// for grants/authorization and AuthorityIngress for projection mutation. The
// returned Gate has no alternate delivery verifier, so direct Apply calls fail
// closed instead of bypassing the authenticated envelope.
type AuthenticatedAccess struct {
	Gate                    *Gate
	Ingress                 *AuthorityIngress
	IdentityIngress         *IdentityRestrictionIngress
	SessionWorkspaceIngress *SessionWorkspaceSupersessionIngress
}

// AttachCleanupPort completes boot-time wiring after the Handler and its
// existing cleanup transaction are constructed. Routers attach at most once
// before serving requests; authority access remains usable without a port, but
// required cleanup then reports pending rather than fabricating completion.
func (a *AuthenticatedAccess) AttachCleanupPort(port CleanupPort) error {
	if a == nil || a.Ingress == nil || a.IdentityIngress == nil || !configuredDependency(port) {
		return errors.New("Tag authority cleanup port is invalid")
	}
	if a.Ingress.cleanupPort != nil || a.IdentityIngress.cleanupPort != nil {
		return errors.New("Tag authority cleanup port is already attached")
	}
	a.Ingress.cleanupPort = port
	a.IdentityIngress.cleanupPort = port
	return nil
}

func NewAuthenticatedAccess(adapter store, clock Clock, keys map[string][]byte, closePort ConnectionClosePort) (*AuthenticatedAccess, error) {
	if !configuredDependency(adapter) || !configuredDependency(clock) ||
		(closePort != nil && !configuredDependency(closePort)) || len(keys) == 0 {
		return nil, errors.New("Tag authority ingress requires store, clock, and authentication keys")
	}
	cloned := make(map[string][]byte, len(keys))
	for keyID, key := range keys {
		if !validStableID(keyID) || len(key) < sha256.Size {
			return nil, errors.New("invalid Tag authority ingress key")
		}
		cloned[keyID] = append([]byte(nil), key...)
	}
	gate := newGate(adapter, clock, nil)
	return &AuthenticatedAccess{
		Gate:                    gate,
		Ingress:                 &AuthorityIngress{gate: gate, keys: cloned, closePort: closePort},
		IdentityIngress:         &IdentityRestrictionIngress{store: adapter, keys: cloned, closePort: closePort},
		SessionWorkspaceIngress: &SessionWorkspaceSupersessionIngress{store: adapter, keys: cloned, closePort: closePort},
	}, nil
}

type dependencyConfiguration interface {
	configured() bool
}

func configuredDependency(dependency any) bool {
	if dependency == nil {
		return false
	}
	value := reflect.ValueOf(dependency)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if value.IsNil() {
			return false
		}
	}
	if configuration, ok := dependency.(dependencyConfiguration); ok {
		return configuration.configured()
	}
	return true
}

func (i *AuthorityIngress) Deliver(ctx context.Context, envelope AuthorityEnvelope) (TwoStageReceipt, error) {
	payload, err := CanonicalAuthorityEnvelope(envelope)
	if err != nil {
		return TwoStageReceipt{}, err
	}
	key, knownKey := i.keys[envelope.Authentication.KeyID]
	if !knownKey || !verifyAuthorityMAC(key, envelope.Authentication.MAC, payload) {
		return TwoStageReceipt{}, ErrUnverifiedDelivery
	}
	authorityPayload, err := canonicalAuthorityPayload(envelope)
	if err != nil {
		return TwoStageReceipt{}, err
	}
	digest := sha256.Sum256(authorityPayload)
	result, err := i.gate.applyVerifiedDelivery(ctx, envelope.Delivery, digest)
	if err != nil {
		return TwoStageReceipt{}, err
	}
	first := envelope.Delivery.Projections[0]
	closeStage := ConnectionCloseStage{Status: ConnectionCloseNotRequired}
	cleanupStage := CleanupStage{Status: CleanupNotRequired}
	if len(envelope.ConnectionCloseTargets) > 0 && (result == ApplyApplied || result == ApplyDuplicate) {
		closeStage = i.closeConnections(ctx, envelope, first)
	}
	if result == ApplyApplied || result == ApplyDuplicate {
		cleanupStage = i.cleanupWorkspace(ctx, envelope, digest)
	}
	return TwoStageReceipt{
		Apply: DurableApplyReceipt{
			DeliveryID:       envelope.DeliveryID,
			CorrelationID:    envelope.CorrelationID,
			WorkspaceID:      first.WorkspaceID,
			AuthorityVersion: first.AuthorityVersion,
			PayloadDigest:    hex.EncodeToString(digest[:]),
			Result:           result,
		},
		ConnectionClose: closeStage,
		Cleanup:         cleanupStage,
	}, nil
}

func verifyAuthorityMAC(key, observedMAC, canonical []byte) bool {
	if len(key) < sha256.Size || len(observedMAC) != sha256.Size {
		return false
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(canonical)
	return hmac.Equal(observedMAC, mac.Sum(nil))
}

func (i *AuthorityIngress) cleanupWorkspace(ctx context.Context, envelope AuthorityEnvelope, payloadDigest [32]byte) CleanupStage {
	targets := make([]CleanupTarget, 0, len(envelope.Delivery.Projections))
	for _, projection := range envelope.Delivery.Projections {
		if projection.Status == StatusRemoved || projection.Status == StatusDisabled {
			targets = append(targets, CleanupTarget{
				VIBESUserID: projection.VIBESUserID, MembershipGeneration: projection.MembershipGeneration,
				Status: projection.Status,
			})
		}
	}
	// Snapshot/reconcile deliveries can remove a member by omission. The
	// cleanup adapter derives those newly inactive rows from the just-committed
	// projection version, so an empty explicit target list is still meaningful.
	if len(targets) == 0 && envelope.Delivery.Kind == DeliveryIncremental {
		return CleanupStage{Status: CleanupNotRequired}
	}
	targets = normalizedCleanupTargets(targets)
	first := envelope.Delivery.Projections[0]
	command := CleanupCommand{
		Source: CleanupWorkspaceProjection, DeliveryID: envelope.DeliveryID, CorrelationID: envelope.CorrelationID,
		WorkspaceID: first.WorkspaceID, AuthorityVersion: first.AuthorityVersion,
		PayloadDigest: hex.EncodeToString(payloadDigest[:]), TargetDigest: cleanupTargetDigest(targets), Targets: targets,
	}
	stage := CleanupStage{Status: CleanupPending}
	if i.cleanupPort == nil {
		return stage
	}
	receipt, err := i.cleanupPort.Cleanup(ctx, command)
	if err != nil || !cleanupReceiptMatches(command, receipt) {
		return stage
	}
	return CleanupStage{Status: CleanupCompleted, ReceiptID: receipt.ReceiptID, CompletedAt: &receipt.CompletedAt}
}

func (i *AuthorityIngress) closeConnections(ctx context.Context, envelope AuthorityEnvelope, first ProjectionEvent) ConnectionCloseStage {
	stage := ConnectionCloseStage{Status: ConnectionClosePending}
	if i.closePort == nil {
		return stage
	}
	targets := normalizedConnectionCloseTargets(envelope.ConnectionCloseTargets)
	targetPayload, err := json.Marshal(targets)
	if err != nil {
		return stage
	}
	targetDigest := sha256.Sum256(targetPayload)
	command := ConnectionCloseCommand{
		Source:           ConnectionCloseWorkspaceProjection,
		DeliveryID:       envelope.DeliveryID,
		CorrelationID:    envelope.CorrelationID,
		WorkspaceID:      first.WorkspaceID,
		AuthorityVersion: first.AuthorityVersion,
		TargetDigest:     hex.EncodeToString(targetDigest[:]),
		Targets:          targets,
	}
	receipt, err := i.closePort.CloseConnections(ctx, command)
	if err != nil || !connectionCloseReceiptMatches(command, receipt) {
		return stage
	}
	return ConnectionCloseStage{
		Status: ConnectionCloseCompleted, ReceiptID: receipt.ReceiptID, CompletedAt: &receipt.CompletedAt,
	}
}

func connectionCloseReceiptMatches(command ConnectionCloseCommand, receipt ConnectionCloseReceipt) bool {
	return validStableID(receipt.ReceiptID) && receipt.Source == command.Source &&
		receipt.DeliveryID == command.DeliveryID && receipt.CorrelationID == command.CorrelationID &&
		receipt.WorkspaceID == command.WorkspaceID && receipt.AuthorityVersion == command.AuthorityVersion &&
		receipt.IdentityRestrictionVersion == command.IdentityRestrictionVersion &&
		receipt.SessionWorkspaceGeneration == command.SessionWorkspaceGeneration &&
		receipt.TargetDigest == command.TargetDigest && !receipt.CompletedAt.IsZero()
}

// CanonicalAuthorityEnvelope returns the exact bytes VIBES authenticates and
// Multica verifies. Authentication.MAC is deliberately excluded.
func CanonicalAuthorityEnvelope(envelope AuthorityEnvelope) ([]byte, error) {
	return canonicalEnvelopeBytes(envelope, true)
}

func canonicalAuthorityPayload(envelope AuthorityEnvelope) ([]byte, error) {
	return canonicalEnvelopeBytes(envelope, false)
}

func canonicalEnvelopeBytes(envelope AuthorityEnvelope, includeAuthentication bool) ([]byte, error) {
	if envelope.SchemaVersion != AuthorityEnvelopeSchemaVersion ||
		!validStableID(envelope.DeliveryID) ||
		!validStableID(envelope.CorrelationID) ||
		!validStableID(envelope.Authentication.KeyID) ||
		!validDelivery(envelope.Delivery) {
		return nil, ErrInvalidProjection
	}
	first := envelope.Delivery.Projections[0]
	closeTargets := normalizedConnectionCloseTargets(envelope.ConnectionCloseTargets)
	if !validConnectionCloseTargets(closeTargets, first.WorkspaceID) {
		return nil, ErrInvalidProjection
	}
	normalized := normalizedDelivery(envelope.Delivery)
	projections := make([]canonicalProjection, 0, len(normalized.Projections))
	for _, projection := range normalized.Projections {
		projections = append(projections, canonicalProjection{
			EventID: projection.EventID, VIBESUserID: projection.VIBESUserID, WorkspaceID: projection.WorkspaceID,
			Role: projection.Role, Status: projection.Status, AccountEpoch: projection.AccountEpoch,
			MembershipGeneration: projection.MembershipGeneration, AuthorityVersion: projection.AuthorityVersion,
		})
	}
	canonicalTargets := make([]canonicalCloseTarget, 0, len(closeTargets))
	for _, target := range closeTargets {
		canonicalTargets = append(canonicalTargets, canonicalCloseTarget(target))
	}
	canonical := canonicalEnvelope{
		SchemaVersion: envelope.SchemaVersion,
		DeliveryID:    envelope.DeliveryID,
		CorrelationID: envelope.CorrelationID,
		Delivery: canonicalDelivery{
			Kind: normalized.Kind, BaselineAuthorityVersion: normalized.BaselineAuthorityVersion,
			AuthorityAssertionID: normalized.AuthorityAssertionID, Projections: projections,
		},
		ConnectionCloseTargets: canonicalTargets,
	}
	if includeAuthentication {
		canonical.Authentication = &canonicalAuthentication{KeyID: envelope.Authentication.KeyID}
	}
	payload, err := json.Marshal(canonical)
	if err != nil {
		return nil, ErrInvalidProjection
	}
	return payload, nil
}

func normalizedConnectionCloseTargets(targets []ConnectionCloseTarget) []ConnectionCloseTarget {
	normalized := append([]ConnectionCloseTarget(nil), targets...)
	sort.Slice(normalized, func(left, right int) bool {
		l, r := normalized[left], normalized[right]
		if l.Scope != r.Scope {
			return l.Scope < r.Scope
		}
		if l.WorkspaceID != r.WorkspaceID {
			return l.WorkspaceID < r.WorkspaceID
		}
		if l.VIBESUserID != r.VIBESUserID {
			return l.VIBESUserID < r.VIBESUserID
		}
		return l.VIBESSessionID < r.VIBESSessionID
	})
	return normalized
}

func validConnectionCloseTargets(targets []ConnectionCloseTarget, workspaceID string) bool {
	for index, target := range targets {
		if target.WorkspaceID != workspaceID || !validStableID(target.WorkspaceID) {
			return false
		}
		switch target.Scope {
		case ConnectionCloseSession:
			if !validStableID(target.VIBESUserID) || !validStableID(target.VIBESSessionID) {
				return false
			}
		case ConnectionCloseAccount, ConnectionCloseMembership:
			if !validStableID(target.VIBESUserID) || target.VIBESSessionID != "" {
				return false
			}
		case ConnectionCloseWorkspace:
			if target.VIBESUserID != "" || target.VIBESSessionID != "" {
				return false
			}
		default:
			return false
		}
		if index > 0 && target == targets[index-1] {
			return false
		}
	}
	return true
}
