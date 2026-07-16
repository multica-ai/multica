export function createHostClient(options) {
  const baseUrl = String(options.baseUrl ?? "").replace(/\/$/, "");
  const fetchImpl = options.fetch ?? globalThis.fetch;

  const call = async (kind, body) => {
    if (!baseUrl || !options.grantToken) throw new Error("App host call failed");
    const response = await fetchImpl(`${baseUrl}/api/cerebro/apps-internal/host/${kind}`, {
      method: "POST",
      headers: { authorization: `Bearer ${options.grantToken}`, "content-type": "application/json" },
      body: JSON.stringify(body),
      signal: AbortSignal.timeout(options.timeoutMs ?? 10_000),
    });
    if (!response.ok) throw new Error("App host call failed");
    const raw = await response.text();
    if (Buffer.byteLength(raw) > 1 << 20) throw new Error("App host call failed");
    try {
      return JSON.parse(raw);
    } catch {
      throw new Error("App host call failed");
    }
  };

  return {
    registryCall: (operation, resourceId, config = {}, input = null) => call("registry", { operation, resource_id: resourceId, config, input }),
    connectionCall: (connectionId, tool, argumentsValue = {}) => call("connection", { connection_id: connectionId, tool, arguments: argumentsValue }),
    log: (...values) => console.log("mini-app", ...values),
  };
}
