export function redactLogText(text: string): string {
  return text
    .replace(/Bearer\s+[^\s,;"'}]+/gi, "Bearer [REDACTED]")
    .replace(
      /((?:token|password|secret|api_key)["']?\s*[:=]\s*["']?)[^\s,"'}]+/gi,
      "$1[REDACTED]",
    );
}
