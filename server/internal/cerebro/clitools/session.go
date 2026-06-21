package clitools

// session.go — SessionState and the small arg/result helpers shared by the
// MCP tool handlers. FIR-1449: lifted out of cmd/multica (package main) so the
// in-app cerebro runtime can populate an *mcp.Server with the full CLI tool
// surface via RegisterTools, exactly like `multica mcp serve` does.

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/multica-ai/multica/server/internal/mcp"
)

// sessionEntry holds per-session counters tracked locally so that parallel
// agents reporting on different work_session_ids cannot clobber each other's
// seq counter or auto-naming flag.
type sessionEntry struct {
	issueID string
	seq     int
	named   bool
}

// SessionState tracks all known work sessions in this MCP process.
//
// The "ambient" session is what get_me reports and what restart-resume
// restores — it exists for single-agent convenience only. All routing of
// activity (report_activity, complete_work, ...) MUST be done via an
// explicit work_session_id parameter, otherwise parallel subagents sharing
// this MCP process will clobber each other.
type SessionState struct {
	mu sync.Mutex

	ambientID    string
	ambientIssue string

	// sessions is keyed by work_session_id.
	sessions map[string]*sessionEntry
}

// Ambient returns the ambient work session id and issue id.
func (s *SessionState) Ambient() (workSessionID, issueID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ambientID, s.ambientIssue
}

// AmbientWorkSessionID returns just the ambient work session id.
func (s *SessionState) AmbientWorkSessionID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ambientID
}

// SetAmbient sets the ambient work session.
func (s *SessionState) SetAmbient(workSessionID, issueID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ambientID = workSessionID
	s.ambientIssue = issueID
}

func (s *SessionState) clearAmbientIfMatches(workSessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ambientID == workSessionID {
		s.ambientID = ""
		s.ambientIssue = ""
		return true
	}
	return false
}

// Track records a work session in the per-session map. Existing entries are
// replaced (e.g. to reset seq on attach).
func (s *SessionState) Track(workSessionID, issueID string, seq int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessions == nil {
		s.sessions = make(map[string]*sessionEntry)
	}
	s.sessions[workSessionID] = &sessionEntry{issueID: issueID, seq: seq}
}

// nextSeq returns and increments the seq counter for the given work session.
// If the session is not yet tracked, it is created with seq starting at 1.
func (s *SessionState) nextSeq(workSessionID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessions == nil {
		s.sessions = make(map[string]*sessionEntry)
	}
	e, ok := s.sessions[workSessionID]
	if !ok {
		e = &sessionEntry{}
		s.sessions[workSessionID] = e
	}
	e.seq++
	return e.seq
}

// markNamed sets the named flag and returns true if the call was the first
// to set it. Used by report_activity to auto-name the session on first
// activity.
func (s *SessionState) markNamed(workSessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessions == nil {
		s.sessions = make(map[string]*sessionEntry)
	}
	e, ok := s.sessions[workSessionID]
	if !ok {
		e = &sessionEntry{}
		s.sessions[workSessionID] = e
	}
	if e.named {
		return false
	}
	e.named = true
	return true
}

// forget removes a tracked session. Called by complete_work.
func (s *SessionState) forget(workSessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, workSessionID)
}

// jsonText marshals v to JSON and returns a TextResult.
func jsonText(v any) (mcp.CallToolResult, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcp.ErrorResult("json encode error"), err
	}
	return mcp.TextResult(string(data)), nil
}

// requireString extracts a required string argument.
func requireString(args map[string]any, key string) (string, error) {
	v, ok := args[key]
	if !ok {
		return "", fmt.Errorf("missing required parameter: %s", key)
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return "", fmt.Errorf("parameter %s must be a non-empty string", key)
	}
	return s, nil
}

// optString extracts an optional string argument.
func optString(args map[string]any, key string) string {
	v, ok := args[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

// optInt extracts an optional int argument (JSON numbers come as float64).
func optInt(args map[string]any, key string, defaultVal int) int {
	v, ok := args[key]
	if !ok {
		return defaultVal
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	}
	return defaultVal
}
