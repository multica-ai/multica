// CEREBRO (FIR-2698): the served price table is injected from the model
// registry; this fixture installs the rows the endpoint tests assert on.
package pricing

import (
	"os"
	"testing"

	corepricing "github.com/multica-ai/multica/server/pkg/pricing"
)

const testTableVersion = "registry-1.0.0"

func TestMain(m *testing.M) {
	corepricing.SetTable(map[string]corepricing.Pricing{
		"claude-opus-4-8": {InputCentsPerMtok: 500, OutputCentsPerMtok: 2500, CacheReadCentsPerMtok: 50, CacheWriteCentsPerMtok: 625},
		"claude-opus-4-1": {InputCentsPerMtok: 1500, OutputCentsPerMtok: 7500, CacheReadCentsPerMtok: 150, CacheWriteCentsPerMtok: 1875},
	}, "claude-opus-4-1", testTableVersion)
	os.Exit(m.Run())
}
