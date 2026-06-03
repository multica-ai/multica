/**
 * Client-side mirror of the server's auto-suggested reminder text
 * (server/internal/cerebro/inbox/handler.go → suggestReminderText, FIR-2641).
 *
 * The comment reminder picker pre-fills the text field with this suggestion so
 * the user sees — and can edit — exactly what the server would have stored had
 * the text been left blank. Keep this in lockstep with the Go version:
 * whitespace collapsed to single spaces, trimmed, truncated to 140 code points
 * with an ellipsis, and the same generic fallback for an empty comment.
 */
const REMINDER_TEXT_MAX_RUNES = 140;

export function suggestReminderText(content: string): string {
  // strings.Fields + strings.Join(" ") on the Go side: split on any run of
  // whitespace, drop empties, rejoin with a single space.
  const snippet = content.split(/\s+/).filter(Boolean).join(" ");
  if (snippet === "") return "Reminder about this comment";

  // Spread iterates Unicode code points, matching Go's []rune slicing so a
  // multi-byte body truncates at the same boundary the server would pick.
  const runes = [...snippet];
  if (runes.length > REMINDER_TEXT_MAX_RUNES) {
    return runes.slice(0, REMINDER_TEXT_MAX_RUNES).join("").trimEnd() + "…";
  }
  return snippet;
}
