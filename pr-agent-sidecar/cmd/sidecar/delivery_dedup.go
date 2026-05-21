package main

import (
	"sync"
	"time"
)

// DeliveryResult is what we cached when a particular X-GitHub-Delivery succeeded.
// On webhook retry GitHub re-sends with the same delivery ID; we return this
// instead of re-creating the Multica issue.
type DeliveryResult struct {
	MulticaIssueID         string
	MulticaIssueIdentifier string
}

type deliveryEntry struct {
	result    DeliveryResult
	expiresAt time.Time
}

// DeliveryDedup is an in-memory cache keyed on X-GitHub-Delivery. Provides
// idempotency over GitHub's webhook retries (we have no server-side
// idempotency on /api/issues).
type DeliveryDedup struct {
	mu     sync.RWMutex
	items  map[string]deliveryEntry
	ttl    time.Duration
	stop   chan struct{}
	closed bool
}

func NewDeliveryDedup(ttl time.Duration) *DeliveryDedup {
	d := &DeliveryDedup{
		items: make(map[string]deliveryEntry),
		ttl:   ttl,
		stop:  make(chan struct{}),
	}
	go d.sweep()
	return d
}

func (d *DeliveryDedup) Lookup(deliveryID string) (DeliveryResult, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	e, ok := d.items[deliveryID]
	if !ok {
		return DeliveryResult{}, false
	}
	if time.Now().After(e.expiresAt) {
		return DeliveryResult{}, false
	}
	return e.result, true
}

func (d *DeliveryDedup) Store(deliveryID string, r DeliveryResult) {
	d.mu.Lock()
	d.items[deliveryID] = deliveryEntry{result: r, expiresAt: time.Now().Add(d.ttl)}
	d.mu.Unlock()
}

func (d *DeliveryDedup) Len() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.items)
}

func (d *DeliveryDedup) Close() {
	d.mu.Lock()
	if !d.closed {
		d.closed = true
		close(d.stop)
	}
	d.mu.Unlock()
}

func (d *DeliveryDedup) sweep() {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-d.stop:
			return
		case <-t.C:
			d.sweepOnce(time.Now())
		}
	}
}

func (d *DeliveryDedup) sweepOnce(now time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for k, v := range d.items {
		if now.After(v.expiresAt) {
			delete(d.items, k)
		}
	}
}
