// Package service: Memory Docket (Plan v1.2 T6). Owner: ALL-16.
//
// This is the SOLE policy engine for docket writes, dedupe, compression,
// injection selection, withdrawal, TTL expiry, cleanup, and replay. ALL-18
// consumes MemoryAttachment.selected_item_refs and never reselects, merges,
// drops, or reorders them; it may only defensively reject an invalid version,
// expired attachment, withdrawn ref, or scope mismatch. No second policy
// engine exists (V4-6).
package service

import (
	"errors"
	"time"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

// MemoryDocket errors.
var (
	ErrDocketScopeInvalid     = errors.New("memoryhub: docket scope invalid")
	ErrDocketItemNotFound     = errors.New("memoryhub: docket item not found")
	ErrDocketItemStateMismatch = errors.New("memoryhub: docket item state mismatch")
	ErrDocketExpired          = errors.New("memoryhub: docket expired")
	ErrDocketWithdrawn        = errors.New("memoryhub: docket item withdrawn")
)

// MemoryItemState is the frozen five-state item enum.
type MemoryItemState string

const (
	MemoryItemActive     MemoryItemState = "active"
	MemoryItemWithdrawn  MemoryItemState = "withdrawn"
	MemoryItemExpired    MemoryItemState = "expired"
	MemoryItemSuperseded MemoryItemState = "superseded"
	MemoryItemPurged     MemoryItemState = "purged"
)

// MemoryDocketItem is the durable docket item view used by the policy engine.
type MemoryDocketItem struct {
	ID           string
	State        MemoryItemState
	Kind         string
	Summary      string
	SourceRef    string
	EvidenceRef  string
	Priority     int
	DedupeKey    string
	ExpiresAt    time.Time
	WithdrawnAt  time.Time
	CreatedAt    time.Time
	SHA256       string
}

// MemoryDocketSelection is the selection input for building an attachment.
type MemoryDocketSelection struct {
	// MemoryPolicy is the frozen execution memory policy.
	MemoryPolicy string // "required" | "optional"
	// MaxItems caps the selected refs after compression.
	MaxItems int
	// Now is the evaluation clock.
	Now time.Time
	// Items are the durable docket items (already scoped to the subject).
	Items []MemoryDocketItem
}

// MemoryAttachmentRef is one selected item reference (the wire shape).
type MemoryAttachmentRef struct {
	ItemID      string
	Kind        string
	SourceRef   string
	EvidenceRef *string
	SHA256      string
	ExpiresAt   *string
	WithdrawnAt *string
}

// SelectionResult is the injection-selection outcome.
type SelectionResult struct {
	// Selected are the ordered, filtered, deduplicated refs.
	Selected []MemoryAttachmentRef
	// WithdrawnRefs are refs excluded because the item was withdrawn.
	WithdrawnRefs []string
	// ExpiredRefs are refs excluded because the item expired.
	ExpiredRefs []string
	// Compressed reports that dedupe/compression reduced the set.
	Compressed bool
}

// SelectMemoryItems applies the frozen injection-selection policy:
//   - scope/state filter: only active items are selectable;
//   - TTL/withdrawal filter: expired and withdrawn items are excluded;
//   - dedupe: one ref per dedupe key, keeping the highest priority;
//   - compression: priority-descending, capped at MaxItems;
//   - ordering: priority desc, then created_at asc (deterministic).
//
// This is the ONLY selection policy; ALL-18 consumes the result untouched.
func SelectMemoryItems(sel MemoryDocketSelection) SelectionResult {
	var res SelectionResult
	seenDedupe := map[string]bool{}
	byID := map[string]MemoryDocketItem{}
	for _, it := range sel.Items {
		byID[it.ID] = it
	}
	// Filter + dedupe.
	filtered := make([]MemoryDocketItem, 0, len(sel.Items))
	for _, it := range sel.Items {
		if it.State == MemoryItemWithdrawn {
			res.WithdrawnRefs = append(res.WithdrawnRefs, it.ID)
			continue
		}
		if it.State == MemoryItemExpired {
			res.ExpiredRefs = append(res.ExpiredRefs, it.ID)
			continue
		}
		if it.State != MemoryItemActive {
			continue
		}
		if !it.ExpiresAt.IsZero() && !it.ExpiresAt.After(sel.Now) {
			res.ExpiredRefs = append(res.ExpiredRefs, it.ID)
			continue
		}
		if !it.WithdrawnAt.IsZero() {
			res.WithdrawnRefs = append(res.WithdrawnRefs, it.ID)
			continue
		}
		if it.DedupeKey != "" {
			if seenDedupe[it.DedupeKey] {
				res.Compressed = true
				continue
			}
			seenDedupe[it.DedupeKey] = true
		}
		filtered = append(filtered, it)
	}
	// Sort: priority desc, then created_at asc.
	sortItems(filtered)
	// Cap at MaxItems.
	if sel.MaxItems > 0 && len(filtered) > sel.MaxItems {
		filtered = filtered[:sel.MaxItems]
		res.Compressed = true
	}
	for _, it := range filtered {
		ref := MemoryAttachmentRef{
			ItemID:    it.ID,
			Kind:      it.Kind,
			SourceRef: it.SourceRef,
			SHA256:    it.SHA256,
		}
		if it.EvidenceRef != "" {
			ref.EvidenceRef = &it.EvidenceRef
		}
		if !it.ExpiresAt.IsZero() {
			s := it.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z07:00")
			ref.ExpiresAt = &s
		}
		if !it.WithdrawnAt.IsZero() {
			s := it.WithdrawnAt.UTC().Format("2006-01-02T15:04:05Z07:00")
			ref.WithdrawnAt = &s
		}
		res.Selected = append(res.Selected, ref)
	}
	return res
}

func sortItems(items []MemoryDocketItem) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0; j-- {
			less := items[j].Priority > items[j-1].Priority ||
				(items[j].Priority == items[j-1].Priority && items[j].CreatedAt.Before(items[j-1].CreatedAt))
			if !less {
				break
			}
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}

// WireMemoryAttachment builds the typed protocol.MemoryAttachment from a
// selection. It carries only selected refs; no raw memory content or secret.
func WireMemoryAttachment(attachmentRef, executionID, runID, docketID string, docketRevision int, scopeKind string, scopeID *string, subjectType, subjectID, policyVersion string, selected []MemoryAttachmentRef, now time.Time, expiresAt *time.Time) protocol.MemoryAttachment {
	itemRefs := make([]protocol.MemoryAttachmentItemRef, 0, len(selected))
	for _, ref := range selected {
		itemRefs = append(itemRefs, protocol.MemoryAttachmentItemRef{
			SchemaVersion: 1,
			ItemID:        ref.ItemID,
			Kind:          ref.Kind,
			SourceRef:     ref.SourceRef,
			EvidenceRef:   ref.EvidenceRef,
			SHA256:        ref.SHA256,
			ExpiresAt:     ref.ExpiresAt,
			WithdrawnAt:   ref.WithdrawnAt,
		})
	}
	var exp *string
	if expiresAt != nil {
		s := expiresAt.UTC().Format("2006-01-02T15:04:05Z07:00")
		exp = &s
	}
	return protocol.MemoryAttachment{
		SchemaVersion:    1,
		AttachmentRef:    attachmentRef,
		ExecutionID:      executionID,
		RunID:            runID,
		DocketID:         docketID,
		DocketRevision:   docketRevision,
		ScopeKind:        scopeKind,
		ScopeID:          scopeID,
		SubjectType:      subjectType,
		SubjectID:        subjectID,
		MemoryPolicy:     "required",
		PolicyVersion:    policyVersion,
		SelectedItemRefs: itemRefs,
		IssuedAt:         now.UTC().Format("2006-01-02T15:04:05Z07:00"),
		ExpiresAt:        exp,
	}
}
