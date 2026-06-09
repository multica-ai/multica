/**
 * Client-side fallback for redacting sensitive information in agent output.
 * The server performs primary redaction; this is a safety net for display.
 */

const patterns: { re: RegExp; replacement: string }[] = [
  // AWS access key IDs
  { re: /\bAKIA[0-9A-Z]{16}\b/g, replacement: "[REDACTED AWS KEY]" },
  // AWS secret access keys
  { re: /(?:aws_secret_access_key|secret_?access_?key)\s*[=:]\s*[A-Za-z0-9/+=]{40}/gi, replacement: "[REDACTED AWS SECRET]" },
  // PEM private keys
  { re: /-----BEGIN[A-Z\s]*PRIVATE KEY-----[\s\S]*?-----END[A-Z\s]*PRIVATE KEY-----/g, replacement: "[REDACTED PRIVATE KEY]" },
  // GitHub tokens
  { re: /\b(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9_]{36,255}\b/g, replacement: "[REDACTED GITHUB TOKEN]" },
  // GitLab personal access tokens
  { re: /\bglpat-[A-Za-z0-9_-]{20,}\b/g, replacement: "[REDACTED GITLAB TOKEN]" },
  // OpenAI / Anthropic API keys
  { re: /\bsk-[A-Za-z0-9_-]{20,}\b/g, replacement: "[REDACTED API KEY]" },
  // Slack tokens
  { re: /\bxox[bporas]-[A-Za-z0-9-]{10,}\b/g, replacement: "[REDACTED SLACK TOKEN]" },
  // JWT tokens
  { re: /\bey[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b/g, replacement: "[REDACTED JWT]" },
  // Bearer tokens
  { re: /\bBearer\s+[A-Za-z0-9\-._~+/]+=*/gi, replacement: "Bearer [REDACTED]" },
  // Connection strings with embedded passwords
  { re: /(?:postgres|mysql|mongodb|redis|amqp)(?:ql)?:\/\/[^:\s]+:[^@\s]+@/gi, replacement: "[REDACTED CONNECTION STRING]@" },
  // Generic key=value secret env vars
  { re: /(?:API_KEY|API_SECRET|SECRET_KEY|SECRET|ACCESS_TOKEN|AUTH_TOKEN|PRIVATE_KEY|DATABASE_URL|DB_PASSWORD|DB_URL|REDIS_URL|PASSWORD|TOKEN)\s*[=:]\s*\S+/gi, replacement: "[REDACTED CREDENTIAL]" },
];

const sensitiveJSONKey =
  /(^|[_-])(api[_-]?key|api[_-]?secret|secret[_-]?key|secret|access[_-]?token|auth[_-]?token|private[_-]?key|database[_-]?url|db[_-]?password|db[_-]?url|redis[_-]?url|password|token|jwt)($|[_-])/i;

function applyTextPatterns(text: string): string {
  let result = text;
  for (const { re, replacement } of patterns) {
    result = result.replace(re, replacement);
  }
  return result;
}

function countObjectKeys(value: unknown): number {
  return value && typeof value === "object" && !Array.isArray(value)
    ? Object.keys(value as Record<string, unknown>).length
    : 0;
}

function redactJSONValue(value: unknown): [unknown, boolean] {
  if (typeof value === "string") {
    const redacted = applyTextPatterns(value);
    return [redacted, redacted !== value];
  }
  if (Array.isArray(value)) {
    let changed = false;
    const out = value.map((item) => {
      const [redacted, itemChanged] = redactJSONValue(item);
      changed = changed || itemChanged;
      return redacted;
    });
    return [out, changed];
  }
  if (!value || typeof value !== "object") return [value, false];

  const input = value as Record<string, unknown>;
  const out: Record<string, unknown> = {};
  let changed = false;
  let customEnvSeen = false;
  let customEnvKeyCount = 0;

  for (const [key, item] of Object.entries(input)) {
    if (key.toLowerCase() === "custom_env") {
      customEnvSeen = true;
      customEnvKeyCount = countObjectKeys(item);
      changed = true;
      continue;
    }
    if (sensitiveJSONKey.test(key)) {
      out[key] = "[REDACTED CREDENTIAL]";
      changed = true;
      continue;
    }
    const [redacted, itemChanged] = redactJSONValue(item);
    out[key] = redacted;
    changed = changed || itemChanged;
  }

  if (customEnvSeen) {
    out.has_custom_env = customEnvKeyCount > 0;
    out.custom_env_key_count = customEnvKeyCount;
    out.custom_env_redacted = true;
  }

  return [out, changed];
}

export function redactValue(value: unknown): unknown {
  return redactJSONValue(value)[0];
}

function redactStructuredJSON(text: string): string {
  const trimmed = text.trim();
  if (!trimmed || (trimmed[0] !== "{" && trimmed[0] !== "[")) return text;
  try {
    const parsed = JSON.parse(trimmed) as unknown;
    const [redacted, changed] = redactJSONValue(parsed);
    return changed ? JSON.stringify(redacted) : text;
  } catch {
    return text;
  }
}

export function redactSecrets(text: string): string {
  return applyTextPatterns(redactStructuredJSON(text));
}
