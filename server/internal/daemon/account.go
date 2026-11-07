package daemon

import (
	"context"
	"errors"
	"sync"
	"time"
)

// daemonAccount is the Multica user this daemon's token belongs to. Computer
// Use offers a task automatic app approval only when Desktop is signed in as
// this same user, so an unknown account offers tasks nothing.
type daemonAccount struct {
	mu sync.Mutex
	id string
}

const accountRetryInterval = time.Minute

// CurrentUserID returns the id of the user the client's token belongs to.
func (c *Client) CurrentUserID(ctx context.Context) (string, error) {
	var me struct {
		ID string `json:"id"`
	}
	if err := c.getJSON(ctx, "/api/me", &me); err != nil {
		return "", err
	}
	if me.ID == "" {
		return "", errors.New("/api/me returned no user id")
	}
	return me.ID, nil
}

func (d *Daemon) accountUserID() string {
	d.account.mu.Lock()
	defer d.account.mu.Unlock()
	return d.account.id
}

// trackAccount resolves the token's user once; the token only changes when the
// daemon restarts. Failures are retried until ctx ends.
func (d *Daemon) trackAccount(ctx context.Context) {
	for attempt := 0; ; attempt++ {
		id, err := d.client.CurrentUserID(ctx)
		if err == nil {
			d.account.mu.Lock()
			d.account.id = id
			d.account.mu.Unlock()
			return
		}
		if attempt == 0 {
			d.logger.Warn("daemon account unknown; agent tasks get no Computer Use until it resolves", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(accountRetryInterval):
		}
	}
}
