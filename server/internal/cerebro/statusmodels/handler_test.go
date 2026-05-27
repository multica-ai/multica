package statusmodels

import (
	"encoding/json"
	"testing"
)

func threeValidStatuses() []statusEntry {
	return []statusEntry{
		{Key: "plan", Label: "Plan", Color: "#8b5cf6", BaseStatus: "todo"},
		{Key: "i_gang", Label: "I gang", Color: "#f59e0b", BaseStatus: "in_progress"},
		{Key: "faerdig", Label: "Færdig", Color: "#10b981", BaseStatus: "done"},
	}
}

func TestValidateWriteRequest_RequiresName(t *testing.T) {
	err := validateWriteRequest(writeStatusModelRequest{Statuses: threeValidStatuses()})
	if err == nil {
		t.Fatal("empty name must be rejected")
	}
}

func TestValidateWriteRequest_RequiresMinStatuses(t *testing.T) {
	req := writeStatusModelRequest{
		Name: "Plan-først",
		Statuses: []statusEntry{
			{Key: "plan", Label: "Plan", BaseStatus: "todo"},
			{Key: "done", Label: "Done", BaseStatus: "done"},
		},
	}
	if err := validateWriteRequest(req); err == nil {
		t.Fatalf("a model with %d statuses must be rejected (min %d)", len(req.Statuses), minStatuses)
	}
}

func TestValidateWriteRequest_AcceptsThreeValidStatuses(t *testing.T) {
	req := writeStatusModelRequest{Name: "Plan-først", Statuses: threeValidStatuses()}
	if err := validateWriteRequest(req); err != nil {
		t.Fatalf("valid 3-status model rejected: %v", err)
	}
}

func TestValidateWriteRequest_RejectsUnknownBaseStatus(t *testing.T) {
	statuses := threeValidStatuses()
	statuses[1].BaseStatus = "shipped" // not one of the 7 base statuses
	req := writeStatusModelRequest{Name: "x", Statuses: statuses}
	if err := validateWriteRequest(req); err == nil {
		t.Fatal("status bound to an unknown base_status must be rejected")
	}
}

func TestValidateWriteRequest_RejectsDuplicateKeys(t *testing.T) {
	statuses := threeValidStatuses()
	statuses[2].Key = statuses[0].Key
	req := writeStatusModelRequest{Name: "x", Statuses: statuses}
	if err := validateWriteRequest(req); err == nil {
		t.Fatal("duplicate status keys must be rejected")
	}
}

func TestValidateWriteRequest_RejectsEmptyKeyOrLabel(t *testing.T) {
	statuses := threeValidStatuses()
	statuses[0].Label = "  "
	if err := validateWriteRequest(writeStatusModelRequest{Name: "x", Statuses: statuses}); err == nil {
		t.Fatal("blank label must be rejected")
	}
	statuses = threeValidStatuses()
	statuses[0].Key = ""
	if err := validateWriteRequest(writeStatusModelRequest{Name: "x", Statuses: statuses}); err == nil {
		t.Fatal("blank key must be rejected")
	}
}

func TestNormalizeStatuses_AssignsPositionByOrder(t *testing.T) {
	raw, err := normalizeStatuses(threeValidStatuses())
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}
	var got []statusEntry
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	for i, s := range got {
		if s.Position != i {
			t.Errorf("status %q: position = %d, want %d", s.Key, s.Position, i)
		}
	}
}

func TestDecodeStatuses_MalformedReturnsEmpty(t *testing.T) {
	if got := decodeStatuses([]byte("{not json")); len(got) != 0 {
		t.Fatalf("malformed JSON should decode to empty slice, got %d entries", len(got))
	}
	if got := decodeStatuses(nil); got == nil {
		t.Fatal("nil input should return a non-nil empty slice (parse-don't-crash)")
	}
}
