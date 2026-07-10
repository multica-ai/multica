// CEREBRO (FIR-2698): Prometheus cost metrics no longer carry their own
// hardcoded price table — prices come from the database-backed model registry
// via the lookup hook below, wired at server startup. Net-new cerebro file
// (cerebro-zone carve-out via the _cerebro suffix).
package metrics

import "sync/atomic"

// registryPriceLookup resolves a raw model string (any alias/spelling) to a
// USD-per-Mtok price row. Set once at startup by the server wiring; nil until
// then, in which case usage is recorded as unpriced (the pre-existing
// behavior for unknown models).
var registryPriceLookup atomic.Pointer[func(model string) (ModelPrice, bool)]

// SetRegistryPriceLookup installs the model-registry lookup used by
// PriceForModelAlias.
func SetRegistryPriceLookup(fn func(model string) (ModelPrice, bool)) {
	if fn == nil {
		registryPriceLookup.Store(nil)
		return
	}
	registryPriceLookup.Store(&fn)
}

func registryPriceLookupFn() func(model string) (ModelPrice, bool) {
	p := registryPriceLookup.Load()
	if p == nil {
		return nil
	}
	return *p
}
