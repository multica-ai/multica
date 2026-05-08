const CLI_VERSION_RE = /^v?\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/;

export function normalizeCliVersion(raw) {
  if (!raw) return null;
  const trimmed = String(raw).trim();
  if (!CLI_VERSION_RE.test(trimmed)) return null;
  return trimmed.replace(/^v/, "");
}
