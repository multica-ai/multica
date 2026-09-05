package agent

import (
	"encoding/json"
	"time"

	"github.com/robfig/cron/v3"
)

// claudeSchedules observes native scheduling receipts. Claude owns the timers,
// prompt expansion and dispatch; this only decides when its stdin may close.
// A successful tool_result alone is insufficient: disabled/expired dynamic
// loops return a non-error result with scheduledFor=0.
type claudeSchedules struct {
	calls  map[string]claudeContentBlock
	wakeup time.Time
	crons  map[string]claudeCron
}

type claudeCron struct {
	schedule  cron.Schedule
	next      time.Time
	expires   time.Time
	recurring bool
}

func (s *claudeSchedules) observe(msg claudeSDKMessage, now time.Time) {
	if msg.ParentToolUseID != "" {
		return
	}
	var content claudeMessageContent
	if json.Unmarshal(msg.Message, &content) != nil {
		return
	}
	for _, block := range content.Content {
		if msg.Type == "assistant" && block.Type == "tool_use" && block.ID != "" {
			switch block.Name {
			case "ScheduleWakeup", "CronCreate", "CronDelete":
				if s.calls == nil {
					s.calls = make(map[string]claudeContentBlock)
				}
				s.calls[block.ID] = block
			}
		}
		if msg.Type != "user" || block.Type != "tool_result" {
			continue
		}
		call, ok := s.calls[block.ToolUseID]
		if !ok {
			continue
		}
		delete(s.calls, block.ToolUseID)
		if block.IsError {
			continue
		}
		switch call.Name {
		case "ScheduleWakeup":
			var receipt struct {
				ScheduledFor int64 `json:"scheduledFor"`
				Stopped      bool  `json:"stopped"`
			}
			if json.Unmarshal(msg.ToolUseResult, &receipt) != nil {
				continue
			}
			if receipt.Stopped {
				s.wakeup = time.Time{}
			} else if receipt.ScheduledFor > 0 {
				s.wakeup = time.UnixMilli(receipt.ScheduledFor)
			}
		case "CronCreate":
			var receipt struct {
				ID        string `json:"id"`
				Recurring bool   `json:"recurring"`
			}
			var input struct {
				Cron string `json:"cron"`
			}
			if json.Unmarshal(msg.ToolUseResult, &receipt) != nil || receipt.ID == "" || json.Unmarshal(call.Input, &input) != nil {
				continue
			}
			schedule, err := cron.ParseStandard(input.Cron)
			if err != nil {
				continue
			}
			if s.crons == nil {
				s.crons = make(map[string]claudeCron)
			}
			s.crons[receipt.ID] = claudeCron{schedule: schedule, next: schedule.Next(now), expires: now.Add(7 * 24 * time.Hour), recurring: receipt.Recurring}
		case "CronDelete":
			var receipt struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(msg.ToolUseResult, &receipt) != nil {
				continue
			}
			delete(s.crons, receipt.ID)
		}
	}
}

// A main-thread response after a due time consumes that scheduled fire. Early
// responses (for example a task notification) leave future wakeups intact.
func (s *claudeSchedules) beginTurn(now time.Time) {
	if !s.wakeup.After(now) {
		s.wakeup = time.Time{}
	}
	for id, entry := range s.crons {
		if entry.expires.Before(now) || !entry.recurring && !entry.next.After(now) {
			delete(s.crons, id)
			continue
		}
		if !entry.next.After(now) {
			entry.next = entry.schedule.Next(now)
			s.crons[id] = entry
		}
	}
}

func (s *claudeSchedules) waitingUntil() (time.Time, bool) {
	until := s.wakeup
	for _, entry := range s.crons {
		// Claude jitters recurring fires by up to half an interval, capped at
		// 30 minutes. After that boundary the daemon's normal idle budget applies.
		next := entry.next
		if entry.recurring {
			jitter := min(entry.schedule.Next(next).Sub(next)/2, 30*time.Minute)
			next = next.Add(jitter)
		}
		if until.IsZero() || next.Before(until) {
			until = next
		}
	}
	return until, !until.IsZero()
}
