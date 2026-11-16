// Schedule Pulse — reads the workspace-storage row the scheduled Hook writes.
//
// Plain ES module, no build step. The surface has no credential; it talks to
// Multica only through the host bridge.

const pending = new Map();
const port = globalThis.__multicaPluginBridgePortV2;
let sequence = 0;

if (!(port instanceof MessagePort)) throw new Error("Multica surface bridge is unavailable");
delete globalThis.__multicaPluginBridgePortV2;
port.onmessage = (message) => {
  const payload = message.data;
  if (payload?.kind === "theme") return applyTheme(payload.theme);
  const entry = pending.get(payload?.id);
  if (!entry) return;
  pending.delete(payload.id);
  if (payload.ok) entry.resolve(payload.data);
  else entry.reject(new Error(payload.error));
};
port.start();
start();

function applyTheme(theme) {
  for (const [name, value] of Object.entries(theme ?? {})) {
    document.documentElement.style.setProperty(name, value);
  }
}

function call(method, path, body) {
  const id = `r${++sequence}`;
  return new Promise((resolve, reject) => {
    pending.set(id, { resolve, reject });
    port.postMessage({ id, kind: "action", method, path, body });
  });
}

function resize() {
  port.postMessage({ id: `resize${Date.now()}`, kind: "ui.resize", height: document.body.scrollHeight + 16 });
}

async function start() {
  const root = document.getElementById("root");
  root.innerHTML = `
    <div style="padding:12px 14px; display:grid; gap:8px">
      <div style="font-weight:600">Schedule pulse</div>
      <div id="status" style="color:var(--muted-foreground)">Loading…</div>
      <dl id="pulse" style="display:none; margin:0; grid-template-columns:auto 1fr; gap:4px 12px"></dl>
    </div>`;

  const status = root.querySelector("#status");
  const list = root.querySelector("#pulse");
  try {
    const stored = await call("GET", "/storage/workspace/last_pulse");
    const pulse = JSON.parse(stored.value);
    status.textContent = "Multica has woken this plugin on a durable five-minute schedule.";
    list.style.display = "grid";
    list.innerHTML = `
      <dt style="color:var(--muted-foreground)">Count</dt><dd style="margin:0">${pulse.count ?? 0}</dd>
      <dt style="color:var(--muted-foreground)">Delivery</dt><dd style="margin:0; overflow-wrap:anywhere">${pulse.delivery_id ?? "—"}</dd>
      <dt style="color:var(--muted-foreground)">Planned</dt><dd style="margin:0">${pulse.last_planned_at ?? "—"}</dd>
      <dt style="color:var(--muted-foreground)">Attempt</dt><dd style="margin:0">${pulse.last_attempt ?? "—"}</dd>`;
  } catch {
    status.textContent = "No pulse yet. After install, Multica calls this plugin every five minutes and writes the first delivery here.";
  }
  resize();
}
