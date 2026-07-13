export type RegistryQL = {
  parameters?: Record<string, unknown>;
  filterGroup?: Record<string, unknown>;
  orderBy?: Array<{ field: string; direction: "asc" | "desc" }>;
  pagination?: { limit?: number; offset?: number; cursor?: string };
  aggregation?: Record<string, unknown>;
  input?: unknown;
};

export type AppToken = {
  key: string;
  session_id: string;
  expires_at: string;
};

export type AppClientOptions = {
  appId: string;
  appVersion: string;
  multicaBaseUrl?: string;
  registryBaseUrl?: string;
  fetch?: typeof globalThis.fetch;
};

const expirySkewMs = 60_000;

export function createAppClient(options: AppClientOptions) {
  const fetcher = options.fetch ?? globalThis.fetch;
  const multica = (options.multicaBaseUrl ?? "").replace(/\/$/, "");
  const registry = (options.registryBaseUrl ?? "https://registry.firtal.com/api/registry/v1").replace(/\/$/, "");
  let cachedToken: AppToken | undefined;

  async function token(): Promise<AppToken> {
    if (cachedToken && Date.parse(cachedToken.expires_at) > Date.now() + expirySkewMs) return cachedToken;
    cachedToken = await requestJSON<AppToken>(fetcher, `${multica}/api/cerebro/apps/${encodeURIComponent(options.appId)}/token`, {
      method: "POST",
      credentials: "include",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ version: options.appVersion }),
    });
    return cachedToken;
  }

  async function registryRequest<T>(path: string, body: unknown): Promise<T> {
    const personal = await token();
    return requestJSON<T>(fetcher, `${registry}${path}`, {
      method: "POST",
      headers: { authorization: `Bearer ${personal.key}`, "content-type": "application/json" },
      body: JSON.stringify(body),
    });
  }

  const execute = (kind: "data-sources" | "data-destinations", id: string, query: RegistryQL) =>
    registryRequest(`/${kind}/${encodeURIComponent(id)}/execute`, query);

  const multicaRequest = <T>(path: string, method: string, body?: unknown) =>
    requestJSON<T>(fetcher, `${multica}${path}`, {
      method,
      credentials: "include",
      headers: body === undefined ? undefined : { "content-type": "application/json" },
      body: body === undefined ? undefined : JSON.stringify(body),
    });

  return {
    auth: {
      token,
      forget: () => { cachedToken = undefined; },
    },
    data: {
      source: (id: string) => ({ execute: <T = unknown>(query: RegistryQL) => execute("data-sources", id, query) as Promise<T> }),
      destination: (id: string) => ({ execute: <T = unknown>(query: RegistryQL) => execute("data-destinations", id, query) as Promise<T> }),
      endpoint: <T = unknown>(request: RegistryQL) => registryRequest<T>("/endpoints/execute", request),
      execute: <T = unknown>(request: RegistryQL) => registryRequest<T>("/execute", request),
    },
    connections: {
      call: <T = unknown>(connectionId: string, tool: string, input: unknown) =>
        multicaRequest<T>(`/api/cerebro/connections/${encodeURIComponent(connectionId)}/call`, "POST", { tool, input }),
    },
    storage: {
      get: <T = unknown>(key: string) => multicaRequest<T>(`/api/cerebro/apps/${encodeURIComponent(options.appId)}/storage/${encodeURIComponent(key)}`, "GET"),
      set: <T = unknown>(key: string, value: unknown) => multicaRequest<T>(`/api/cerebro/apps/${encodeURIComponent(options.appId)}/storage/${encodeURIComponent(key)}`, "PUT", { value }),
      delete: (key: string) => multicaRequest<void>(`/api/cerebro/apps/${encodeURIComponent(options.appId)}/storage/${encodeURIComponent(key)}`, "DELETE"),
    },
    views: {
      submit: <T = unknown>(viewId: string, value: unknown) =>
        multicaRequest<T>(`/api/cerebro/apps/${encodeURIComponent(options.appId)}/views/${encodeURIComponent(viewId)}/submissions`, "POST", { value, version: options.appVersion }),
    },
  };
}

async function requestJSON<T>(fetcher: typeof globalThis.fetch, url: string, init: RequestInit): Promise<T> {
  const response = await fetcher(url, init);
  const raw = await response.text();
  let body: unknown;
  try {
    body = raw === "" ? undefined : JSON.parse(raw);
  } catch {
    body = raw;
  }
  if (!response.ok) {
    const detail = typeof body === "object" && body !== null && "error" in body ? String((body as { error: unknown }).error) : String(body ?? response.statusText);
    throw new Error(`HTTP ${response.status}: ${detail}`);
  }
  return body as T;
}
