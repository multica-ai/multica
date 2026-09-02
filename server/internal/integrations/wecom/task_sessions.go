package wecom

// task_sessions.go — remembering which chat, and which round, a task belongs to.
//
// task:message and task:progress both carry a task id and no chat session, so
// the session a bubble is keyed on has to be read back off the task row. One
// run posts dozens of tool messages and the answer never changes, so without
// this the bubble would cost a database read per tool call on a code path that
// runs inside the daemon's own HTTP request.
//
// The absence of a session is cached too, and that is the more important half.
// Issue runs and autopilot runs publish exactly the same events, have no chat
// session at all, and on most deployments outnumber chat runs — so "this task
// has no bubble" is the answer being looked up most often.
//
// The round travels with the session because ONE ROW answers both questions.
// chat_input_task_id is a column on the same agent_task_queue row: it holds a
// first attempt's own id and an auto-retry clone's PARENT id, which is the id
// the debounced flush bound the round under (roundTaker reads the same column
// for the endings). Reading it here costs nothing extra and is what keeps a
// clone's steps landing in the bubble its first attempt opened.

import (
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

const (
	// taskSessionTTL is memory hygiene rather than correctness: neither a
	// task's chat session nor its round ever changes, so an entry is only ever
	// stale in the sense that nobody will ask for it again. Comfortably past
	// the stream window, so no live bubble ever re-reads the row it already
	// read.
	taskSessionTTL = 10 * time.Minute

	// taskSessionFailTTL is how long a read that FAILED is remembered. A
	// failure is not an answer — the row may be perfectly readable a moment
	// later — so it is held only long enough to cover the batch that provoked
	// it. That is the whole point: a database slow enough to fail is a database
	// slow enough that asking it once per transcript message spends the
	// daemon's entire request.
	taskSessionFailTTL = 2 * time.Second

	// taskSessionMax bounds the map on a busy workspace. Well above the number
	// of tasks that can be in flight inside one TTL; hitting it means
	// something unusual, and dropping the oldest entries costs one query each.
	taskSessionMax = 512
)

// taskRound is where one run's transcript belongs: the chat session behind it,
// and the round it is part of.
type taskRound struct {
	// session is invalid for a task that has no chat session at all — an issue
	// or autopilot run — which is an answer worth remembering, not a miss.
	session pgtype.UUID

	// round is chat_input_task_id: this run's own id for a first attempt, its
	// parent's for an auto-retry clone. Empty when the row does not carry one,
	// which leaves the caller matching on the run's own id and nothing else.
	round string
}

// taskSessionEntry is one remembered answer. until is stamped per entry rather
// than derived from one TTL because the two kinds of answer are worth keeping
// for very different lengths of time.
type taskSessionEntry struct {
	round taskRound
	until time.Time
}

type taskSessionCache struct {
	mu     sync.Mutex
	byTask map[string]taskSessionEntry
	max    int
	ttl    time.Duration
	now    func() time.Time
}

func newTaskSessionCache() *taskSessionCache {
	return &taskSessionCache{
		byTask: make(map[string]taskSessionEntry),
		max:    taskSessionMax,
		ttl:    taskSessionTTL,
		now:    time.Now,
	}
}

// get returns what is known about a task and whether the question has been
// answered before. A hit whose session is invalid is a task known to have none
// — the caller must stop there rather than fall through to the database.
func (c *taskSessionCache) get(taskID string) (taskRound, bool) {
	if taskID == "" {
		return taskRound{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.byTask[taskID]
	if !ok {
		return taskRound{}, false
	}
	if !c.now().Before(entry.until) {
		delete(c.byTask, taskID)
		return taskRound{}, false
	}
	return entry.round, true
}

// put records an answer, including the answer "none".
func (c *taskSessionCache) put(taskID string, round taskRound) {
	c.record(taskID, round, c.ttl)
}

// putFailure records that the read did not work, so the rest of the batch stops
// re-asking. The caller reads it back as "no session", which is the right
// behaviour either way: with no session there is no bubble to write into.
func (c *taskSessionCache) putFailure(taskID string) {
	c.record(taskID, taskRound{}, taskSessionFailTTL)
}

func (c *taskSessionCache) record(taskID string, round taskRound, ttl time.Duration) {
	if taskID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if len(c.byTask) >= c.max {
		c.evictLocked(now)
	}
	c.byTask[taskID] = taskSessionEntry{round: round, until: now.Add(ttl)}
}

// evictLocked makes room. Expired entries go first; if that was not enough the
// map is emptied rather than half-sorted, because at this point the cache is
// not doing its job anyway and the cost of being wrong is one query per task.
// Caller holds c.mu.
func (c *taskSessionCache) evictLocked(now time.Time) {
	for k, e := range c.byTask {
		if !now.Before(e.until) {
			delete(c.byTask, k)
		}
	}
	if len(c.byTask) >= c.max {
		clear(c.byTask)
	}
}
