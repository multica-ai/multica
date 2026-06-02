/**
 * Client-side MCP config schema validation.
 *
 * Mirrors the server-side validation in `server/internal/mcpvalidate/validate.go`
 * so users get instant feedback before hitting Save. Two transport shapes are
 * accepted:
 *
 *  - **stdio** — `{ command: string, args?: string[], env?: Record<string,string> }`
 *  - **HTTP/SSE** — `{ type: "sse" | "streamable-http", url: string, headers?: Record<string,string> }`
 */

export type McpValidateResult =
  | { ok: true }
  | { ok: false; error: string };

export interface McpServerEntry {
  command?: string;
  args?: string[];
  env?: Record<string, string>;
  type?: "sse" | "streamable-http";
  url?: string;
  headers?: Record<string, string>;
}

export type McpServers = Record<string, McpServerEntry>;

/**
 * Validate a raw MCP config value (already parsed from JSON into an object).
 * Returns `{ ok: true }` when valid, or `{ ok: false, error }` with a
 * human-readable reason.
 */
export function validateMcpConfig(value: unknown): McpValidateResult {
  if (value === null || value === undefined) {
    return { ok: true };
  }

  if (typeof value !== "object" || Array.isArray(value)) {
    return { ok: false, error: "mcp_config must be a JSON object" };
  }

  const root = value as Record<string, unknown>;

  if (!("mcpServers" in root)) {
    return { ok: false, error: 'mcp_config must contain a "mcpServers" key' };
  }

  const servers = root.mcpServers;
  if (typeof servers !== "object" || servers === null || Array.isArray(servers)) {
    return { ok: false, error: '"mcpServers" must be a JSON object' };
  }

  for (const [name, entry] of Object.entries(
    servers as Record<string, unknown>,
  )) {
    const result = validateEntry(name, entry);
    if (!result.ok) return result;
  }

  return { ok: true };
}

function validateEntry(
  name: string,
  entry: unknown,
): McpValidateResult {
  if (typeof entry !== "object" || entry === null || Array.isArray(entry)) {
    return { ok: false, error: `mcpServers.${name}: must be a JSON object` };
  }

  const e = entry as Record<string, unknown>;

  if ("command" in e) {
    return validateStdioEntry(name, e);
  }
  if ("type" in e) {
    return validateHttpEntry(name, e);
  }

  return {
    ok: false,
    error: `mcpServers.${name}: must have "command" (stdio) or "type" (HTTP/SSE)`,
  };
}

function validateStdioEntry(
  name: string,
  entry: Record<string, unknown>,
): McpValidateResult {
  const cmd = entry.command;
  if (typeof cmd !== "string" || cmd === "") {
    return {
      ok: false,
      error: `mcpServers.${name}: "command" must be a non-empty string`,
    };
  }

  if ("args" in entry) {
    if (!Array.isArray(entry.args)) {
      return {
        ok: false,
        error: `mcpServers.${name}: "args" must be an array`,
      };
    }
    for (let i = 0; i < entry.args.length; i++) {
      if (typeof entry.args[i] !== "string") {
        return {
          ok: false,
          error: `mcpServers.${name}: args[${i}] must be a string`,
        };
      }
    }
  }

  if ("env" in entry) {
    const r = validateStringMap(name, "env", entry.env);
    if (!r.ok) return r;
  }

  return { ok: true };
}

function validateHttpEntry(
  name: string,
  entry: Record<string, unknown>,
): McpValidateResult {
  const typ = entry.type;
  if (typ !== "sse" && typ !== "streamable-http") {
    return {
      ok: false,
      error: `mcpServers.${name}: "type" must be "sse" or "streamable-http", got "${String(typ)}"`,
    };
  }

  if (!("url" in entry) || typeof entry.url !== "string" || entry.url === "") {
    return {
      ok: false,
      error: `mcpServers.${name}: "url" must be a non-empty string`,
    };
  }

  if ("headers" in entry) {
    const r = validateStringMap(name, "headers", entry.headers);
    if (!r.ok) return r;
  }

  return { ok: true };
}

function validateStringMap(
  entryName: string,
  field: string,
  value: unknown,
): McpValidateResult {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    return {
      ok: false,
      error: `mcpServers.${entryName}: "${field}" must be a JSON object`,
    };
  }
  for (const [k, v] of Object.entries(value as Record<string, unknown>)) {
    if (typeof v !== "string") {
      return {
        ok: false,
        error: `mcpServers.${entryName}: ${field}.${k} must be a string`,
      };
    }
  }
  return { ok: true };
}
