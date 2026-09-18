package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Retain the existing 40KB prompt budget, including the <=12KB instruction.
// Full receipts stay linked to the task; only the prompt's evidence is condensed.
const wakeupNoteLimit = 40000
const wakeupOmittedEvidence = "Some trigger details were omitted to keep this prompt bounded. Read current issue comments, runs and state before deciding what to do.\n"

func buildWakeupNote(w db.IssueWakeup, previous string, receipts []db.IssueWakeupReceipt) string {
	header := "Wakeup " + util.UUIDToString(w.ID) + " triggered. Instruction:\n" + w.Instruction + "\nTrigger facts (read current state before deciding what to do):\n"
	budget := wakeupNoteLimit - len(header) - len(wakeupOmittedEvidence)
	omitted := strings.Contains(previous, wakeupOmittedEvidence)
	previous = strings.TrimPrefix(strings.TrimPrefix(previous, header), wakeupOmittedEvidence)
	parts := []string{}
	total := 0
	appendPart := func(part string) {
		parts = append(parts, part)
		total += len(part)
		for total > budget && len(parts) > 0 {
			total -= len(parts[0])
			parts = parts[1:]
			omitted = true
		}
	}
	if previous != "" {
		appendPart(previous)
	}
	for _, r := range receipts {
		line := fmt.Sprintf("%s %s\n", r.EventType, r.Payload)
		if len(line) > budget {
			// A single large changed-key list must not hide its event and source
			// references or prevent consumption. Do not cut JSON/UTF-8 mid-value.
			var fields map[string]json.RawMessage
			_ = json.Unmarshal(r.Payload, &fields)
			refs := map[string]json.RawMessage{}
			for _, key := range []string{"event_id", "occurred_at", "task_id", "source_task_id", "comment_id", "thread_id", "attachment_id", "agent_id"} {
				if value := fields[key]; len(value) > 0 && len(value) <= 256 {
					refs[key] = value
				}
			}
			compact, _ := json.Marshal(refs)
			line = fmt.Sprintf("%s %s (receipt %s; additional fields omitted)\n", r.EventType, compact, util.UUIDToString(r.ID))
			omitted = true
		}
		appendPart(line)
	}
	if omitted {
		header += wakeupOmittedEvidence
	}
	return header + strings.Join(parts, "")
}
