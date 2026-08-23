// The human side of Deploy Sentinel: an issue panel that answers "what changed"
// without making anybody leave the issue.
//
// The host renders the surrounding document and injects its own theme, so this
// file is only the behaviour. It runs in a sandboxed iframe with an opaque
// origin: no host cookies, no host storage, and no credential of any kind. Every
// call below goes over the bridge, where the host re-issues it as the signed-in
// user and checks the scopes this plugin was granted.
//
// A surface entry is ONE script with no module graph. Multica stores and serves
// the published artifact and serves it in a generated content document, so
// there is no module graph for a static `import` to resolve against — and that is the
// point: a surface cannot reach its author's server just by loading. In a real
// plugin you would bundle `@multica/plugin-sdk` in; this file inlines the few
// calls it needs so the example stays readable and has nothing to install.

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
resize(360);
correlate();

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

function resize(height) {
  port.postMessage({ id: `resize${Date.now()}`, kind: "ui.resize", height });
}

// One of THIS plugin's own hooks, over the `ui` trigger. The host signs the
// outbound call and applies the rate limit; the frame never sees the endpoint.
async function invokeHook(hookKey, input) {
  const result = await call("POST", `/hooks/${encodeURIComponent(hookKey)}`, { trigger: "ui", input });
  if (result?.status !== "ok") throw new Error(result?.error ?? "The hook did not answer.");
  return result.output;
}

const root = document.getElementById("root");

function render(html) {
  root.innerHTML = html;
  resize(document.body.scrollHeight + 16);
}

function escapeHTML(value) {
  return String(value).replace(/[&<>"']/g, (character) => ({
    "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
  })[character]);
}

function deployRow(deploy) {
  return `
    <li class="deploy">
      <div class="deploy-head">
        <code>${escapeHTML(deploy.id)}</code>
        <span class="muted">${escapeHTML(deploy.minutes_ago)}m ago · ${escapeHTML(deploy.author)}</span>
      </div>
      <div class="muted">${escapeHTML(deploy.commit)} — ${escapeHTML(deploy.blast_radius)}</div>
      <button data-rollback="${escapeHTML(deploy.id)}">Request rollback</button>
    </li>`;
}

async function correlate() {
  render(`<p class="muted">Looking for recent deploys…</p>`);
  try {
    const context = await call("GET", "/context");
    const issue = context.issue
      ? await call("GET", `/issues/${encodeURIComponent(context.issue.id)}`)
      : null;
    // The service name is a workspace-level setting rather than something
    // typed per issue: everyone investigating the same service should get the
    // same answer.
    const stored = await call("GET", "/storage/workspace/default_service").catch(() => null);
    const service = stored?.value ?? "checkout-api";

    const result = await invokeHook("correlate_deploys", { service, window_minutes: 120 });

    if (!result?.deploys?.length) {
      render(`<p>${escapeHTML(result?.summary ?? "No deploys found.")}</p>`);
      return;
    }
    render(`
      <p>${escapeHTML(result.summary)}</p>
      <ul class="deploys">${result.deploys.map(deployRow).join("")}</ul>
      <p class="muted">Issue: ${escapeHTML(issue?.title ?? "")}</p>
    `);
  } catch (error) {
    // A failing hook is the plugin author's own server being down. Say that,
    // rather than showing an empty panel that reads like "nothing deployed".
    render(`<p class="error">Deploy Sentinel could not reach its backend: ${escapeHTML(error.message)}</p>`);
  }
}

root.addEventListener("click", async (event) => {
  const deployId = event.target?.dataset?.rollback;
  if (!deployId) return;

  const reason = prompt("Why is this deploy suspected? The approver reads this — write the evidence, not the conclusion.");
  if (!reason) return;

  event.target.disabled = true;
  try {
    const result = await invokeHook("request_rollback", { deploy_id: deployId, reason });
    render(result.status === "filed"
      ? `<p>Filed <code>${escapeHTML(result.change_id)}</code>. ${escapeHTML(result.next_step)}</p>`
      : `<p class="error">${escapeHTML(result.reason)}</p>`);
  } catch (error) {
    render(`<p class="error">${escapeHTML(error.message)}</p>`);
  }
});
