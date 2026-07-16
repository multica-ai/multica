export function createMulticaApp({ appId, version }) {
  if (!appId || !version) throw new Error("appId and version are required");
  const pending = new Map();
  const receive = (event) => {
    const message = event.data;
    if (event.source !== window.parent || message?.type !== "multica.app-sdk.response" || message.appId !== appId || message.version !== version) return;
    const request = pending.get(message.requestId);
    if (!request) return;
    pending.delete(message.requestId);
    if (message.ok === true) request.resolve(message.value);
    else request.reject(new Error(message.error || "Mini-app request failed"));
  };
  window.addEventListener("message", receive);

  const request = (method, args = []) => new Promise((resolve, reject) => {
    const requestId = crypto.randomUUID();
    pending.set(requestId, { resolve, reject });
    window.parent.postMessage({ type: "multica.app-sdk.request", requestId, appId, version, method, args }, "*");
  });

  return {
    registry: {
      token: () => request("registry.token"),
    },
    storage: {
      get: (key) => request("storage.get", [key]),
      set: (key, value) => request("storage.set", [key, value]),
      delete: (key) => request("storage.delete", [key]),
    },
    connections: {
      call: (connectionId, tool, arguments_) => request("connections.call", [connectionId, tool, arguments_ ?? {}]),
    },
    workers: {
      invoke: (input) => request("workers.invoke", [input]),
    },
    views: {
      submit: (viewId, value, requestId) => request("views.submit", [viewId, value, requestId]),
      onInput: (handler) => {
        const receiveInput = (event) => {
          if (event.source === window.parent && event.data?.type === "multica.app-view.init") handler(event.data.input, event.data.request_id);
        };
        window.addEventListener("message", receiveInput);
        return () => window.removeEventListener("message", receiveInput);
      },
    },
  };
}
