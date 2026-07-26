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
  // GitHub OAuth / classic tokens (ghp_/gho_/ghu_/ghs_/ghr_)
  { re: /\b(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9_]{36,255}\b/g, replacement: "[REDACTED GITHUB TOKEN]" },
  // GitHub fine-grained PATs (github_pat_…, recommended since 2022)
  { re: /\bgithub_pat_[A-Za-z0-9_]{20,255}\b/g, replacement: "[REDACTED GITHUB TOKEN]" },
  // Google API keys (AIza…, e.g. Gemini / Maps / Firebase)
  { re: /\bAIza[0-9A-Za-z_-]{35}([^0-9A-Za-z_-]|$)/g, replacement: "[REDACTED GOOGLE API KEY]$1" },
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

export function redactSecrets(text: string): string {
  let result = text;
  for (const { re, replacement } of patterns) {
    result = result.replace(re, replacement);
  }
  return result;
}

export interface FormattedTaskError {
  summary: string;
  detail: string;
}

const taskErrorSummaryLimit = 240;

function clipSummary(value: string): string {
  return value.length > taskErrorSummaryLimit
    ? `${value.slice(0, taskErrorSummaryLimit - 3)}...`
    : value;
}

function asRecord(value: unknown): Record<string, unknown> | null {
  return value !== null && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null;
}

function structuredErrorMessage(value: unknown): string | null {
  const root = asRecord(value);
  if (!root) return null;

  const nested = root.error;
  if (typeof nested === "string") return nested;

  const error = asRecord(nested) ?? root;
  const message = ["message", "error_description", "detail"]
    .map((key) => error[key])
    .find((candidate): candidate is string =>
      typeof candidate === "string" && candidate.trim().length > 0,
    );
  if (!message) return null;

  const param = error.param;
  return typeof param === "string" &&
    param.length > 0 &&
    !message.includes(param)
    ? `${message} · ${param}`
    : message;
}

function parseStructuredError(
  text: string,
): { value: unknown; prefix: string } | null {
  const firstBrace = text.indexOf("{");
  const lastBrace = text.lastIndexOf("}");
  const candidates = [
    { json: text, prefix: "" },
    ...(firstBrace > 0 && lastBrace > firstBrace
      ? [{
          json: text.slice(firstBrace, lastBrace + 1),
          prefix: text.slice(0, firstBrace).trim(),
        }]
      : []),
  ];

  for (const candidate of candidates) {
    try {
      return { value: JSON.parse(candidate.json), prefix: candidate.prefix };
    } catch {
      // Plain-text provider errors are expected; try the next shape.
    }
  }
  return null;
}

/**
 * Make the persisted terminal task error safe and readable. Providers may
 * return either plain text or a stringified JSON error envelope.
 */
export function formatTaskError(
  rawError: string | null | undefined,
): FormattedTaskError | null {
  const redacted = redactSecrets(rawError?.trim() ?? "");
  if (!redacted) return null;

  const structured = parseStructuredError(redacted);
  if (structured) {
    const pretty = JSON.stringify(structured.value, null, 2);
    const message = structuredErrorMessage(structured.value);
    return {
      summary: clipSummary(
        ((message ?? structured.prefix) || "Task execution failed")
          .replace(/\s+/g, " ")
          .trim(),
      ),
      detail: structured.prefix
        ? `${structured.prefix}\n${pretty}`
        : pretty,
    };
  }

  return {
    summary: clipSummary(redacted.replace(/\s+/g, " ").trim()),
    detail: redacted,
  };
}
