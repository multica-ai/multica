package tagaccess

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"
)

type CleanupSource string

const (
	CleanupWorkspaceProjection CleanupSource = "workspace_projection"
	CleanupIdentityRestriction CleanupSource = "identity_restriction"
)

type CleanupTarget struct {
	VIBESUserID          string `json:"vibesUserId"`
	MembershipGeneration uint64 `json:"membershipGeneration"`
	Status               Status `json:"status"`
}

type CleanupCommand struct {
	Source                     CleanupSource   `json:"source"`
	DeliveryID                 string          `json:"deliveryId"`
	CorrelationID              string          `json:"correlationId"`
	WorkspaceID                string          `json:"workspaceId,omitempty"`
	VIBESUserID                string          `json:"vibesUserId,omitempty"`
	AuthorityVersion           uint64          `json:"authorityVersion,omitempty"`
	IdentityRestrictionVersion uint64          `json:"identityRestrictionVersion,omitempty"`
	AccountEpoch               uint64          `json:"accountEpoch,omitempty"`
	PayloadDigest              string          `json:"payloadDigest"`
	TargetDigest               string          `json:"targetDigest"`
	Targets                    []CleanupTarget `json:"targets,omitempty"`
}

type CleanupReceipt struct {
	ReceiptID                  string        `json:"receiptId"`
	Source                     CleanupSource `json:"source"`
	DeliveryID                 string        `json:"deliveryId"`
	CorrelationID              string        `json:"correlationId"`
	WorkspaceID                string        `json:"workspaceId,omitempty"`
	VIBESUserID                string        `json:"vibesUserId,omitempty"`
	AuthorityVersion           uint64        `json:"authorityVersion,omitempty"`
	IdentityRestrictionVersion uint64        `json:"identityRestrictionVersion,omitempty"`
	AccountEpoch               uint64        `json:"accountEpoch,omitempty"`
	PayloadDigest              string        `json:"payloadDigest"`
	TargetDigest               string        `json:"targetDigest"`
	CompletedAt                time.Time     `json:"completedAt"`
}

// CleanupPort is the one execution-side consequence seam for restrictive
// VIBES authority deliveries. Implementations are idempotent for Source plus
// DeliveryID and may return a receipt only after the exact command is durable.
type CleanupPort interface {
	Cleanup(context.Context, CleanupCommand) (CleanupReceipt, error)
}

type CleanupStatus string

const (
	CleanupNotRequired CleanupStatus = "not_required"
	CleanupPending     CleanupStatus = "pending"
	CleanupCompleted   CleanupStatus = "completed"
)

type CleanupStage struct {
	Status      CleanupStatus `json:"status"`
	ReceiptID   string        `json:"receiptId,omitempty"`
	CompletedAt *time.Time    `json:"completedAt,omitempty"`
}

func normalizedCleanupTargets(targets []CleanupTarget) []CleanupTarget {
	normalized := append([]CleanupTarget(nil), targets...)
	sort.Slice(normalized, func(left, right int) bool {
		return normalized[left].VIBESUserID < normalized[right].VIBESUserID
	})
	return normalized
}

func cleanupTargetDigest(targets []CleanupTarget) string {
	payload, err := json.Marshal(normalizedCleanupTargets(targets))
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func cleanupReceiptMatches(command CleanupCommand, receipt CleanupReceipt) bool {
	return validStableID(receipt.ReceiptID) && receipt.Source == command.Source &&
		receipt.DeliveryID == command.DeliveryID && receipt.CorrelationID == command.CorrelationID &&
		receipt.WorkspaceID == command.WorkspaceID && receipt.VIBESUserID == command.VIBESUserID &&
		receipt.AuthorityVersion == command.AuthorityVersion &&
		receipt.IdentityRestrictionVersion == command.IdentityRestrictionVersion &&
		receipt.AccountEpoch == command.AccountEpoch && receipt.PayloadDigest == command.PayloadDigest &&
		receipt.TargetDigest == command.TargetDigest && !receipt.CompletedAt.IsZero()
}
