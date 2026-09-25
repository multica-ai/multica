package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Retain the existing 40KB prompt budget, including the <=12KB instruction.
// Consumed notifications stay linked to the task during the retention window.
const wakeupNoteLimit = 40000
const wakeupLegacyHeading = "Previous wakeup context (historical; follow the current instruction above):\n"
const wakeupOmittedEvidence = "Some trigger details were omitted to keep this prompt bounded. Read current issue comments, runs and state before deciding what to do.\n"

// Keep facts as data in task.context; handoff_note is only a rendering.
// Legacy notes remain opaque during mixed-version operation, never parsed.
type wakeupEvidence struct {
	Version     int          `json:"version"`
	Instruction string       `json:"instruction,omitempty"`
	Facts       []wakeupFact `json:"facts,omitempty"`
	Legacy      string       `json:"legacy,omitempty"`
	Omitted     bool         `json:"omitted,omitempty"`
}
type wakeupFact struct {
	EventType string          `json:"event_type"`
	Payload   json.RawMessage `json:"payload"`
}

func mergeWakeupEvidence(w db.IssueWakeup, previous db.AgentTaskQueue, receipts []db.IssueWakeupReceipt) (string, json.RawMessage) {
	var stored struct {
		Evidence wakeupEvidence `json:"wakeup_evidence"`
	}
	_ = json.Unmarshal(previous.Context, &stored)
	evidence := stored.Evidence
	// jsonb can reorder object keys/spacing; normalize before rendering.
	for i := range evidence.Facts {
		evidence.Facts[i].Payload = canonicalWakeupPayload(evidence.Facts[i].Payload)
	}
	previousConfig := w
	if evidence.Instruction != "" {
		previousConfig.Instruction = evidence.Instruction
	}
	if evidence.Version != 1 || renderWakeupEvidence(previousConfig, evidence) != previous.HandoffNote.String {
		// An old dispatcher may have updated only handoff_note. Preserve its text
		// as one bounded legacy item instead of trusting a stale structured summary.
		evidence = wakeupEvidence{Version: 1, Legacy: previous.HandoffNote.String}
	}
	if w.Kind != "event" {
		evidence = wakeupEvidence{Version: 1}
	}

	header := "Wakeup " + util.UUIDToString(w.ID) + " triggered. Instruction:\n" + w.Instruction + "\nTrigger facts (read current state before deciding what to do):\n"
	budget := wakeupNoteLimit - len(header) - len(wakeupOmittedEvidence) - len(wakeupLegacyHeading)
	budget = max(budget, 0)
	total := len(evidence.Legacy)
	for _, fact := range evidence.Facts {
		total += len(fact.EventType) + len(fact.Payload) + 2
	}
	trim := func() {
		if total > budget && evidence.Legacy != "" {
			total -= len(evidence.Legacy)
			evidence.Legacy = ""
			evidence.Omitted = true
		}
		for total > budget && len(evidence.Facts) > 0 {
			fact := evidence.Facts[0]
			total -= len(fact.EventType) + len(fact.Payload) + 2
			evidence.Facts = evidence.Facts[1:]
			evidence.Omitted = true
		}
	}
	trim()
	for _, r := range receipts {
		var summary struct {
			Count int64 `json:"coalesced_count"`
		}
		_ = json.Unmarshal(r.Payload, &summary)
		if summary.Count > 1 {
			evidence.Omitted = true
		}
		payload := canonicalWakeupPayload(r.Payload)
		if len(r.EventType)+len(payload)+2 > budget {
			// Keep references as data even when a changed-field list is oversized.
			var fields map[string]json.RawMessage
			_ = json.Unmarshal(payload, &fields)
			refs := map[string]json.RawMessage{}
			for _, key := range []string{"event_id", "occurred_at", "first_occurred_at", "coalesced_count", "task_id", "source_task_id", "comment_id", "thread_id", "attachment_id", "agent_id", "actor_type", "actor_id"} {
				if value := fields[key]; len(value) > 0 && len(value) <= 256 {
					refs[key] = value
				}
			}
			refs["receipt_id"], _ = json.Marshal(util.UUIDToString(r.ID))
			payload, _ = json.Marshal(refs)
			evidence.Omitted = true
		}
		evidence.Facts = append(evidence.Facts, wakeupFact{EventType: r.EventType, Payload: payload})
		total += len(r.EventType) + len(payload) + 2
		trim()
	}
	evidence.Instruction = w.Instruction
	raw, _ := json.Marshal(evidence)
	return renderWakeupEvidence(w, evidence), raw
}

func renderWakeupEvidence(w db.IssueWakeup, evidence wakeupEvidence) string {
	header := "Wakeup " + util.UUIDToString(w.ID) + " triggered. Instruction:\n" + w.Instruction + "\nTrigger facts (read current state before deciding what to do):\n"
	if evidence.Omitted {
		header += wakeupOmittedEvidence
	}
	var b strings.Builder
	b.WriteString(header)
	if evidence.Legacy != "" {
		b.WriteString(wakeupLegacyHeading)
		b.WriteString(evidence.Legacy)
	}
	for _, fact := range evidence.Facts {
		fmt.Fprintf(&b, "%s %s\n", fact.EventType, fact.Payload)
	}
	return b.String()
}

func canonicalWakeupPayload(raw json.RawMessage) json.RawMessage {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		return json.RawMessage(`{}`)
	}
	result, _ := json.Marshal(value)
	return result
}
