//go:build !windows

package awakeclock

import "time"

// sleptSinceOrigin is zero because the runtime monotonic clock already
// excludes suspend on Linux and macOS.
func sleptSinceOrigin() time.Duration { return 0 }
