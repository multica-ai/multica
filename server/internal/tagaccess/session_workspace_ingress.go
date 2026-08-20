package tagaccess

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

type SessionWorkspaceSupersededEnvelope struct {
	SchemaVersion  uint64                             `json:"schemaVersion"`
	Delivery       SessionWorkspaceSupersededDelivery `json:"delivery"`
	Authentication AuthorityEnvelopeAuthentication    `json:"authentication"`
}

type SessionWorkspaceSupersessionIngress struct {
	store     store
	keys      map[string][]byte
	closePort ConnectionClosePort
}

type SessionWorkspaceApplyReceipt struct {
	DeliveryID                 string      `json:"deliveryId"`
	CorrelationID              string      `json:"correlationId"`
	IdempotencyKey             string      `json:"idempotencyKey"`
	VIBESUserID                string      `json:"vibesUserId"`
	VIBESSessionID             string      `json:"vibesSessionId"`
	PreviousWorkspaceID        string      `json:"previousWorkspaceId"`
	NewWorkspaceID             string      `json:"newWorkspaceId"`
	SessionWorkspaceGeneration uint64      `json:"sessionWorkspaceGeneration"`
	AccountEpoch               uint64      `json:"accountEpoch"`
	IdentityRestrictionVersion uint64      `json:"identityRestrictionVersion"`
	AuthorityVersion           uint64      `json:"authorityVersion"`
	MembershipGeneration       uint64      `json:"membershipGeneration"`
	PayloadDigest              string      `json:"payloadDigest"`
	Result                     ApplyResult `json:"result"`
}

type SessionWorkspaceTwoStageReceipt struct {
	Apply           SessionWorkspaceApplyReceipt `json:"apply"`
	ConnectionClose ConnectionCloseStage         `json:"connectionClose"`
}

type canonicalSessionWorkspaceDelivery struct {
	Kind                       SessionWorkspaceSupersessionKind     `json:"kind"`
	EventID                    string                               `json:"eventId"`
	DeliveryID                 string                               `json:"deliveryId"`
	CorrelationID              string                               `json:"correlationId"`
	IdempotencyKey             string                               `json:"idempotencyKey"`
	VIBESUserID                string                               `json:"vibesUserId"`
	VIBESSessionID             string                               `json:"vibesSessionId"`
	PreviousWorkspaceID        string                               `json:"previousWorkspaceId"`
	NewWorkspaceID             string                               `json:"newWorkspaceId"`
	SessionWorkspaceGeneration uint64                               `json:"sessionWorkspaceGeneration"`
	AccountEpoch               uint64                               `json:"accountEpoch"`
	IdentityRestrictionVersion uint64                               `json:"identityRestrictionVersion"`
	AuthorityVersion           uint64                               `json:"authorityVersion"`
	MembershipGeneration       uint64                               `json:"membershipGeneration"`
	CloseTarget                canonicalSessionWorkspaceCloseTarget `json:"closeTarget"`
}

type canonicalSessionWorkspaceCloseTarget struct {
	Scope          ConnectionCloseScope `json:"scope"`
	VIBESUserID    string               `json:"vibesUserId"`
	VIBESSessionID string               `json:"vibesSessionId"`
	WorkspaceID    string               `json:"workspaceId"`
}

type canonicalSessionWorkspaceEnvelope struct {
	SchemaVersion  uint64                            `json:"schemaVersion"`
	Delivery       canonicalSessionWorkspaceDelivery `json:"delivery"`
	Authentication *canonicalAuthentication          `json:"authentication,omitempty"`
}

func (i *SessionWorkspaceSupersessionIngress) Deliver(ctx context.Context, envelope SessionWorkspaceSupersededEnvelope) (SessionWorkspaceTwoStageReceipt, error) {
	authenticated, err := CanonicalSessionWorkspaceSupersessionEnvelope(envelope)
	if err != nil {
		return SessionWorkspaceTwoStageReceipt{}, err
	}
	key, known := i.keys[envelope.Authentication.KeyID]
	if !known || !verifyAuthorityMAC(key, envelope.Authentication.MAC, authenticated) {
		return SessionWorkspaceTwoStageReceipt{}, ErrUnverifiedDelivery
	}
	unsigned, err := CanonicalSessionWorkspaceSupersessionPayload(envelope)
	if err != nil {
		return SessionWorkspaceTwoStageReceipt{}, err
	}
	digest := sha256.Sum256(unsigned)
	result, err := i.store.applySessionWorkspaceSupersession(ctx, envelope.Delivery, digest)
	if err != nil {
		return SessionWorkspaceTwoStageReceipt{}, err
	}
	closeStage := ConnectionCloseStage{Status: ConnectionCloseNotRequired}
	if result == ApplyApplied || result == ApplyDuplicate || result == ApplyStale {
		closeStage = i.closePreviousWorkspace(ctx, envelope.Delivery)
	}
	delivery := envelope.Delivery
	return SessionWorkspaceTwoStageReceipt{
		Apply: SessionWorkspaceApplyReceipt{
			DeliveryID: delivery.DeliveryID, CorrelationID: delivery.CorrelationID, IdempotencyKey: delivery.IdempotencyKey,
			VIBESUserID: delivery.VIBESUserID, VIBESSessionID: delivery.VIBESSessionID,
			PreviousWorkspaceID: delivery.PreviousWorkspaceID, NewWorkspaceID: delivery.NewWorkspaceID,
			SessionWorkspaceGeneration: delivery.SessionWorkspaceGeneration, AccountEpoch: delivery.AccountEpoch,
			IdentityRestrictionVersion: delivery.IdentityRestrictionVersion, AuthorityVersion: delivery.AuthorityVersion,
			MembershipGeneration: delivery.MembershipGeneration, PayloadDigest: hex.EncodeToString(digest[:]), Result: result,
		},
		ConnectionClose: closeStage,
	}, nil
}

