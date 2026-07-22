package handler

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/cerebro/internalbrowserqa"
)

const internalBrowserJobTTL = 15 * time.Minute

type internalBrowserJob struct {
	WorkspaceID string
	AgentID     string
	App         string
	State       string
	Result      internalbrowserqa.Result
	Err         error
	CreatedAt   time.Time
}

type internalBrowserJobStore struct {
	mu   sync.Mutex
	jobs map[string]*internalBrowserJob
	now  func() time.Time
}

func newInternalBrowserJobStore() *internalBrowserJobStore {
	return &internalBrowserJobStore{jobs: make(map[string]*internalBrowserJob), now: time.Now}
}

func (s *internalBrowserJobStore) create(workspaceID, agentID, app string) (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	id := hex.EncodeToString(random)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()
	s.jobs[id] = &internalBrowserJob{WorkspaceID: workspaceID, AgentID: agentID, App: app, State: "pending", CreatedAt: s.now()}
	return id, nil
}

func (s *internalBrowserJobStore) complete(id string, result internalbrowserqa.Result, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if job, ok := s.jobs[id]; ok {
		job.Result, job.Err, job.State = result, err, "complete"
	}
}

func (s *internalBrowserJobStore) get(id, workspaceID, agentID string) (internalBrowserJob, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()
	job, ok := s.jobs[id]
	if !ok || job.WorkspaceID != workspaceID || job.AgentID != agentID {
		return internalBrowserJob{}, false
	}
	return *job, true
}

func (s *internalBrowserJobStore) pruneLocked() {
	cutoff := s.now().Add(-internalBrowserJobTTL)
	for id, job := range s.jobs {
		if job.CreatedAt.Before(cutoff) {
			delete(s.jobs, id)
		}
	}
}
