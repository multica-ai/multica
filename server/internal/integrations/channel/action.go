package channel

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
)

// ActionHandler answers what a member does from a chat app outside the agent
// pipeline (spec 2.7, plan 9.11): a press on a signed button, and the /brief and
// /decisions commands. The handler resolves the platform user to a Multica
// member through the binding and applies the app's rules. An adapter only
// carries the platform's wire format in and renders what comes back.
type ActionHandler interface {
	// Press applies one button press. An error is an infrastructure failure; a
	// refusal (expired, not an owner, already decided) is a result, not an error.
	Press(ctx context.Context, p Press) (PressResult, error)
	// ActionKey returns the decision a button's data acts on, so an adapter can
	// remove every button of a settled decision together. It does not verify the
	// data, and returns "" for data that is not a signed action.
	ActionKey(data string) string
	// Command answers a chat command. handled is false when the sender has no
	// binding to a member of the workspace; the message then takes its ordinary
	// path, which is the binding prompt.
	Command(ctx context.Context, c Command) (reply Message, handled bool, err error)
}

// Sender is the platform user who acted, as the platform names them.
type Sender struct {
	ChannelType Type
	// InstallationID is the channel_installation row the message arrived on. The
	// binding is looked up within it.
	InstallationID pgtype.UUID
	ChannelUserID  string
	// IsBot says the platform marks the actor as a bot. An agent never presses a
	// button, so a bot press is refused.
	IsBot bool
}

// Press is one button press: the sender and the callback data the button carried.
type Press struct {
	Sender
	Data string
}

// PressResult is what the adapter does after a press.
type PressResult struct {
	// Toast is the short answer for the platform's callback answer, which the
	// presser sees on the button. It cannot carry a link.
	Toast string
	// Reply, when set, is posted in the chat the button is in, so a link can be
	// clicked. Empty when the toast says all there is.
	Reply string
	// Settled says the decision the button acted on is final (decided now, or
	// already decided). The adapter then removes every button for that decision.
	Settled bool
}

// Command is a chat command the sender typed to the bot.
type Command struct {
	Sender
	// Name is "brief" or "decisions".
	Name string
}

// Chat command names, as ParseChatCommand returns them.
const (
	CommandBrief     = "brief"
	CommandDecisions = "decisions"
)

// ParseChatCommand reads the chat command a message's cleaned text is. Only the
// first non-empty line counts, and the command must be the whole line: "/brief"
// and "/decisions" match, "/briefing" and a command inside a sentence do not.
// Case matches the /issue rule: the commands are lowercase.
func ParseChatCommand(text string) (string, bool) {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		switch line {
		case "/" + CommandBrief:
			return CommandBrief, true
		case "/" + CommandDecisions:
			return CommandDecisions, true
		}
		return "", false
	}
	return "", false
}
