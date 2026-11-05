package codegraph

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// BuildTimeout bounds cumulative active clone, index and publish time, excluding slot waits.
func BuildTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv("MULTICA_CODEGRAPH_BUILD_TIMEOUT_SECONDS"))
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds < 120 || seconds > 86400 {
		return 11 * time.Minute
	}
	return time.Duration(seconds) * time.Second
}
