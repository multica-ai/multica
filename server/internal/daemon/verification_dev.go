//go:build verificationdev

package daemon

import (
	"context"
	"github.com/multica-ai/multica/server/internal/localconversation/service"
)

func verificationAgents() (map[string]AgentEntry, bool) {
	if !service.VerificationEnabled() {
		return nil, false
	}
	return map[string]AgentEntry{"claude": {Path: "/multica-verification/fake-provider"}, "codex": {Path: "/multica-verification/fake-provider"}}, true
}

// Verification runs the real local service without registering or claiming cloud work.
func (d *Daemon) runVerification(ctx context.Context) (bool, error) {
	if !service.VerificationEnabled() {
		return false, nil
	}
	if _, err := d.client.ListWorkspaces(ctx); err != nil {
		return true, err
	}
	d.ready.Store(true)
	d.logger.Info("verification runtime ready; cloud task claims and provider discovery disabled")
	<-ctx.Done()
	return true, nil
}