func (i *SessionWorkspaceSupersessionIngress) closePreviousWorkspace(ctx context.Context, delivery SessionWorkspaceSupersededDelivery) ConnectionCloseStage {
	stage := ConnectionCloseStage{Status: ConnectionClosePending}
	if i.closePort == nil {
		return stage
	}
	targets := []ConnectionCloseTarget{delivery.CloseTarget}
	payload, err := json.Marshal(targets)
	if err != nil {
		return stage
	}
	digest := sha256.Sum256(payload)
	command := ConnectionCloseCommand{
		Source: ConnectionCloseSessionWorkspaceSupersession, DeliveryID: delivery.DeliveryID,
		CorrelationID: delivery.CorrelationID, WorkspaceID: delivery.PreviousWorkspaceID,
		AuthorityVersion: delivery.AuthorityVersion, IdentityRestrictionVersion: delivery.IdentityRestrictionVersion,
		SessionWorkspaceGeneration: delivery.SessionWorkspaceGeneration,
		TargetDigest:               hex.EncodeToString(digest[:]), Targets: targets,
	}
	receipt, err := i.closePort.CloseConnections(ctx, command)
	if err != nil || !connectionCloseReceiptMatches(command, receipt) {
		return stage
	}
	return ConnectionCloseStage{Status: ConnectionCloseCompleted, ReceiptID: receipt.ReceiptID, CompletedAt: &receipt.CompletedAt}
}

func CanonicalSessionWorkspaceSupersessionEnvelope(envelope SessionWorkspaceSupersededEnvelope) ([]byte, error) {
	return canonicalSessionWorkspaceEnvelopeBytes(envelope, true)
}

func CanonicalSessionWorkspaceSupersessionPayload(envelope SessionWorkspaceSupersededEnvelope) ([]byte, error) {
	return canonicalSessionWorkspaceEnvelopeBytes(envelope, false)
}

func canonicalSessionWorkspaceEnvelopeBytes(envelope SessionWorkspaceSupersededEnvelope, includeAuthentication bool) ([]byte, error) {
	if envelope.SchemaVersion != AuthorityEnvelopeSchemaVersion || (includeAuthentication && !validStableID(envelope.Authentication.KeyID)) ||
		!validSessionWorkspaceSupersession(envelope.Delivery) {
		return nil, ErrInvalidProjection
	}
	delivery := envelope.Delivery
	canonical := canonicalSessionWorkspaceEnvelope{
		SchemaVersion: envelope.SchemaVersion,
		Delivery: canonicalSessionWorkspaceDelivery{
			Kind: delivery.Kind, EventID: delivery.EventID, DeliveryID: delivery.DeliveryID,
			CorrelationID: delivery.CorrelationID, IdempotencyKey: delivery.IdempotencyKey,
			VIBESUserID: delivery.VIBESUserID, VIBESSessionID: delivery.VIBESSessionID,
			PreviousWorkspaceID: delivery.PreviousWorkspaceID, NewWorkspaceID: delivery.NewWorkspaceID,
			SessionWorkspaceGeneration: delivery.SessionWorkspaceGeneration, AccountEpoch: delivery.AccountEpoch,
			IdentityRestrictionVersion: delivery.IdentityRestrictionVersion, AuthorityVersion: delivery.AuthorityVersion,
			MembershipGeneration: delivery.MembershipGeneration,
			CloseTarget: canonicalSessionWorkspaceCloseTarget{
				Scope: delivery.CloseTarget.Scope, VIBESUserID: delivery.CloseTarget.VIBESUserID,
				VIBESSessionID: delivery.CloseTarget.VIBESSessionID, WorkspaceID: delivery.CloseTarget.WorkspaceID,
			},
		},
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
