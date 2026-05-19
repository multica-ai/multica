// This file defines user-visible errors for channel agent turn selection.
package turn

// ChannelAgentUnavailableError carries a user-safe message for channel agent
// selection failures.
type ChannelAgentUnavailableError struct {
	Message string
	Reason  string
}

func (e *ChannelAgentUnavailableError) Error() string {
	if e == nil {
		return ""
	}
	if e.Reason != "" {
		return e.Reason
	}
	if e.Message != "" {
		return e.Message
	}
	return "channel agent unavailable"
}

// UserMessage returns the text that can be sent to the channel.
func (e *ChannelAgentUnavailableError) UserMessage() string {
	if e == nil {
		return ""
	}
	return e.Message
}
