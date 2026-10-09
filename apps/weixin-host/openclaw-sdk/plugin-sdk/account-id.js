// Account ids become file names in the plugin's state dir ("abc@im.bot" ->
// "abc-im-bot"), so the mapping must stay stable across releases.
export function normalizeAccountId(raw) {
  return String(raw ?? "")
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9_-]+/g, "-")
    .replace(/^-+|-+$/g, "");
}
