// Agent Office (FIR-1775) — secret masking for the versioned context bundle.
//
// The bundle keeps secret NAMES only (custom_env_keys), never values. But the
// free-form JSON fields — mcp_config, custom_args, runtime_config — can legitimately
// embed raw auth tokens (an MCP server's `Authorization: Bearer …` header, an
// `env: { API_TOKEN: … }` map, a connection URL with inline credentials). Agent
// Office surfaces these fields verbatim in the version history and the Propose-
// change modal, which turned a stored config value into a plaintext secret leak
// on the overview screen.
//
// This masks the VALUES of secret-looking entries before they are rendered or
// diffed, while keeping the structure (server names, URLs, tool lists) readable —
// the same principle as showing secret names without their values.

export const SECRET_MASK = "••••••";

// Object keys whose string value is a secret regardless of where it sits.
// Matched case-insensitively as a whole word / sub-token (so `api_key`,
// `apiKey`, `X-Api-Key`, `authToken`, `clientSecret`, `DB_PASSWORD` all hit).
const SENSITIVE_KEY_RE =
  /(authorization|bearer|api[\s._-]?key|access[\s._-]?key|secret|password|passwd|pwd|token|credential|private[\s._-]?key|client[\s._-]?secret|session[\s._-]?key|x[\s._-]?api[\s._-]?key)/i;

// Some keys contain a sensitive sub-word but are not secrets — don't mask those.
const SENSITIVE_KEY_ALLOWLIST_RE =
  /^(token[\s._-]?count|max[\s._-]?tokens|tokens?|password[\s._-]?policy|key[\s._-]?id|public[\s._-]?key|key[\s._-]?name|secret[\s._-]?name)$/i;

// String values that look like a secret no matter what key holds them:
// "Bearer xxx", common token prefixes (sk-, ghp_, xoxb-, AKIA…), or a URL that
// carries inline credentials (https://user:pass@host).
const SECRET_VALUE_RES: RegExp[] = [
  /\bbearer\s+\S+/i,
  /\b(sk|rk|pk)-[A-Za-z0-9_-]{8,}/,
  /\b(gh[pousr]|github_pat)_[A-Za-z0-9_]{8,}/,
  /\bxox[baprs]-[A-Za-z0-9-]{8,}/,
  /\bAKIA[0-9A-Z]{12,}/,
  /\bey[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]+/, // JWT
  /[a-z][a-z0-9+.-]*:\/\/[^/\s:@]+:[^/\s:@]+@/i, // scheme://user:pass@host
];

function isSensitiveKey(key: string): boolean {
  if (SENSITIVE_KEY_ALLOWLIST_RE.test(key)) return false;
  return SENSITIVE_KEY_RE.test(key);
}

function maskSecretInString(value: string): string {
  let out = value;
  for (const re of SECRET_VALUE_RES) {
    out = out.replace(new RegExp(re, re.flags.includes("g") ? re.flags : re.flags + "g"), SECRET_MASK);
  }
  return out;
}

/**
 * Returns a deep copy of `value` with secret-looking entries masked. Keys are
 * preserved; only the offending string values become {@link SECRET_MASK}.
 * Non-string secret values (a number/bool under a sensitive key) are masked too.
 * Pure and side-effect free — never mutates the input.
 */
export function maskSecretsDeep(value: unknown, parentKeyIsSensitive = false): unknown {
  if (typeof value === "string") {
    if (parentKeyIsSensitive) return SECRET_MASK;
    return maskSecretInString(value);
  }
  if (typeof value === "number" || typeof value === "boolean") {
    return parentKeyIsSensitive ? SECRET_MASK : value;
  }
  if (Array.isArray(value)) {
    return value.map((v) => maskSecretsDeep(v, parentKeyIsSensitive));
  }
  if (value && typeof value === "object") {
    const out: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(value as Record<string, unknown>)) {
      out[k] = maskSecretsDeep(v, parentKeyIsSensitive || isSensitiveKey(k));
    }
    return out;
  }
  return value;
}
