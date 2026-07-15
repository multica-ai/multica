export function createMulticaApp({ appId, version, runtimeBase = "/api/cerebro/apps-runtime" }) {
  if (!appId || !version) throw new Error("appId and version are required");
  const request = async (path, init = {}) => {
    const response = await fetch(path, { credentials: "include", ...init, headers: { "content-type": "application/json", ...(init.headers ?? {}) } });
    const body = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(body.error || "Mini-app request failed");
    return body;
  };
  return {
    registry: {
      token: () => request(`/api/cerebro/apps/${encodeURIComponent(appId)}/token`, { method: "POST", body: JSON.stringify({ version }) }),
    },
    storage: {
      get: (key) => request(`/api/cerebro/apps/${encodeURIComponent(appId)}/storage/${encodeURIComponent(key)}`),
      set: (key, value) => request(`/api/cerebro/apps/${encodeURIComponent(appId)}/storage/${encodeURIComponent(key)}`, { method: "PUT", body: JSON.stringify({ value }) }),
      delete: (key) => request(`/api/cerebro/apps/${encodeURIComponent(appId)}/storage/${encodeURIComponent(key)}`, { method: "DELETE" }),
    },
    connections: {
      call: (connectionId, tool, arguments_) => request(`/api/cerebro/connections/${encodeURIComponent(connectionId)}/call`, {
        method: "POST",
        body: JSON.stringify({ app_id: appId, version, tool, arguments: arguments_ ?? {} }),
      }),
    },
    workers: {
      invoke: (input) => request(`${runtimeBase}/workers/${encodeURIComponent(appId)}/${encodeURIComponent(version)}/invoke`, { method: "POST", body: JSON.stringify(input) }),
    },
    views: {
      submit: (viewId, value, requestId) => request(`/api/cerebro/apps/${encodeURIComponent(appId)}/views/${encodeURIComponent(viewId)}/submissions`, {
        method: "POST",
        body: JSON.stringify({ request_id: requestId, version, value }),
      }),
      onInput: (handler) => {
        const receive = (event) => {
          if (event.source === window.parent && event.data?.type === "multica.app-view.init") handler(event.data.input, event.data.request_id);
        };
        window.addEventListener("message", receive);
        return () => window.removeEventListener("message", receive);
      },
    },
  };
}
