package issueguard

import (
	"encoding/json"
	"fmt"
)

type QueueGapChildState struct {
	Status   string
	Metadata []byte
}

func ShouldEmitParentQueueGap(prevStatus, nextStatus, parentStatus string, parentMetadata []byte, children []QueueGapChildState) bool {
	if prevStatus != "todo" && prevStatus != "in_progress" {
		return false
	}
	if nextStatus != "in_review" {
		return false
	}
	if parentStatus != "in_progress" || len(children) == 0 {
		return false
	}
	if HasExplicitWait(parentMetadata) {
		return false
	}
	for _, child := range children {
		switch child.Status {
		case "todo", "in_progress":
			return false
		case "blocked":
			return false
		}
		if HasExplicitWait(child.Metadata) {
			return false
		}
	}
	return true
}

func HasExplicitWait(metadata []byte) bool {
	if len(metadata) == 0 {
		return false
	}
	var values map[string]any
	if err := json.Unmarshal(metadata, &values); err != nil {
		return false
	}
	for _, key := range []string{"waiting_on", "blocked_reason"} {
		if v, ok := values[key]; ok && fmt.Sprint(v) != "" {
			return true
		}
	}
	return false
}
