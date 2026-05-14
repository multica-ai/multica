package inbound

import (
	chintent "github.com/multica-ai/multica/server/internal/channel/intent"
	"github.com/multica-ai/multica/server/internal/channel/port"
)

func toPortIntent(in chintent.Intent) port.InboundIntent {
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
