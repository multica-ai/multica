package catalog

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
)

var reasoningEfforts = map[string]bool{"none": true, "minimal": true, "low": true, "medium": true, "high": true, "xhigh": true, "max": true, "ultra": true}

func effortLabel(v string) string {
	if v == "xhigh" {
		return "Extra high"
	}
	return strings.ToUpper(v[:1]) + v[1:]
}

// claudeModels reads the models of Claude Code's initialize response. Rows keep
// their picker token as the ID, since settings already hold those. The default
// row is folded the way pkg/agent folds it: "follow the default" is an empty
// model, so it only marks the row that resolves to the same model.
func claudeModels(initialize json.RawMessage) Models {
	var result struct {
		Models []struct {
			Value                 string   `json:"value"`
			ResolvedModel         string   `json:"resolvedModel"`
			DisplayName           string   `json:"displayName"`
			SupportsEffort        bool     `json:"supportsEffort"`
			SupportedEffortLevels []string `json:"supportedEffortLevels"`
			Disabled              bool     `json:"disabled"`
		} `json:"models"`
	}
	_ = json.Unmarshal(initialize, &result)
	models := Models{Supported: true, Models: []Model{}}
	defaultRow := -1
	identity := map[string]int{}
	for i, m := range result.Models {
		if m.Value == agent.ClaudeDefaultModelValue {
			defaultRow = i
			continue
		}
		if m.Value == "" {
			continue
		}
		model := Model{ID: m.Value, Label: m.DisplayName, Thinking: claudeThinking(m.SupportsEffort, m.SupportedEffortLevels)}
		if model.Label == "" {
			model.Label = m.Value
		}
		identity[agent.ClaudeModelID(m.Value, m.ResolvedModel)] = len(models.Models)
		models.Models = append(models.Models, model)
	}
	if defaultRow < 0 {
		return models
	}
	row := result.Models[defaultRow]
	id := agent.ClaudeModelID(row.Value, row.ResolvedModel)
	if i, ok := identity[id]; ok {
		models.Models[i].IsDefault = true
	} else if !row.Disabled && id != agent.ClaudeDefaultModelValue {
		// An org-restricted install can resolve its default to a model no other
		// row names; without this row nothing says what following it runs.
		label := row.DisplayName
		if label == "" {
			label = id
		}
		models.Models = append(models.Models, Model{ID: id, Label: label, IsDefault: true, Thinking: claudeThinking(row.SupportsEffort, row.SupportedEffortLevels)})
	}
	return models
}

func claudeThinking(supported bool, levels []string) *Thinking {
	if !supported || len(levels) == 0 {
		return nil
	}
	thinking := &Thinking{SupportedLevels: []Level{}}
	for _, level := range levels {
		if reasoningEfforts[level] {
			thinking.SupportedLevels = append(thinking.SupportedLevels, Level{Value: level, Label: effortLabel(level)})
		}
	}
	return thinking
}

// codexModels reads Codex's local model cache. A missing or unreadable
// cache is an empty catalog.
func codexModels(codexDir string) Models {
	models := Models{Supported: true, Models: []Model{}}
	data, err := os.ReadFile(filepath.Join(codexDir, "models_cache.json"))
	if err != nil {
		return models
	}
	var cache struct {
		Models []struct {
			Slug                     string `json:"slug"`
			DisplayName              string `json:"display_name"`
			Visibility               string `json:"visibility"`
			DefaultReasoningLevel    string `json:"default_reasoning_level"`
			SupportedReasoningLevels []struct {
				Effort      string `json:"effort"`
				Description string `json:"description"`
			} `json:"supported_reasoning_levels"`
		} `json:"models"`
	}
	if json.Unmarshal(data, &cache) != nil {
		return models
	}
	for _, item := range cache.Models {
		if item.Slug == "" || item.Visibility == "hide" {
			continue
		}
		model := Model{ID: item.Slug, Label: item.DisplayName}
		if model.Label == "" {
			model.Label = item.Slug
		}
		if item.SupportedReasoningLevels != nil {
			thinking := &Thinking{SupportedLevels: []Level{}}
			for _, level := range item.SupportedReasoningLevels {
				if reasoningEfforts[level.Effort] {
					thinking.SupportedLevels = append(thinking.SupportedLevels, Level{Value: level.Effort, Label: effortLabel(level.Effort), Description: level.Description})
				}
			}
			if reasoningEfforts[item.DefaultReasoningLevel] {
				thinking.DefaultLevel = item.DefaultReasoningLevel
			}
			model.Thinking = thinking
		}
		models.Models = append(models.Models, model)
	}
	return models
}

