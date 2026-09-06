package handler

import (
	"encoding/json"
	"fmt"
	"time"
)

// Resolve member instructions once on the server. The browser and scheduled
// secretary consume this same view; the saved projection remains rebuildable.
func resolveSecretaryView(raw json.RawMessage, instructions []secretaryStoredInstruction) (json.RawMessage, error) {
	var projection map[string]any
	if err := json.Unmarshal(raw, &projection); err != nil {
		return nil, err
	}
	rows, ok := projection["items"].([]any)
	if !ok {
		return nil, fmt.Errorf("invalid secretary items")
	}
	items := map[string]map[string]any{}
	for _, row := range rows {
		item, ok := row.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid secretary item")
		}
		key, ok := item["key"].(string)
		if !ok {
			return nil, fmt.Errorf("invalid secretary key")
		}
		items[key] = item
	}
	latest := map[string]secretaryStoredInstruction{}
	for _, instruction := range instructions {
		item := items[instruction.ItemKey]
		if item == nil {
			continue
		}
		if instruction.Kind == "capacity" {
			continue
		}
		if instruction.Kind != "plan" {
			latest[instruction.ItemKey] = instruction
			continue
		}
		var payload secretaryInstructionRequest
		if err := json.Unmarshal(instruction.Payload, &payload); err != nil {
			return nil, err
		}
		item["scheduled_on"] = payload.ScheduledOn
		item["estimate_minutes"] = payload.EstimateMinutes
	}
	for key, instruction := range latest {
		item := items[key]
		var payload secretaryInstructionRequest
		if err := json.Unmarshal(instruction.Payload, &payload); err != nil {
			return nil, err
		}
		item["instruction_note"] = payload.Note
		item["pending_reconciliation"] = instruction.CanonicalReceipt == nil
		if instruction.CanonicalReceipt != nil || item["stage"] == "history" {
			continue
		}
		followUp := instruction.CreatedAt.In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("2006-01-02")
		switch instruction.Kind {
		case "complete":
			item["stage"] = "preparing"
			item["owner"] = "secretary"
			item["reported_status"] = "completed"
			item["reported_at"] = instruction.CreatedAt.Format(time.RFC3339Nano)
			if item["closure_mode"] == "self_report" {
				item["stage"] = "history"
			}
			item["next_step"] = "秘书核对你反馈的结果，确认后办结；只反馈实际缺口，不重复催办这一步。"
			item["follow_up_on"] = followUp
		case "cancel":
			item["stage"] = "history"
			item["reported_status"] = "cancelled"
		case "prepare":
			item["stage"] = "preparing"
			item["owner"] = "secretary"
			item["reported_status"] = nil
			item["next_step"] = payload.Note
			item["follow_up_on"] = followUp
		case "wait":
			item["stage"] = "waiting"
			item["owner"] = "external"
			item["reported_status"] = nil
			item["next_step"] = payload.Note
			item["follow_up_on"] = payload.ScheduledOn
		case "later":
			item["stage"] = "later"
			item["owner"] = "secretary"
			item["reported_status"] = nil
			item["next_step"] = payload.Note
			item["follow_up_on"] = payload.ScheduledOn
		}
	}
	return json.Marshal(projection)
}
