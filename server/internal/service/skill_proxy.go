package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// skillMaxBody is the maximum bytes read from a skill API response.
// Skills are small JSON objects; 1MB is a generous upper bound that protects
// against a misbehaving upstream filling memory.
const skillMaxBody = 1 << 20 // 1 MB

// Skill represents a skill fetched from the costrict-web internal API.
type Skill struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`
	Type        string `json:"type"`
}

// skillCacheEntry holds a cached skill and its expiry time.
type skillCacheEntry struct {
	skill     Skill
	expiresAt time.Time
}

// agentRateBucket tracks per-agent sliding-window rate limiting state.
type agentRateBucket struct {
	timestamps []time.Time
}

// SkillProxy is an internal HTTP client for the costrict-web skill API.
// It provides caching, per-agent rate limiting (60/min, sliding window),
// and audit logging to the multica_agent_audit_logs table.
type SkillProxy struct {
	baseURL    string
	secret     string
	cacheTTL   time.Duration
	queries    *db.Queries
	httpClient *http.Client

	mu    sync.RWMutex
	cache map[string]skillCacheEntry

	rateMu  sync.Mutex
	buckets map[string]*agentRateBucket
}

// NewSkillProxy creates a new SkillProxy. queries may be nil to disable audit logging.
func NewSkillProxy(baseURL, secret string, cacheTTL time.Duration, queries *db.Queries) *SkillProxy {
	return &SkillProxy{
		baseURL:  baseURL,
		secret:   secret,
		cacheTTL: cacheTTL,
		queries:  queries,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		cache:   make(map[string]skillCacheEntry),
		buckets: make(map[string]*agentRateBucket),
	}
}

// rateLimitPerMin is the maximum number of non-cached calls per agent per minute.
const rateLimitPerMin = 60

// checkRateLimit returns an error if the agent has exceeded 60 calls/min.
// Cache hits don't count — callers should check rate limit before making
// the HTTP call, after confirming a cache miss.
func (sp *SkillProxy) checkRateLimit(agentID string) error {
	sp.rateMu.Lock()
	defer sp.rateMu.Unlock()

	now := time.Now()
	windowStart := now.Add(-time.Minute)

	bucket, ok := sp.buckets[agentID]
	if !ok {
		bucket = &agentRateBucket{}
		sp.buckets[agentID] = bucket
	}

	// Prune timestamps outside the sliding window.
	pruned := bucket.timestamps[:0]
	for _, ts := range bucket.timestamps {
		if ts.After(windowStart) {
			pruned = append(pruned, ts)
		}
	}
	bucket.timestamps = pruned

	if len(bucket.timestamps) >= rateLimitPerMin {
		return fmt.Errorf("rate limit exceeded: agent %s has made %d calls in the last minute", agentID, len(bucket.timestamps))
	}

	bucket.timestamps = append(bucket.timestamps, now)
	return nil
}

// FetchSkill fetches a skill by ID from the costrict-web API. Results are
// cached for the configured TTL. Cache hits don't count against the rate limit.
// Audit logs are written to the database when queries is non-nil.
func (sp *SkillProxy) FetchSkill(ctx context.Context, id, agentID string) (*Skill, error) {
	// Check cache first (cache hits don't count against rate limit).
	sp.mu.RLock()
	if entry, ok := sp.cache[id]; ok && time.Now().Before(entry.expiresAt) {
		sp.mu.RUnlock()
		skill := entry.skill
		return &skill, nil
	}
	sp.mu.RUnlock()

	// Rate limit check (before HTTP call, after cache miss).
	if err := sp.checkRateLimit(agentID); err != nil {
		sp.logAudit(ctx, agentID, "fetch_skill", "skill", id, 429, err.Error())
		return nil, err
	}

	url := fmt.Sprintf("%s/api/skills/%s", sp.baseURL, id)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("X-Internal-Secret", sp.secret)

	resp, err := sp.httpClient.Do(req)
	if err != nil {
		sp.logAudit(ctx, agentID, "fetch_skill", "skill", id, 0, err.Error())
		return nil, fmt.Errorf("fetching skill %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		sp.logAudit(ctx, agentID, "fetch_skill", "skill", id, resp.StatusCode, "non-200 response")
		return nil, fmt.Errorf("fetching skill %s: status %d", id, resp.StatusCode)
	}

	// Limit body size to protect against misbehaving upstream.
	limitedBody := io.LimitReader(resp.Body, skillMaxBody)

	var skill Skill
	if err := json.NewDecoder(limitedBody).Decode(&skill); err != nil {
		sp.logAudit(ctx, agentID, "fetch_skill", "skill", id, resp.StatusCode, err.Error())
		return nil, fmt.Errorf("decoding skill %s: %w", id, err)
	}

	// Cache the result.
	sp.mu.Lock()
	sp.cache[id] = skillCacheEntry{
		skill:     skill,
		expiresAt: time.Now().Add(sp.cacheTTL),
	}
	sp.mu.Unlock()

	sp.logAudit(ctx, agentID, "fetch_skill", "skill", id, resp.StatusCode, "")
	return &skill, nil
}

// ListSkills lists all skills from the costrict-web API. Results are not cached.
func (sp *SkillProxy) ListSkills() ([]Skill, error) {
	url := fmt.Sprintf("%s/api/skills", sp.baseURL)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("X-Internal-Secret", sp.secret)

	resp, err := sp.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("listing skills: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("listing skills: status %d", resp.StatusCode)
	}

	// Limit body size to protect against misbehaving upstream.
	limitedBody := io.LimitReader(resp.Body, skillMaxBody)

	var skills []Skill
	if err := json.NewDecoder(limitedBody).Decode(&skills); err != nil {
		return nil, fmt.Errorf("decoding skills list: %w", err)
	}

	return skills, nil
}

// InvalidateCache removes a skill from the cache.
func (sp *SkillProxy) InvalidateCache(id string) {
	sp.mu.Lock()
	delete(sp.cache, id)
	sp.mu.Unlock()
}

// logAudit writes an audit log entry to the database. Silently skips if
// queries is nil or the agent ID cannot be parsed as a UUID.
func (sp *SkillProxy) logAudit(ctx context.Context, agentID, action, targetType, targetID string, statusCode int, errMsg string) {
	if sp.queries == nil {
		return
	}

	var agentUUID pgtype.UUID
	if err := agentUUID.Scan(agentID); err != nil || !agentUUID.Valid {
		slog.Warn("skill_proxy: skipping audit log, invalid agent_id", "agent_id", agentID)
		return
	}

	var statusInt pgtype.Int4
	if statusCode > 0 {
		statusInt = pgtype.Int4{Int32: int32(statusCode), Valid: true}
	}

	var errText pgtype.Text
	if errMsg != "" {
		errText = pgtype.Text{String: errMsg, Valid: true}
	}

	_, err := sp.queries.CreateAgentAuditLog(ctx, db.CreateAgentAuditLogParams{
		AgentID:    agentUUID,
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		StatusCode: statusInt,
		ErrorMsg:   errText,
	})
	if err != nil {
		slog.Warn("skill_proxy: failed to write audit log", "error", err, "agent_id", agentID, "action", action)
	}
}
