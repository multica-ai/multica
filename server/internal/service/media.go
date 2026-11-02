package service

import (
	"encoding/json"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type MediaRuntimeSelection struct {
	db.AgentRuntime
	Tag string
}

// SelectMediaRuntime applies machine-owner consent before considering quota.
// Unknown quota is usable but ranks behind any measured remaining capacity.
func SelectMediaRuntime(runtimes []db.AgentRuntime, caller pgtype.UUID, request protocol.MediaRequest, now time.Time) *MediaRuntimeSelection {
	var best *MediaRuntimeSelection
	bestScore := -2.0
	for i := range runtimes {
		rt := &runtimes[i]
		if rt.Status != "online" || !RuntimeAllowsUser(*rt, caller) {
			continue
		}
		var meta struct {
			Capabilities []string         `json:"capabilities"`
			Cooldowns    map[string]int64 `json:"media_cooldowns"`
		}
		if json.Unmarshal(rt.Metadata, &meta) != nil {
			continue
		}
		var quota protocol.RuntimePlanQuota
		_ = json.Unmarshal(rt.PlanQuota, &quota)
		for _, tag := range meta.Capabilities {
			provider := protocol.MediaTagProvider(tag)
			if !protocol.ValidMediaTag(rt.Provider, tag) || (request.Provider != "auto" && request.Provider != provider) || protocol.MediaCapabilityTag(provider, request.Capability) != tag || meta.Cooldowns[tag] > now.Unix() {
				continue
			}
			score, usable := mediaQuotaScore(quota, provider, now)
			if usable && score > bestScore {
				best, bestScore = &MediaRuntimeSelection{AgentRuntime: *rt, Tag: tag}, score
			}
		}
	}
	return best
}

func applicableMediaWindow(w protocol.RuntimePlanQuotaWindow, provider string) bool {
	switch provider {
	case "codex":
		return true
	case "agy":
		return w.Group == "gemini"
	default:
		return false
	}
}

func mediaQuotaScore(quota protocol.RuntimePlanQuota, provider string, now time.Time) (float64, bool) {
	score := -1.0
	hasActive := false
	for _, w := range quota.Windows {
		if !applicableMediaWindow(w, provider) || (w.ResetsAt != nil && *w.ResetsAt <= now.Unix()) {
			continue
		}
		hasActive = true
		if w.UsedPercent == nil {
			continue
		}
		remaining := 100 - *w.UsedPercent
		if math.IsNaN(remaining) || math.IsInf(remaining, 0) || remaining <= 0 {
			return 0, false
		}
		if score < 0 || remaining < score {
			score = remaining
		}
	}
	// Only Codex consumes the whole account quota. Antigravity's aggregate
	// limited status may come from a pool unrelated to this media source.
	if provider == "codex" && quota.Status == protocol.PlanQuotaStatusLimited && (hasActive || len(quota.Windows) == 0) {
		return 0, false
	}
	return score, true
}

func MediaCooldown(rt db.AgentRuntime, tag string, retryAfter int64, now time.Time) time.Time {
	if retryAfter > 0 {
		return now.Add(time.Duration(min(retryAfter, int64(86400*7))) * time.Second)
	}
	var quota protocol.RuntimePlanQuota
	_ = json.Unmarshal(rt.PlanQuota, &quota)
	var reset int64
	for _, w := range quota.Windows {
		if applicableMediaWindow(w, protocol.MediaTagProvider(tag)) && w.ResetsAt != nil && *w.ResetsAt > now.Unix() && (reset == 0 || *w.ResetsAt < reset) {
			reset = *w.ResetsAt
		}
	}
	if reset > 0 {
		return time.Unix(reset, 0)
	}
	return now.Add(time.Minute)
}
