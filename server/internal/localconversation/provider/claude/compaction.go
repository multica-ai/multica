package claude

import "crypto/rand"

// Claude's status and boundary messages describe one compaction without a
// shared item id. Keep the start's id until an authoritative boundary or
// failure closes it. A status reset alone does not confirm success.
func (n *Normalizer) compaction(m sdkMessage) []Event {
	starting := m.Subtype == "status" && m.Status == "compacting"
	failed := m.CompactResult == "failed"
	if !starting && !failed && m.Subtype != "compact_boundary" {
		return nil
	}
	if m.UUID != "" {
		if n.compactionSeen[m.UUID] {
			return nil
		}
		n.compactionSeen[m.UUID] = true
	}
	if starting && n.compactionID != "" {
		return nil
	}
	if n.compactionID == "" {
		id := m.UUID
		if id == "" {
			id = rand.Text()
		}
		n.compactionID = "compaction:" + id
	}
	id := n.compactionID
	if starting {
		n.openItem(id, "compaction", nil)
		return []Event{{ItemID: id, Type: "item.started", Payload: map[string]any{"kind": "compaction"}}}
	}
	n.compactionID = ""
	state := map[string]any{"kind": "compaction", "status": "completed"}
	if failed {
		state["status"] = "failed"
		if m.CompactError != "" {
			state["error"] = map[string]any{"kind": "compaction_failed", "message": m.CompactError}
		}
	}
	return []Event{n.complete(id, state)}
}
