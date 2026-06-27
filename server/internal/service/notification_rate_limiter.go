package service

import (
	"sync"
	"time"
)

// NotificationRateLimiter implements a per-user token bucket rate limiter
// to prevent notification storms during batch status changes.
//
// Design (OXY-583 §实现注意事项 #6):
//   - rate: 3 notifications/second
//   - burst: 5 (allow short bursts)
//   - 1-second sliding window
//   - Periodic cleanup of idle windows (every 10 minutes, idle > 5 min)
type NotificationRateLimiter struct {
	mu      sync.Mutex
	windows map[string]*userWindow

	rate  int // max tokens regenerated per second
	burst int // max burst size

	stopCh chan struct{}
	doneCh chan struct{}
}

type userWindow struct {
	tokens    int
	lastReset time.Time
}

// NewNotificationRateLimiter creates a rate limiter with the given rate and burst.
// A background goroutine periodically cleans up idle windows.
func NewNotificationRateLimiter(rate, burst int) *NotificationRateLimiter {
	if rate <= 0 {
		rate = 3
	}
	if burst <= 0 {
		burst = 5
	}
	rl := &NotificationRateLimiter{
		windows: make(map[string]*userWindow),
		rate:    rate,
		burst:   burst,
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
	}
	go rl.cleanup()
	return rl
}

// Allow checks whether a notification is allowed for the given userID.
// Returns true if the notification should proceed, false if rate-limited.
func (r *NotificationRateLimiter) Allow(userID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	w, ok := r.windows[userID]
	if !ok || now.Sub(w.lastReset) >= time.Second {
		// First request in this window or window expired — reset.
		r.windows[userID] = &userWindow{
			tokens:    r.burst - 1, // consume one token for this request
			lastReset: now,
		}
		return true
	}

	if w.tokens > 0 {
		w.tokens--
		return true
	}

	return false
}

// Stop signals the cleanup goroutine to exit and waits for it.
func (r *NotificationRateLimiter) Stop() {
	close(r.stopCh)
	<-r.doneCh
}

// cleanup periodically removes idle windows to prevent memory leaks.
// Runs every 10 minutes; removes windows idle for more than 5 minutes.
func (r *NotificationRateLimiter) cleanup() {
	defer close(r.doneCh)
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			r.mu.Lock()
			now := time.Now()
			for id, w := range r.windows {
				if now.Sub(w.lastReset) > 5*time.Minute {
					delete(r.windows, id)
				}
			}
			r.mu.Unlock()
		case <-r.stopCh:
			return
		}
	}
}
