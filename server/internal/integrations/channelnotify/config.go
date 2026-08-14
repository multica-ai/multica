package channelnotify

import (
	"sort"
	"strings"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// ParseEnabledChannels turns the deployment allowlist into stable Channel
// types. Unknown values remain in the result so startup wiring can report a
// single missing-sender warning without coupling this package to adapters.
func ParseEnabledChannels(raw string) []channel.Type {
	seen := make(map[channel.Type]struct{})
	for _, part := range strings.Split(raw, ",") {
		value := strings.TrimSpace(strings.ToLower(part))
		if value == "" {
			continue
		}
		seen[channel.Type(value)] = struct{}{}
	}
	result := make([]channel.Type, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}
