package inbound

import (
	chaction "github.com/multica-ai/multica/server/internal/channel/action"
	"github.com/multica-ai/multica/server/internal/channel/port"
)

func toPortIntent(in chaction.Intent) port.InboundIntent {
	params := in.Params
	if params == nil {
		params = map[string]string{}
	}
	return port.InboundIntent{
		Kind:       port.IntentKind(in.Kind),
		Confidence: in.Confidence,
		Params:     params,
		Source:     port.IntentSource(in.Source),
	}
}
