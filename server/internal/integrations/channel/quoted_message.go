package channel

import "strings"

// FormatQuotedMessage renders the one historical message selected by the user
// as ordinary Markdown, including its readable author when available. Adapters
// call it while the selected body is still separate from the current message;
// no consumer needs to scan user prose for an adapter-private envelope.
func FormatQuotedMessage(sender, body string) string {
	body = strings.TrimSpace(strings.ReplaceAll(body, "\r\n", "\n"))
	if body == "" {
		return ""
	}
	lines := strings.Split(body, "\n")
	for i := range lines {
		if lines[i] == "" {
			lines[i] = ">"
		} else {
			lines[i] = "> " + lines[i]
		}
	}
	quote := strings.Join(lines, "\n")
	if sender = strings.Join(strings.Fields(sender), " "); sender != "" {
		// A platform display name is plain text, not Markdown. Escaping it also
		// prevents names such as [Image] from becoming adapter media markers.
		sender = strings.NewReplacer(
			`\`, `\\`, "*", `\*`, "_", `\_`, "[", `\[`, "]", `\]`,
			"`", "\\`", "<", "&lt;", ">", "&gt;", "&", "&amp;",
		).Replace(sender)
		quote = "> **" + sender + ":**\n>\n" + quote
	}
	return quote
}
