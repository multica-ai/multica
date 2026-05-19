package providers

import (
	feishuadapter "github.com/multica-ai/multica/server/internal/channel/adapter/feishu"
	"github.com/multica-ai/multica/server/internal/channel/provider"
)

// All returns the built-in channel provider factories. Runtime configuration
// decides which concrete connections are enabled; adding a provider should be
// contained to this catalog and the provider's adapter package.
func All() []provider.Factory {
	return []provider.Factory{
		feishuadapter.NewFactory(),
	}
}
