//go:build !linux

package agent

import "testing"

// requireCursorBackgroundOwnership only probes Linux, whose kernel decides
// whether pidfd process-group signalling exists. Other platforms keep their own
// skips.
func requireCursorBackgroundOwnership(t *testing.T) { t.Helper() }
