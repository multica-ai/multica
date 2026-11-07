package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis-backed CLIUpgradeStore. Agent CLI upgrades have the same shape as
// daemon CLI updates: the browser creates a job, a heartbeat claims it, the
// daemon reports progress, and the UI polls by job id. In a multi-node deploy
// those calls can land on different API replicas, so the lifecycle lives in
// shared storage.
//
// Each runtime's jobs sit in one JSON blob. Mutations compare-and-swap that
// blob with a Lua script so two nodes cannot both pop the serial queue.

const (
	cliUpgradeRuntimePrefix = "mul:" + runtimePendingRedisHashTag + ":cli_upgrade:rt:"
	cliUpgradeJobPrefix     = "mul:" + runtimePendingRedisHashTag + ":cli_upgrade:job:"
	cliUpgradeCASRetries    = 8
)

func cliUpgradeRuntimeKey(runtimeID string) string {
	return cliUpgradeRuntimePrefix + runtimeID
}

func cliUpgradeJobKey(id string) string {
	return cliUpgradeJobPrefix + id
}

// cliUpgradeCAS writes the runtime blob only when it still matches the value
// the caller read. KEYS[1] is the blob. Later KEYS are job-index sets, then
// job-index deletes. ARGV is expected blob, next blob (empty deletes), TTL
// seconds, set-count, then one value per set key.
var cliUpgradeCAS = redis.NewScript(`
local cur = redis.call('GET', KEYS[1])
if cur == false then cur = '' end
if cur ~= ARGV[1] then
  return 0
end
local ttl = tonumber(ARGV[3])
if ARGV[2] == '' then
  redis.call('DEL', KEYS[1])
else
  redis.call('SET', KEYS[1], ARGV[2], 'EX', ttl)
end
local nset = tonumber(ARGV[4])
for i = 1, nset do
  redis.call('SET', KEYS[1 + i], ARGV[4 + i], 'EX', ttl)
end
for i = 2 + nset, #KEYS do
  redis.call('DEL', KEYS[i])
end
return 1
`)

type cliUpgradeBlob struct {
	Jobs []cliUpgradeStoredJob `json:"jobs"`
}