// parseAccount accepts only provider-owned numeric fields: control
// envelopes and credentials never pass through.
func parseAccount(provider string, raw json.RawMessage, now time.Time) AccountUsage {
	var data map[string]any
	_ = json.Unmarshal(raw, &data)
	var windows []Window
	var plan *string
	if provider == "claude" {
		plan = stringPtr(data["subscription_type"])
		limits := object(data["rate_limits"])
		for _, w := range []struct{ key, label string }{{"five_hour", "5h"}, {"seven_day", "7d"}} {
			item := object(limits[w.key])
			if win, ok := window(w.key, w.label, item["utilization"], item["resets_at"]); ok {
				windows = append(windows, win)
			}
		}
		scoped, _ := limits["model_scoped"].([]any)
		for i, raw := range scoped {
			item := object(raw)
			label := "Model"
			if name, ok := item["display_name"].(string); ok {
				label = truncate(name, 80)
			}
			if win, ok := window(fmt.Sprintf("model:%d", i), label, item["utilization"], item["resets_at"]); ok {
				windows = append(windows, win)
			}
		}
	} else {
		type entry struct {
			id     string
			limits map[string]any
		}
		var all []entry
		for id, v := range object(data["rateLimitsByLimitId"]) {
			all = append(all, entry{id, object(v)})
		}
		if len(all) == 0 {
			all = []entry{{"codex", object(data["rateLimits"])}}
		}
		sort.Slice(all, func(i, j int) bool { return all[i].id < all[j].id })
		for _, e := range all {
			if plan == nil {
				plan = stringPtr(e.limits["planType"])
			}
			for _, key := range []string{"primary", "secondary"} {
				item := object(e.limits[key])
				duration := key
				if minutes, ok := nonNegative(item["windowDurationMins"]); ok {
					if minutes >= 1440 {
						duration = trimFloat(minutes/1440) + "d"
					} else {
						duration = trimFloat(minutes/60) + "h"
					}
				}
				label := duration
				if e.id != "codex" {
					label = truncate(e.id, 60) + " · " + duration
				}
				if win, ok := window(e.id+":"+key, label, item["usedPercent"], item["resetsAt"]); ok {
					windows = append(windows, win)
				}
			}
		}
	}
	status := "unavailable"
	if len(windows) > 0 {
		status = "available"
	} else {
		windows = []Window{}
	}
	at := now.UTC().Format(time.RFC3339)
	return AccountUsage{Status: status, Plan: plan, Windows: windows, UpdatedAt: &at}
}

// claudeModelLimits reads Claude's per-model limits from its usage reply.
func claudeModelLimits(raw json.RawMessage) map[string]json.RawMessage {
	var data struct {
		Session struct {
			ModelUsage map[string]json.RawMessage `json:"model_usage"`
		} `json:"session"`
	}
	_ = json.Unmarshal(raw, &data)
	return data.Session.ModelUsage
}

// parseContext reads the latest main-conversation request of a session
// transcript: context is never the sum of billed turns.
func parseContext(provider string, lines [][]byte) ContextUsage {
	var context ContextUsage
	for _, line := range lines {
		var row map[string]any
		if json.Unmarshal(line, &row) != nil {
			continue
		}
		if provider == "claude" {
			if row["type"] != "assistant" || row["isSidechain"] == true || row["parent_tool_use_id"] != nil {
				continue
			}
			message := object(row["message"])
			usage := object(message["usage"])
			input, ok := nonNegative(usage["input_tokens"])
			if !ok {
				continue
			}
			cacheRead, _ := nonNegative(usage["cache_read_input_tokens"])
			cacheWrite, _ := nonNegative(usage["cache_creation_input_tokens"])
			used := int64(input + cacheRead + cacheWrite)
			context = ContextUsage{UsedTokens: &used, Model: stringPtr(message["model"]), UpdatedAt: stringPtr(row["timestamp"])}
		} else {
			payload := object(row["payload"])
			info := object(payload["info"])
			total, ok := nonNegative(object(info["last_token_usage"])["total_tokens"])
			if row["type"] != "event_msg" || payload["type"] != "token_count" || !ok {
				continue
			}
			used := int64(total)
			context = ContextUsage{UsedTokens: &used, UpdatedAt: stringPtr(row["timestamp"])}
			if limit, ok := nonNegative(info["model_context_window"]); ok {
				l := int64(limit)
				context.LimitTokens = &l
			}
		}
	}
	return context
}

func window(id, label string, percent, resets any) (Window, bool) {
	used, ok := nonNegative(percent)
	if !ok {
		return Window{}, false
	}
	return Window{ID: id, Label: label, UsedPercent: math.Min(100, used), ResetsAt: resetTime(resets)}, true
}

func resetTime(v any) *string {
	var t time.Time
	switch v := v.(type) {
	case float64:
		t = time.Unix(int64(v), 0)
	case string:
		parsed, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return nil
		}
		t = parsed
	default:
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

func nonNegative(v any) (float64, bool) {
	f, ok := v.(float64)
	return f, ok && f >= 0 && !math.IsInf(f, 0) && !math.IsNaN(f)
}

func object(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func stringPtr(v any) *string {
	if s, ok := v.(string); ok {
		return &s
	}
	return nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

func trimFloat(f float64) string {
	return strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%.2f", f), "0"), ".")
}
