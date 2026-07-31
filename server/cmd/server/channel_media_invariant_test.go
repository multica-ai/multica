package main

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/integrations/lark"
	"github.com/multica-ai/multica/server/internal/service"
)

// The reconciler's settle delay carries NO correctness weight (the ledger
// state machine does), but it must dwarf every media/HTTP budget in the
// pipeline so the reconciler never races a healthy in-flight operation's
// normal completion. This is the cross-package assertion of that invariant:
// if any budget grows past a tenth of the settle delay, this test forces the
// author to re-derive the margin instead of silently eroding it.
func TestChannelMediaSettleDwarfsEveryPipelineBudget(t *testing.T) {
	budgets := map[string]int64{
		"engine media timeout (download+upload+bind budget)": int64(engine.DefaultMediaTimeout),
		"lark resource download cap":                         int64(lark.DefaultResourceDownloadTimeout),
	}
	settle := int64(service.ChannelMediaReconcileSettleDelay)
	for name, budget := range budgets {
		if settle < 10*budget {
			t.Fatalf("settle delay %d must be >= 10x %s (%d)", settle, name, budget)
		}
	}
}
