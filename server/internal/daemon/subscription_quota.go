package daemon

import (
	"context"
	"fmt"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func (d *Daemon) heartbeatRequest(runtimeID string) protocol.DaemonHeartbeatRequestPayload {
	return protocol.DaemonHeartbeatRequestPayload{
		RuntimeID: runtimeID, SupportsBatchImport: true, SupportsSubscriptionQuota: d.supportsSubscriptionQuota(runtimeID),
	}
}

func (d *Daemon) supportsSubscriptionQuota(runtimeID string) bool {
	rt := d.findRuntime(runtimeID)
	return rt != nil && rt.Provider == "codex" && rt.ProfileID == ""
}

func (d *Daemon) handleSubscriptionQuota(ctx context.Context, runtimeID, requestID string) {
	// Delivery is at least once. A repeated HTTP/WS acknowledgement must
	// not start a second app-server while this request is being processed.
	if _, exists := d.quotaRequests.LoadOrStore(requestID, struct{}{}); exists {
		return
	}
	reported := false
	defer func() {
		if reported {
			time.AfterFunc(protocol.SubscriptionQuotaRequestTTL, func() { d.quotaRequests.Delete(requestID) })
		} else {
			d.quotaRequests.Delete(requestID)
		}
	}()

	report := protocol.SubscriptionQuotaReport{Status: protocol.SubscriptionQuotaFailed}
	if d.supportsSubscriptionQuota(runtimeID) {
		if entry, ok := d.agents()["codex"]; ok {
			entry, _ = d.resolveAgentEntry(ctx, "codex", entry)
			snapshot, err := agent.ReadCodexSubscriptionQuota(ctx, agent.NewCommand(entry.Path, nil))
			switch {
			case err != nil:
				// Provider output can include account data. Keep it local to
				// the process and report only the coarse outcome.
				d.logger.Debug("Codex subscription quota read failed", "runtime_id", runtimeID)
			case snapshot == nil:
				report.Status = protocol.SubscriptionQuotaUnavailable
			default:
				report.Status = protocol.SubscriptionQuotaComplete
				report.Snapshot = snapshot
			}
		}
	}

	d.reportRuntimeResultWithRetry(ctx, protocol.PendingWorkKindQuota, runtimeID, requestID, func(ctx context.Context) error {
		path := fmt.Sprintf("/api/daemon/runtimes/%s/subscription-quota/%s/result", runtimeID, requestID)
		err := d.client.postJSON(ctx, path, report, nil)
		reported = err == nil
		return err
	})
}

func (d *Daemon) reportObservedSubscriptionQuota(ctx context.Context, runtimeID string, snapshot *protocol.SubscriptionQuotaSnapshot) {
	if snapshot == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	path := fmt.Sprintf("/api/daemon/runtimes/%s/subscription-quota/observation", runtimeID)
	if err := d.client.postJSON(ctx, path, snapshot, nil); err != nil {
		d.logger.Debug("subscription quota observation could not be saved", "runtime_id", runtimeID)
	}
}
