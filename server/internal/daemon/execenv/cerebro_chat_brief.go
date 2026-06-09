package execenv

// cerebroChatBrief returns the Chat Reply subsection injected into the
// "Available Commands" block of the agent brief. Kept in its own
// cerebro-prefixed sibling file so runtime_config.go only needs a single
// marked call-site.
func cerebroChatBrief() string { // CEREBRO-PATCH(cerebro-chat-brief): TECH-3183 — Chat Reply section for Available Commands
	return `### Chat Reply (agent → user)

When you are assigned to a chat session you can send a message (and optionally attach files) back to the user directly, without waiting for the daemon to create a task.

- ` + "`multica chat session send <session-id> --content \"...\" [--attachment <path>]`" + ` — post an assistant message to the chat session. ` + "`--attachment`" + ` may be repeated for multiple files.

**Who can call this:** only the agent assigned to the session (` + "`session.agent_id == caller`" + `).
**How to get the session ID:** look for ` + "`MULTICA_CHAT_SESSION_ID`" + ` in your environment (set by the daemon when you run inside a chat task), or find it from the URL the user shared.

`
}