// cliUpgradeStoredJob keeps the wait clocks that the public job JSON hides.
type cliUpgradeStoredJob struct {
	ID              string     `json:"id"`
	RuntimeID       string     `json:"runtime_id"`
	CLI             string     `json:"cli"`
	InitiatorUserID string     `json:"initiator_user_id,omitempty"`
	Status          string     `json:"status"`
	Error           string     `json:"error,omitempty"`
	Current         string     `json:"current,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	QueuedAt        time.Time  `json:"queued_at"`
	DispatchedAt    *time.Time `json:"dispatched_at,omitempty"`
}

func (s cliUpgradeStoredJob) job() *CLIUpgradeJob {
	job := &CLIUpgradeJob{
		ID:              s.ID,
		RuntimeID:       s.RuntimeID,
		CLI:             s.CLI,
		InitiatorUserID: s.InitiatorUserID,
		Status:          s.Status,
		Error:           s.Error,
		Current:         s.Current,
		CreatedAt:       s.CreatedAt,
		UpdatedAt:       s.UpdatedAt,
		QueuedAt:        s.QueuedAt,
		DispatchedAt:    s.DispatchedAt,
	}
	return job
}

func storedCLIUpgradeJob(job *CLIUpgradeJob) cliUpgradeStoredJob {
	if job == nil {
		return cliUpgradeStoredJob{}
	}
	return cliUpgradeStoredJob{
		ID:              job.ID,
		RuntimeID:       job.RuntimeID,
		CLI:             job.CLI,
		InitiatorUserID: job.InitiatorUserID,
		Status:          job.Status,
		Error:           job.Error,
		Current:         job.Current,
		CreatedAt:       job.CreatedAt,
		UpdatedAt:       job.UpdatedAt,
		QueuedAt:        job.QueuedAt,
		DispatchedAt:    job.DispatchedAt,
	}
}

type RedisCLIUpgradeStore struct {
	rdb redis.UniversalClient
}

func NewRedisCLIUpgradeStore(rdb redis.UniversalClient) *RedisCLIUpgradeStore {
	return &RedisCLIUpgradeStore{rdb: rdb}
}

var _ CLIUpgradeStore = (*RedisCLIUpgradeStore)(nil)

func (s *RedisCLIUpgradeStore) Create(ctx context.Context, runtimeID, cli, initiatorUserID string) (*CLIUpgradeJob, error) {
	cli = strings.ToLower(strings.TrimSpace(cli))
	var created *CLIUpgradeJob
	err := s.mutate(ctx, runtimeID, func(jobs []*CLIUpgradeJob) ([]*CLIUpgradeJob, error) {
		now := time.Now()
		jobs = dropExpiredCLIUpgradeJobs(jobs, now)
		applyCLIUpgradeTimeouts(jobs, now)
		for _, job := range jobs {
			if job.RuntimeID == runtimeID && job.CLI == cli && !cliUpgradeTerminal(job.Status) {
				cp := *job
				created = &cp
				return jobs, nil
			}
		}
		job := &CLIUpgradeJob{
			ID:              randomID(),
			RuntimeID:       runtimeID,
			CLI:             cli,
			InitiatorUserID: initiatorUserID,
			Status:          CLIUpgradeQueued,
			CreatedAt:       now,
			UpdatedAt:       now,
			QueuedAt:        now,
		}
		cp := *job
		created = &cp
		return append(jobs, job), nil
	})
	return created, err
}

func (s *RedisCLIUpgradeStore) Get(ctx context.Context, id string) (*CLIUpgradeJob, error) {
	runtimeID, err := s.rdb.Get(ctx, cliUpgradeJobKey(id)).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var found *CLIUpgradeJob
	err = s.mutate(ctx, runtimeID, func(jobs []*CLIUpgradeJob) ([]*CLIUpgradeJob, error) {
		now := time.Now()
		jobs = dropExpiredCLIUpgradeJobs(jobs, now)
		applyCLIUpgradeTimeouts(jobs, now)
		for _, job := range jobs {
			if job.ID == id {
				cp := *job
				found = &cp
				break
			}
		}
		return jobs, nil
	})
	return found, err
}

func (s *RedisCLIUpgradeStore) HasPending(ctx context.Context, runtimeID string) (bool, error) {
	jobs, err := s.load(ctx, runtimeID)
	if err != nil || len(jobs) == 0 {
		return false, err
	}
	before := cliUpgradeTimeoutFingerprint(jobs)
	now := time.Now()
	applyCLIUpgradeTimeouts(jobs, now)
	if cliUpgradeTimeoutFingerprint(jobs) != before {
		if err := s.mutate(ctx, runtimeID, func(current []*CLIUpgradeJob) ([]*CLIUpgradeJob, error) {
			current = dropExpiredCLIUpgradeJobs(current, time.Now())
			applyCLIUpgradeTimeouts(current, time.Now())
			return current, nil
		}); err != nil {
			return false, err
		}
		jobs, err = s.load(ctx, runtimeID)
		if err != nil {
			return false, err
		}
	}
	for _, job := range jobs {
		if job.RuntimeID == runtimeID && job.Status == CLIUpgradeQueued {
			return true, nil
		}
	}
	return false, nil
}

func (s *RedisCLIUpgradeStore) PopPending(ctx context.Context, runtimeID string) (*CLIUpgradeJob, error) {
	var popped *CLIUpgradeJob
	err := s.mutate(ctx, runtimeID, func(jobs []*CLIUpgradeJob) ([]*CLIUpgradeJob, error) {
		now := time.Now()
		jobs = dropExpiredCLIUpgradeJobs(jobs, now)
		applyCLIUpgradeTimeouts(jobs, now)
		for _, job := range jobs {
			if job.RuntimeID == runtimeID && cliUpgradeInFlight(job.Status) {
				return jobs, nil
			}
		}
		var oldest *CLIUpgradeJob
		for _, job := range jobs {
			if job.RuntimeID == runtimeID && job.Status == CLIUpgradeQueued {
				if oldest == nil || job.CreatedAt.Before(oldest.CreatedAt) {
					oldest = job
				}
			}
		}
		if oldest == nil {
			return jobs, nil
		}
		oldest.Status = CLIUpgradeDraining
		started := now
		oldest.DispatchedAt = &started
		oldest.UpdatedAt = now
		cp := *oldest
		popped = &cp
		return jobs, nil
	})
	return popped, err
}

func (s *RedisCLIUpgradeStore) SetStatus(ctx context.Context, id, status, errMsg, current string) error {
	runtimeID, err := s.rdb.Get(ctx, cliUpgradeJobKey(id)).Result()
	if errors.Is(err, redis.Nil) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.mutate(ctx, runtimeID, func(jobs []*CLIUpgradeJob) ([]*CLIUpgradeJob, error) {
		var job *CLIUpgradeJob
		for _, candidate := range jobs {
			if candidate.ID == id {
				job = candidate
				break
			}
		}
		if job == nil || cliUpgradeTerminal(job.Status) {
			return jobs, nil
		}
		prev := job.Status
		now := time.Now()
		switch status {
		case CLIUpgradeQueued:
			job.Status = CLIUpgradeQueued
			job.DispatchedAt = nil
			job.Error = ""
			job.QueuedAt = now
		case CLIUpgradeDraining, CLIUpgradeUpgrading, CLIUpgradeSucceeded, CLIUpgradeFailed:
			job.Status = status
		default:
			return nil, errors.New("invalid cli upgrade status")
		}
		if status != CLIUpgradeQueued {
			if errMsg != "" {
				job.Error = errMsg
			}
			if current != "" {
				job.Current = current
			}
		}
		job.UpdatedAt = now
		if cliUpgradeInFlight(prev) && !cliUpgradeInFlight(job.Status) {
			for _, other := range jobs {
				if other.RuntimeID == job.RuntimeID && other.ID != job.ID && other.Status == CLIUpgradeQueued {
					other.QueuedAt = now
					other.UpdatedAt = now
				}
			}
		}
		return jobs, nil
	})
}

func (s *RedisCLIUpgradeStore) load(ctx context.Context, runtimeID string) ([]*CLIUpgradeJob, error) {
	raw, err := s.rdb.Get(ctx, cliUpgradeRuntimeKey(runtimeID)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return decodeCLIUpgradeBlob(raw)
}

func (s *RedisCLIUpgradeStore) mutate(ctx context.Context, runtimeID string, fn func([]*CLIUpgradeJob) ([]*CLIUpgradeJob, error)) error {
	key := cliUpgradeRuntimeKey(runtimeID)
	ttl := int(cliUpgradeStoreRetention.Seconds())
	if ttl < 1 {
		ttl = 1
	}
	var lastErr error
	for attempt := 0; attempt < cliUpgradeCASRetries; attempt++ {
		raw, err := s.rdb.Get(ctx, key).Bytes()
		if errors.Is(err, redis.Nil) {
			raw = nil
			err = nil
		}
		if err != nil {
			return err
		}
		jobs, err := decodeCLIUpgradeBlob(raw)
		if err != nil {
			return err
		}
		prevIDs := make(map[string]struct{}, len(jobs))
		for _, job := range jobs {
			prevIDs[job.ID] = struct{}{}
		}
		next, err := fn(jobs)
		if err != nil {
			return err
		}
		body, setKeys, delKeys, err := encodeCLIUpgradeMutation(runtimeID, next, prevIDs)
		if err != nil {
			return err
		}
		keys := make([]string, 0, 1+len(setKeys)+len(delKeys))
		keys = append(keys, key)
		args := make([]interface{}, 0, 4+len(setKeys))
		expected := ""
		if len(raw) > 0 {
			expected = string(raw)
		}
		args = append(args, expected, body, ttl, len(setKeys))
		for _, item := range setKeys {
			keys = append(keys, item.key)
			args = append(args, item.value)
		}
		keys = append(keys, delKeys...)
		wrote, err := cliUpgradeCAS.Run(ctx, s.rdb, keys, args...).Int()
		if err != nil {
			lastErr = err
			continue
		}
		if wrote == 1 {
			return nil
		}
	}
	if lastErr != nil {
		return fmt.Errorf("persist cli upgrade jobs: %w", lastErr)
	}
	return errors.New("persist cli upgrade jobs: concurrent update")
}

type cliUpgradeIndexSet struct {
	key   string
	value string
}

func encodeCLIUpgradeMutation(runtimeID string, jobs []*CLIUpgradeJob, prevIDs map[string]struct{}) (string, []cliUpgradeIndexSet, []string, error) {
	kept := make(map[string]struct{}, len(jobs))
	stored := make([]cliUpgradeStoredJob, 0, len(jobs))
	sets := make([]cliUpgradeIndexSet, 0, len(jobs))
	for _, job := range jobs {
		if job == nil || job.ID == "" {
			continue
		}
		kept[job.ID] = struct{}{}
		stored = append(stored, storedCLIUpgradeJob(job))
		sets = append(sets, cliUpgradeIndexSet{key: cliUpgradeJobKey(job.ID), value: runtimeID})
	}
	var dels []string
	for id := range prevIDs {
		if _, ok := kept[id]; !ok {
			dels = append(dels, cliUpgradeJobKey(id))
		}
	}
	if len(stored) == 0 {
		return "", nil, dels, nil
	}
	body, err := json.Marshal(cliUpgradeBlob{Jobs: stored})
	if err != nil {
		return "", nil, nil, err
	}
	return string(body), sets, dels, nil
}

func decodeCLIUpgradeBlob(raw []byte) ([]*CLIUpgradeJob, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var blob cliUpgradeBlob
	if err := json.Unmarshal(raw, &blob); err != nil {
		return nil, fmt.Errorf("decode cli upgrade jobs: %w", err)
	}
	out := make([]*CLIUpgradeJob, 0, len(blob.Jobs))
	for _, stored := range blob.Jobs {
		out = append(out, stored.job())
	}
	return out, nil
}

func dropExpiredCLIUpgradeJobs(jobs []*CLIUpgradeJob, now time.Time) []*CLIUpgradeJob {
	if len(jobs) == 0 {
		return jobs
	}
	out := jobs[:0]
	for _, job := range jobs {
		if job == nil {
			continue
		}
		if now.Sub(job.CreatedAt) > cliUpgradeStoreRetention {
			continue
		}
		out = append(out, job)
	}
	return out
}

func cliUpgradeTimeoutFingerprint(jobs []*CLIUpgradeJob) string {
	var b strings.Builder
	for _, job := range jobs {
		if job == nil {
			continue
		}
		fmt.Fprintf(&b, "%s|%s|%d;", job.ID, job.Status, job.QueuedAt.UnixNano())
	}
	return b.String()
}
