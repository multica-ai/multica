package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

var errRuntimeSetChanged = errors.New("runtime set changed")

func (d *Daemon) taskWakeupLoop(ctx context.Context, taskWakeups chan<- struct{}) {
	backoff := time.Second

	for {
		runtimeIDs := d.allRuntimeIDs()
		if len(runtimeIDs) == 0 {
			if err := sleepWithContextOrRuntimeChange(ctx, 5*time.Second, d.runtimeSetCh); err != nil {
				return
			}
			continue
		}

		err := d.runTaskWakeupConnection(ctx, runtimeIDs, taskWakeups)
		if ctx.Err() != nil {
			return
		}
		if errors.Is(err, errRuntimeSetChanged) {
			backoff = time.Second
			continue
		}
		if err != nil {
			d.logger.Debug("task wakeup websocket unavailable; polling fallback remains active", "error", err, "retry_in", backoff)
		}

		if err := sleepWithContextOrRuntimeChange(ctx, jitterDuration(backoff), d.runtimeSetCh); err != nil {
			return
		}
		if backoff < 30*time.Second {
			backoff *= 2
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
		}
	}
}

func jitterDuration(d time.Duration) time.Duration {
	if d <= 0 {
		return d
	}
	spread := d / 5
	if spread <= 0 {
		return d
	}
	delta := time.Duration(rand.Int63n(int64(spread)*2+1)) - spread
	return d + delta
}

func (d *Daemon) runTaskWakeupConnection(ctx context.Context, runtimeIDs []string, taskWakeups chan<- struct{}) error {
	wsURL, err := taskWakeupURL(d.cfg.ServerBaseURL, runtimeIDs)
	if err != nil {
		return err
	}

	headers := http.Header{}
	if token := d.client.Token(); token != "" {
		headers.Set("Authorization", "Bearer "+token)
	}
	if d.client.platform != "" {
		headers.Set("X-Client-Platform", d.client.platform)
	}
	if d.client.version != "" {
		headers.Set("X-Client-Version", d.client.version)
	}
	if d.client.os != "" {
		headers.Set("X-Client-OS", d.client.os)
	}

	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.DialContext(ctx, wsURL, headers)
	if err != nil {
		return err
	}
	defer conn.Close()

	d.logger.Info("task wakeup websocket connected", "runtimes", len(runtimeIDs))
	signalTaskWakeup(taskWakeups)

	errCh := make(chan error, 1)
	go func() {
		errCh <- d.readTaskWakeupMessages(conn, taskWakeups)
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-d.runtimeSetCh:
		return errRuntimeSetChanged
	case err := <-errCh:
		return err
	}
}

func (d *Daemon) readTaskWakeupMessages(conn *websocket.Conn, taskWakeups chan<- struct{}) error {
	conn.SetReadLimit(64 * 1024)
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		var msg protocol.Message
		if err := json.Unmarshal(raw, &msg); err != nil {
			d.logger.Debug("task wakeup websocket invalid message", "error", err)
			continue
		}
		if msg.Type != protocol.EventDaemonTaskAvailable {
			continue
		}
		var payload protocol.TaskAvailablePayload
		if len(msg.Payload) > 0 {
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				d.logger.Debug("task wakeup websocket invalid payload", "error", err)
				continue
			}
		}
		if payload.RuntimeID != "" {
			d.logger.Debug("task wakeup received", "runtime_id", payload.RuntimeID, "task_id", payload.TaskID)
		}
		signalTaskWakeup(taskWakeups)
	}
}

func signalTaskWakeup(taskWakeups chan<- struct{}) {
	select {
	case taskWakeups <- struct{}{}:
	default:
	}
}

func taskWakeupURL(baseURL string, runtimeIDs []string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", fmt.Errorf("invalid daemon server URL: %w", err)
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("daemon server URL must use http, https, ws, or wss")
	}

	u.Path = strings.TrimRight(u.Path, "/") + "/api/daemon/ws"
	u.RawPath = ""
	q := u.Query()
	ids := append([]string(nil), runtimeIDs...)
	sort.Strings(ids)
	q.Set("runtime_ids", strings.Join(ids, ","))
	u.RawQuery = q.Encode()
	u.Fragment = ""
	return u.String(), nil
}

func sleepWithContextOrRuntimeChange(ctx context.Context, d time.Duration, runtimeSetCh <-chan struct{}) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-runtimeSetCh:
		return nil
	case <-timer.C:
		return nil
	}
}
