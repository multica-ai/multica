// Package conversation owns the channel conversation, message, entity reference,
// and turn persistence boundary.
//
// Responsibilities:
//   - Persist external chat conversations as first-class channel containers.
//   - Persist inbound, outbound, agent, system, and notification messages.
//   - Persist business entity references and user-to-agent turn records.
//
// Boundaries:
//   - Does not parse user intent or infer business actions.
//   - Does not call issue, agent, or notification facades directly.
//   - Does not send messages to provider adapters.
package conversation
