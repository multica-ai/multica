package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestSkillProxy_FetchSkill verifies auth header and response parsing.
func TestSkillProxy_FetchSkill(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("X-Internal-Secret")
		if !strings.Contains(r.URL.Path, "/api/skills/skill-123") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Skill{
			ID:          "skill-123",
			Name:        "Test Skill",
			Description: "A test skill",
			Content:     "# Skill content",
			Type:        "prompt",
		})
	}))
	defer srv.Close()

	proxy := NewSkillProxy(srv.URL, "test-secret", 5*time.Minute, nil)

	skill, err := proxy.FetchSkill(context.Background(), "skill-123", "agent-abc")
	if err != nil {
		t.Fatalf("FetchSkill returned error: %v", err)
	}
	if gotAuth != "test-secret" {
		t.Errorf("auth header = %q, want %q", gotAuth, "test-secret")
	}
	if skill.ID != "skill-123" {
		t.Errorf("skill.ID = %q, want %q", skill.ID, "skill-123")
	}
	if skill.Name != "Test Skill" {
		t.Errorf("skill.Name = %q, want %q", skill.Name, "Test Skill")
	}
	if skill.Content != "# Skill content" {
		t.Errorf("skill.Content = %q, want %q", skill.Content, "# Skill content")
	}
	if skill.Type != "prompt" {
		t.Errorf("skill.Type = %q, want %q", skill.Type, "prompt")
	}
}

// TestSkillProxy_CachesResults verifies cache prevents second HTTP call.
func TestSkillProxy_CachesResults(t *testing.T) {
	var callCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Skill{
			ID:   "skill-1",
			Name: "Cached Skill",
		})
	}))
	defer srv.Close()

	proxy := NewSkillProxy(srv.URL, "secret", 5*time.Minute, nil)

	// First call hits the server
	s1, err := proxy.FetchSkill(context.Background(), "skill-1", "agent-1")
	if err != nil {
		t.Fatalf("first FetchSkill: %v", err)
	}
	if atomic.LoadInt32(&callCount) != 1 {
		t.Fatalf("expected 1 HTTP call after first fetch, got %d", atomic.LoadInt32(&callCount))
	}

	// Second call should be served from cache
	s2, err := proxy.FetchSkill(context.Background(), "skill-1", "agent-1")
	if err != nil {
		t.Fatalf("second FetchSkill: %v", err)
	}
	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("expected 1 HTTP call after cached fetch, got %d", atomic.LoadInt32(&callCount))
	}
	if s1.ID != s2.ID || s1.Name != s2.Name {
		t.Errorf("cached skill mismatch: %+v vs %+v", s1, s2)
	}

	// After invalidation, the next call should hit the server again
	proxy.InvalidateCache("skill-1")
	_, err = proxy.FetchSkill(context.Background(), "skill-1", "agent-1")
	if err != nil {
		t.Fatalf("third FetchSkill after invalidation: %v", err)
	}
	if atomic.LoadInt32(&callCount) != 2 {
		t.Errorf("expected 2 HTTP calls after invalidation, got %d", atomic.LoadInt32(&callCount))
	}
}

// TestSkillProxy_RateLimit verifies 60 calls OK, 61st fails, different agent unaffected.
func TestSkillProxy_RateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Skill{ID: "s1"})
	}))
	defer srv.Close()

	// Use a long cache TTL so cache doesn't expire during the test.
	// We use different skill IDs per call to prevent caching.
	proxy := NewSkillProxy(srv.URL, "secret", 5*time.Minute, nil)

	ctx := context.Background()
	agentA := "agent-a"
	agentB := "agent-b"

	// 60 calls for agentA should all succeed (use unique skill IDs to avoid cache hits)
	for i := 0; i < 60; i++ {
		skillID := "skill-" + string(rune('A'+i/26)) + string(rune('a'+i%26))
		_, err := proxy.FetchSkill(ctx, skillID, agentA)
		if err != nil {
			t.Fatalf("call %d for agentA failed: %v", i+1, err)
		}
	}

	// 61st call for agentA should be rate-limited
	_, err := proxy.FetchSkill(ctx, "skill-rate-limited", agentA)
	if err == nil {
		t.Fatal("expected rate limit error on 61st call for agentA, got nil")
	}
	if !strings.Contains(err.Error(), "rate limit") {
		t.Errorf("expected rate limit error, got: %v", err)
	}

	// agentB should still be fine (different rate limit bucket)
	_, err = proxy.FetchSkill(ctx, "skill-b-first", agentB)
	if err != nil {
		t.Fatalf("first call for agentB should succeed, got: %v", err)
	}
}

// TestSkillProxy_ListSkills verifies list endpoint and parsing.
func TestSkillProxy_ListSkills(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("X-Internal-Secret")
		if !strings.HasSuffix(r.URL.Path, "/api/skills") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]Skill{
			{ID: "s1", Name: "Skill One", Type: "prompt"},
			{ID: "s2", Name: "Skill Two", Type: "tool"},
		})
	}))
	defer srv.Close()

	proxy := NewSkillProxy(srv.URL, "list-secret", 5*time.Minute, nil)

	skills, err := proxy.ListSkills()
	if err != nil {
		t.Fatalf("ListSkills returned error: %v", err)
	}
	if gotAuth != "list-secret" {
		t.Errorf("auth header = %q, want %q", gotAuth, "list-secret")
	}
	if len(skills) != 2 {
		t.Fatalf("got %d skills, want 2", len(skills))
	}
	if skills[0].ID != "s1" || skills[0].Name != "Skill One" {
		t.Errorf("skills[0] = %+v, want {ID:s1 Name:Skill One}", skills[0])
	}
	if skills[1].ID != "s2" || skills[1].Type != "tool" {
		t.Errorf("skills[1] = %+v, want {ID:s2 Type:tool}", skills[1])
	}
}
