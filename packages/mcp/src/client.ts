// Minimal HTTP client for the Multica REST API. Self-contained — no
// imports from @multica/core. We deliberately pay the cost of duplicating
// a few request shapes here so the MCP package can ship as a standalone
// monorepo node, and a future PR could lift it into its own repo with
// a single `pnpm tsup` invocation.
//
// Authentication: every request carries `Authorization: Bearer <token>`.
// `X-Workspace-ID` is included on workspace-scoped endpoints; the caller
// passes per-request overrides so a tool like `multica_issue_create` can
// optionally target a non-default workspace.
//
// Errors: API 4xx/5xx responses are surfaced as `ApiError` with the body
// captured for the model. Network failures (DNS, connection) bubble up
// untouched so the MCP runtime sees the original exception.

import type { MulticaConfig } from "./config.js";

export class ApiError extends Error {
  readonly status: number;
  readonly body: unknown;
  constructor(status: number, message: string, body: unknown) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.body = body;
  }
}

export interface RequestOptions {
  /** Workspace override for this single call (defaults to config.defaultWorkspaceId). */
  workspaceId?: string | null;
  /** Path is appended to apiUrl as-is. Include leading slash. */
  query?: Record<string, string | number | boolean | undefined>;
  body?: unknown;
  headers?: Record<string, string>;
  /** Per-call timeout in ms. Default 30s. */
  timeoutMs?: number;
}

export class MulticaClient {
  constructor(private readonly cfg: MulticaConfig) {}

  get apiUrl(): string {
    return this.cfg.apiUrl;
  }

  get defaultWorkspaceId(): string | null {
    return this.cfg.defaultWorkspaceId;
  }

  async get<T>(path: string, opts: RequestOptions = {}): Promise<T> {
    return this.request<T>("GET", path, opts);
  }
  async post<T>(path: string, body: unknown, opts: RequestOptions = {}): Promise<T> {
    return this.request<T>("POST", path, { ...opts, body });
  }
  async patch<T>(path: string, body: unknown, opts: RequestOptions = {}): Promise<T> {
    return this.request<T>("PATCH", path, { ...opts, body });
  }
  async delete<T>(path: string, opts: RequestOptions = {}): Promise<T> {
    return this.request<T>("DELETE", path, opts);
  }

  private async request<T>(
    method: string,
    path: string,
    opts: RequestOptions,
  ): Promise<T> {
    const url = new URL(this.cfg.apiUrl + path);
    if (opts.query) {
      for (const [k, v] of Object.entries(opts.query)) {
        if (v === undefined) continue;
        url.searchParams.set(k, String(v));
      }
    }

    const headers: Record<string, string> = {
      Authorization: `Bearer ${this.cfg.token}`,
      Accept: "application/json",
      ...(opts.headers ?? {}),
    };
    const wsId = opts.workspaceId ?? this.cfg.defaultWorkspaceId;
    if (wsId) headers["X-Workspace-ID"] = wsId;
    if (opts.body !== undefined) headers["Content-Type"] = "application/json";

    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), opts.timeoutMs ?? 30_000);
    let res: Response;
    try {
      res = await fetch(url.toString(), {
        method,
        headers,
        body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
        signal: controller.signal,
      });
    } finally {
      clearTimeout(timeout);
    }

    // 204 / empty body — common for DELETE and write endpoints that
    // don't echo. Resolve with `null` cast to T so callers that expect
    // void don't have to special-case.
    if (res.status === 204) return null as T;

    const text = await res.text();
    let parsed: unknown = text;
    if (text) {
      try {
        parsed = JSON.parse(text);
      } catch {
        // Non-JSON response — leave as text and let the caller cope.
      }
    }

    if (!res.ok) {
      const message =
        (parsed && typeof parsed === "object" && "error" in parsed
          ? String((parsed as { error: unknown }).error)
          : null) ??
        (typeof parsed === "string" && parsed ? parsed : `${method} ${path} failed`);
      throw new ApiError(res.status, `${res.status} ${message}`, parsed);
    }
    return parsed as T;
  }
}
