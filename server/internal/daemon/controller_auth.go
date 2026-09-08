package daemon

import (
	"errors"
	"strings"
)

// SetControllerDaemonToken configures one pre-provisioned workspace/host
// credential before the client starts. It is distinct from controller authority
// and from the ordinary bootstrap login; never pass it to a provider process.
func (c *Client) SetControllerDaemonToken(token string) error {
	if token != "" && !strings.HasPrefix(token, "mdt_") {
		return errors.New("controller daemon credential must be a bound mdt_ token")
	}
	c.controllerDaemonToken = token
	return nil
}

func (c *Client) requestToken(path string) string {
	if c.controllerDaemonToken != "" && (strings.HasPrefix(path, "/api/daemon/tasks/") || strings.HasPrefix(path, "/api/daemon/runtimes/") || path == "/api/daemon/claim" || path == "/api/daemon/ws") {
		return c.controllerDaemonToken
	}
	return c.token
}
