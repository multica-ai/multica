package tagaccess

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

type IdentityRestrictionEnvelope struct {
	SchemaVersion  uint64                          `json:"schemaVersion"`
	Delivery       IdentityRestrictionDelivery     `json:"delivery"`
	Authentication AuthorityEnvelopeAuthentication `json:"authentication"`
}

type IdentityRestrictionIngress struct {
	store       store
	keys        map[string][]byte
	closePort   ConnectionClosePort
	cleanupPort CleanupPort
}

type IdentityApplyReceipt struct {
	DeliveryID                 string      `json:"deliveryId"`
	CorrelationID              string      `json:"correlationId"`
	VIBESUserID                string      `json:"vibesUserId"`
	IdentityRestrictionVersion uint64      `json:"identityRestrictionVersion"`
	AccountEpoch               uint64      `json:"accountEpoch"`
	PayloadDigest              string      `json:"payloadDigest"`
	Result                     ApplyResult `json:"result"`
}

type IdentityTwoStageReceipt struct {
	Apply           IdentityApplyReceipt `json:"apply"`
	ConnectionClose ConnectionCloseStage `json:"connectionClose"`
	Cleanup         CleanupStage         `json:"cleanup"`
}

type canonicalIdentityCloseTarget struct {
	Scope          ConnectionCloseScope `json:"scope"`
	VIBESUserID    string               `json:"vibesUserId"`
	VIBESSessionID string               `json:"vibesSessionId"`
}

type canonicalIdentityDelivery struct {
	Kind                       IdentityRestrictionKind      `json:"kind"`
	EventID                    string                       `json:"eventId"`
	CorrelationID              string                       `json:"correlationId"`
	IdempotencyKey             string                       `json:"idempotencyKey"`
	VIBESUserID                string                       `json:"vibesUserId"`
	VIBESSessionID             string                       `json:"vibesSessionId"`
	AccountEpoch               uint64                       `json:"accountEpoch"`
	IdentityRestrictionVersion uint64                       `json:"identityRestrictionVersion"`
	CloseTarget                canonicalIdentityCloseTarget `json:"closeTarget"`
}

type canonicalIdentityEnvelope struct {
	SchemaVersion  uint64                    `json:"schemaVersion"`
	Delivery       canonicalIdentityDelivery `json:"delivery"`
	Authentication *canonicalAuthentication  `json:"authentication,omitempty"`
}

func (i *IdentityRestrictionIngress) Deliver(ctx context.Context, envelope IdentityRestrictionEnvelope) (IdentityTwoStageReceipt, error) {
	payload, err := CanonicalIdentityRestrictionEnvelope(envelope)
	if err != nil {
		return IdentityTwoStageReceipt{}, err
	}
	key, known := i.keys[envelope.Authentication.KeyID]
	if !known || !verifyAuthorityMAC(key, envelope.Authentication.MAC, payload) {
		return IdentityTwoStageReceipt{}, ErrUnverifiedDelivery
	}
	authorityPayload, err := canonicalIdentityEnvelopeBytes(envelope, false)
	if err != nil {
		return IdentityTwoStageReceipt{}, err
	}
	digest := sha256.Sum256(authorityPayload)
	result, err := i.store.applyIdentityRestriction(ctx, envelope.Delivery, digest)
	if err != nil {
		return IdentityTwoStageReceipt{}, err
	}
	closeStage := ConnectionCloseStage{Status: ConnectionCloseNotRequired}
	cleanupStage := CleanupStage{Status: CleanupNotRequired}
	if result == ApplyApplied || result == ApplyDuplicate {
		closeStage = i.closeIdentityConnections(ctx, envelope.Delivery)
		if envelope.Delivery.Kind == IdentityRestrictionAccountBan {
			cleanupStage = i.cleanupIdentity(ctx, envelope.Delivery, digest)
		}
	}
	return IdentityTwoStageReceipt{
		Apply: IdentityApplyReceipt{
			DeliveryID: envelope.Delivery.EventID, CorrelationID: envelope.Delivery.CorrelationID,
			VIBESUserID: envelope.Delivery.VIBESUserID, IdentityRestrictionVersion: envelope.Delivery.IdentityRestrictionVersion,
			AccountEpoch: envelope.Delivery.AccountEpoch, PayloadDigest: hex.EncodeToString(digest[:]), Result: result,
		},
		ConnectionClose: closeStage,
		Cleanup:         cleanupStage,
	}, nil
}

func (i *IdentityRestrictionIngress) cleanupIdentity(ctx context.Context, delivery IdentityRestrictionDelivery, payloadDigest [32]byte) CleanupStage {
	command := CleanupCommand{
		Source: CleanupIdentityRestriction, DeliveryID: delivery.EventID, CorrelationID: delivery.CorrelationID,
		VIBESUserID: delivery.VIBESUserID, IdentityRestrictionVersion: delivery.IdentityRestrictionVersion,
		AccountEpoch: delivery.AccountEpoch, PayloadDigest: hex.EncodeToString(payloadDigest[:]),
		TargetDigest: cleanupTargetDigest(nil),
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

func (i *IdentityRestrictionIngress) closeIdentityConnections(ctx context.Context, delivery IdentityRestrictionDelivery) ConnectionCloseStage {
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
		Source:     ConnectionCloseIdentityRestriction,
		DeliveryID: delivery.EventID, CorrelationID: delivery.CorrelationID,
		IdentityRestrictionVersion: delivery.IdentityRestrictionVersion,
		TargetDigest:               hex.EncodeToString(digest[:]), Targets: targets,
	}
	receipt, err := i.closePort.CloseConnections(ctx, command)
	if err != nil || !connectionCloseReceiptMatches(command, receipt) {
		return stage
	}
	return ConnectionCloseStage{Status: ConnectionCloseCompleted, ReceiptID: receipt.ReceiptID, CompletedAt: &receipt.CompletedAt}
}

func CanonicalIdentityRestrictionEnvelope(envelope IdentityRestrictionEnvelope) ([]byte, error) {
	return canonicalIdentityEnvelopeBytes(envelope, true)
}

func canonicalIdentityEnvelopeBytes(envelope IdentityRestrictionEnvelope, includeAuthentication bool) ([]byte, error) {
	if envelope.SchemaVersion != AuthorityEnvelopeSchemaVersion || !validStableID(envelope.Authentication.KeyID) ||
		!validIdentityRestrictionDelivery(envelope.Delivery) {
		return nil, ErrInvalidProjection
	}
	delivery := envelope.Delivery
	canonical := canonicalIdentityEnvelope{
		SchemaVersion: envelope.SchemaVersion,
		Delivery: canonicalIdentityDelivery{
			Kind: delivery.Kind, EventID: delivery.EventID, CorrelationID: delivery.CorrelationID,
			IdempotencyKey: delivery.IdempotencyKey, VIBESUserID: delivery.VIBESUserID, VIBESSessionID: delivery.VIBESSessionID,
			AccountEpoch: delivery.AccountEpoch, IdentityRestrictionVersion: delivery.IdentityRestrictionVersion,
			CloseTarget: canonicalIdentityCloseTarget{
				Scope: delivery.CloseTarget.Scope, VIBESUserID: delivery.CloseTarget.VIBESUserID,
				VIBESSessionID: delivery.CloseTarget.VIBESSessionID,
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
