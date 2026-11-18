package kagentbroker

import (
	"encoding/json"
	"time"
)

// Recovery checks identity only, allowing cleanup after capability/spec drift.
// alpha7 routes by namespace/name, not UID: an unavailable identity check or a
// replacement resource cannot safely be bypassed with this upstream protocol.
func (p Policy) recoveryAgent(m map[string]any) bool {
	meta := object(m["metadata"])
	return m["apiVersion"] == "api.kagent.dev/v1alpha3" && m["kind"] == "Agent" &&
		meta["name"] == p.name() && meta["namespace"] == p.Namespace && meta["uid"] == p.ExpectedUID &&
		object(meta["labels"])[managedBy] == "multica" && object(meta["labels"])[agentIDLabel] == p.AgentID &&
		object(meta["annotations"])[ownerAnnotation] == p.OwnerToken
}

func generation(v any) bool {
	n, ok := v.(json.Number)
	if !ok {
		return false
	}
	i, err := n.Int64()
	// Preserve integers exactly in common JSON consumers as well as Go.
	return err == nil && i >= 0 && i <= 9007199254740991
}

func readiness(observed, out map[string]any) bool {
	meta := object(observed["metadata"])
	if v, ok := meta["generation"]; ok {
		if !generation(v) {
			return false
		}
	} else {
		delete(object(out["metadata"]), "generation")
	}
	status, exists := observed["status"]
	if !exists {
		return true
	}
	s := object(status)
	if s == nil {
		return false
	}
	v, exists := s["conditions"]
	if !exists {
		return true
	}
	a, ok := v.([]any)
	if !ok || len(a) > 32 {
		return false
	}
	conditions := make([]any, 0, len(a))
	seen := map[string]bool{}
	for _, v := range a {
		c := object(v)
		if !keys(c, []string{"type", "status"}, "reason", "message", "observedGeneration", "lastTransitionTime") {
			return false
		}
		for _, k := range []string{"type", "reason", "message"} {
			if !optionalText(c, k, false) {
				return false
			}
		}
		typ := str(c["type"])
		if typ == "" || len(typ) > 256 || seen[typ] {
			return false
		}
		seen[typ] = true
		if c["status"] != "True" && c["status"] != "False" && c["status"] != "Unknown" {
			return false
		}
		if g, ok := c["observedGeneration"]; ok && !generation(g) {
			return false
		}
		if ts, ok := c["lastTransitionTime"]; ok {
			if _, err := time.Parse(time.RFC3339Nano, str(ts)); err != nil {
				return false
			}
		}
		conditions = append(conditions, c)
	}
	out["status"] = map[string]any{"conditions": conditions}
	return true
}
