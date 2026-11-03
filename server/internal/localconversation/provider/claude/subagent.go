package claude

import (
	"encoding/json"
	"strconv"
)

// Each child has its own stream indices and tool IDs. Its result and native
// session never participate in the root conversation's lifecycle.
func (n *Normalizer) subagent(raw json.RawMessage, parent string) []Event {
	child := n.children[parent]
	if child == nil {
		child = NewNormalizer("")
		// Child questions are tools in the transcript, not root interactions.
		child.questionTool = ""
		n.children[parent] = child
	}
	var envelope map[string]json.RawMessage
	_ = json.Unmarshal(raw, &envelope)
	delete(envelope, "parent_tool_use_id")
	message, _ := json.Marshal(envelope)
	events := child.Accept(message)
	if result := child.TakeResult(); result != nil {
		status := "cancelled"
		if result.IsError {
			status = "failed"
		}
		events = append(events, child.Close(status)...)
	}
	return n.childEvents(parent, events)
}

func (n *Normalizer) childEvents(parent string, events []Event) []Event {
	order := n.childOrders[parent]
	if order == nil {
		order = map[string]int{}
		n.childOrders[parent] = order
	}
	var out []Event
	for _, event := range events {
		// Reasoning is private; only public messages and tool activity persist.
		switch event.Payload["kind"] {
		case "assistant_text", "command", "tool_call", "image", "error":
		default:
			continue
		}
		position, exists := order[event.ItemID]
		if !exists {
			position = len(order)
			order[event.ItemID] = position
		}
		event.Payload["item_order"] = position
		event.ItemID = "subagent:" + strconv.Itoa(len(parent)) + ":" + parent + ":" + event.ItemID
		event.Payload["parent_tool_use_id"] = parent
		// Older clients skip this unknown kind instead of mixing child output
		// into the root transcript. The public item's shape stays unchanged.
		event.Payload["item_kind"] = event.Payload["kind"]
		event.Payload["kind"] = "subagent"
		out = append(out, event)
	}
	return out
}

func (n *Normalizer) endTask(id, status string) []Event {
	var events []Event
	if status != "" {
		parent := stringValue(n.tasks[id].state["tool_use_id"])
		if child := n.children[parent]; child != nil {
			terminal := status
			if terminal == "completed" {
				terminal = "cancelled"
			}
			events = append(events, n.childEvents(parent, child.Close(terminal))...)
		}
	}
	return append(events, n.taskEvent(id, status))
}
