package agent

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
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
			cronExpression, err := normalizeClaudeCron(input.Cron)
			if err != nil {
				continue
			}
			schedule, err := cron.ParseStandard(cronExpression)
			if err != nil {
				continue
			}
			if s.crons == nil {
				s.crons = make(map[string]claudeCron)
			}
			entry := claudeCron{schedule: schedule, next: schedule.Next(now), recurring: receipt.Recurring}
			if receipt.Recurring {
				entry.expires = now.Add(7 * 24 * time.Hour)
			}
			s.crons[receipt.ID] = entry
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

// A main-thread response after a due time consumes that scheduled fire. Claude
// emits scheduled_task_fire for cron-driven turns; that marker permits the
// documented 90-second early window for one-shots at :00/:30. Other early
// responses (for example an unrelated task notification) leave them intact.
func (s *claudeSchedules) beginTurn(now time.Time, scheduledCronFire bool) {
	if !s.wakeup.After(now) {
		s.wakeup = time.Time{}
	}
	for id, entry := range s.crons {
		if (entry.recurring && entry.expires.Before(now)) ||
			(!entry.recurring && claudeOneShotFired(entry.next, now, scheduledCronFire)) {
			delete(s.crons, id)
			continue
		}
		if !entry.next.After(now) {
			entry.next = entry.schedule.Next(now)
			s.crons[id] = entry
		}
	}
}

func claudeOneShotFired(next, now time.Time, scheduledCronFire bool) bool {
	if !next.After(now) {
		return true
	}
	if !scheduledCronFire || next.Minute() != 0 && next.Minute() != 30 {
		return false
	}
	return !now.Before(next.Add(-90 * time.Second))
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
		if entry.recurring && entry.expires.Before(next) {
			next = entry.expires
		}
		if until.IsZero() || next.Before(until) {
			until = next
		}
	}
	return until, !until.IsZero()
}

// normalizeClaudeCron converts Claude's Sunday value 7 into robfig/cron's
// equivalent 0. Claude accepts both values, including 7 inside lists and
// ascending numeric ranges; robfig's standard five-field parser accepts only
// 0-6. Other fields and expressions pass through unchanged for the parser to
// validate.
func normalizeClaudeCron(expression string) (string, error) {
	fields := strings.Fields(expression)
	if len(fields) != 5 {
		return "", fmt.Errorf("claude cron: expected 5 fields, got %d", len(fields))
	}

	parts := strings.Split(fields[4], ",")
	normalized := make([]string, 0, len(parts))
	for _, part := range parts {
		values, changed, err := normalizeClaudeSundayPart(part)
		if err != nil {
			return "", err
		}
		if changed {
			normalized = append(normalized, values...)
		} else {
			normalized = append(normalized, part)
		}
	}
	fields[4] = strings.Join(normalized, ",")
	return strings.Join(fields, " "), nil
}

func normalizeClaudeSundayPart(part string) ([]string, bool, error) {
	if part == "7" {
		return []string{"0"}, true, nil
	}

	rangePart, stepPart, hasStep := strings.Cut(part, "/")
	startText, endText, hasRange := strings.Cut(rangePart, "-")
	if !hasRange || endText != "7" {
		return nil, false, nil
	}
	start, err := strconv.Atoi(startText)
	if err != nil || start < 0 || start > 7 {
		return nil, false, nil
	}
	step := 1
	if hasStep {
		step, err = strconv.Atoi(stepPart)
		if err != nil || step <= 0 {
			return nil, false, fmt.Errorf("claude cron: invalid weekday step %q", stepPart)
		}
	}

	values := make([]string, 0, 8-start)
	seen := make(map[int]bool, 8-start)
	for day := start; day <= 7; day += step {
		normalizedDay := day
		if day == 7 {
			normalizedDay = 0
		}
		if !seen[normalizedDay] {
			values = append(values, strconv.Itoa(normalizedDay))
			seen[normalizedDay] = true
		}
	}
	return values, true, nil
}
