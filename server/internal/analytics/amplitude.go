package analytics

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultQueueSize    = 1024
	defaultBatchSize    = 64
	defaultFlushEvery   = 10 * time.Second
	defaultFlushTimeout = 5 * time.Second
)

// AmplitudeConfig configures the live Amplitude client.
type AmplitudeConfig struct {
	APIKey string
	Host   string // e.g. "https://api2.amplitude.com" (default)

	// Optional overrides. Zero values fall back to sensible defaults.
	QueueSize  int
	BatchSize  int
	FlushEvery time.Duration
	HTTPClient *http.Client
}

// AmplitudeClient ships events to Amplitude's HTTP V2 API. It enqueues events
// into a bounded buffer (non-blocking Capture) and flushes them from a
// background worker.
type AmplitudeClient struct {
	cfg  AmplitudeConfig
	ch   chan Event
	done chan struct{}
	wg   sync.WaitGroup

	dropped atomic.Uint64 // events dropped because the queue was full
	sent    atomic.Uint64
	failed  atomic.Uint64
}

// NewAmplitudeClient starts the background flush worker. Caller must call Close
// on shutdown to drain pending events.
func NewAmplitudeClient(cfg AmplitudeConfig) *AmplitudeClient {
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = defaultQueueSize
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = defaultBatchSize
	}
	if cfg.FlushEvery <= 0 {
		cfg.FlushEvery = defaultFlushEvery
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: defaultFlushTimeout}
	}
	if cfg.Host == "" {
		cfg.Host = "https://api2.amplitude.com"
	}
	c := &AmplitudeClient{
		cfg:  cfg,
		ch:   make(chan Event, cfg.QueueSize),
		done: make(chan struct{}),
	}
	c.wg.Add(1)
	go c.run()
	return c
}

// Capture enqueues an event. Returns immediately; on a full queue the event
// is dropped and counted. Analytics must never block a request handler.
func (c *AmplitudeClient) Capture(e Event) {
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	select {
	case c.ch <- e:
	default:
		n := c.dropped.Add(1)
		// Log periodically — every 100 drops — so a broken pipe is visible but
		// doesn't spam logs under sustained load.
		if n%100 == 1 {
			slog.Warn("analytics: queue full, dropping event", "event", e.Name, "total_dropped", n)
		}
	}
}

// Close stops accepting events and drains whatever is already queued.
func (c *AmplitudeClient) Close() {
	close(c.done)
	c.wg.Wait()
	slog.Info("analytics: amplitude client closed",
		"sent", c.sent.Load(),
		"dropped", c.dropped.Load(),
		"failed", c.failed.Load(),
	)
}

func (c *AmplitudeClient) run() {
	defer c.wg.Done()
	ticker := time.NewTicker(c.cfg.FlushEvery)
	defer ticker.Stop()

	batch := make([]Event, 0, c.cfg.BatchSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		c.send(batch)
		batch = batch[:0]
	}

	for {
		select {
		case e := <-c.ch:
			batch = append(batch, e)
			if len(batch) >= c.cfg.BatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-c.done:
			// Drain remaining events. The channel is not closed by Close() to
			// avoid racing with Capture, so we loop until it's empty.
			for {
				select {
				case e := <-c.ch:
					batch = append(batch, e)
					if len(batch) >= c.cfg.BatchSize {
						flush()
					}
				default:
					flush()
					return
				}
			}
		}
	}
}

// amplitudePayload mirrors the Amplitude HTTP V2 API /2/httpapi request shape.
type amplitudePayload struct {
	APIKey string           `json:"api_key"`
	Events []amplitudeEvent `json:"events"`
}

type amplitudeEvent struct {
	EventType       string         `json:"event_type"`
	UserID          string         `json:"user_id"`
	EventProperties map[string]any `json:"event_properties,omitempty"`
	UserProperties  map[string]any `json:"user_properties,omitempty"`
	Time            int64          `json:"time"` // epoch millis
}

func (c *AmplitudeClient) send(batch []Event) {
	items := make([]amplitudeEvent, 0, len(batch))
	for _, e := range batch {
		props := make(map[string]any, len(e.Properties)+1)
		for k, v := range e.Properties {
			props[k] = v
		}
		if e.WorkspaceID != "" {
			props["workspace_id"] = e.WorkspaceID
		}

		// Merge SetOnce and Set into user_properties using Amplitude's
		// special operations format.
		var userProps map[string]any
		if len(e.SetOnce) > 0 || len(e.Set) > 0 {
			userProps = make(map[string]any)
			if len(e.SetOnce) > 0 {
				userProps["$setOnce"] = e.SetOnce
			}
			if len(e.Set) > 0 {
				userProps["$set"] = e.Set
			}
		}

		items = append(items, amplitudeEvent{
			EventType:       e.Name,
			UserID:          e.DistinctID,
			EventProperties: props,
			UserProperties:  userProps,
			Time:            e.Timestamp.UnixMilli(),
		})
	}

	body, err := json.Marshal(amplitudePayload{APIKey: c.cfg.APIKey, Events: items})
	if err != nil {
		c.failed.Add(uint64(len(batch)))
		slog.Error("analytics: marshal batch", "error", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultFlushTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.Host+"/2/httpapi", bytes.NewReader(body))
	if err != nil {
		c.failed.Add(uint64(len(batch)))
		slog.Error("analytics: build request", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "*/*")

	resp, err := c.cfg.HTTPClient.Do(req)
	if err != nil {
		c.failed.Add(uint64(len(batch)))
		slog.Warn("analytics: send batch failed", "error", err, "events", len(batch))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		c.failed.Add(uint64(len(batch)))
		slog.Warn("analytics: amplitude rejected batch", "status", resp.StatusCode, "events", len(batch))
		return
	}
	c.sent.Add(uint64(len(batch)))
}
