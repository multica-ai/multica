// Jira Link — the issue panel.
//
// It reads the link from issue metadata (`jira.key`, `jira.url`, `jira.status`),
// which the handler writes. Linking and creating go through the plugin's own
// hooks, so the Jira credentials stay on the handler and never reach this frame.
// Unlinking needs no Jira call, so the panel deletes the metadata keys itself.

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
  else entry.reject(Object.assign(new Error(payload.error), { status: payload.status }));
};
port.start();
boot();

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
  port.postMessage({ id: `z${Date.now()}`, kind: "ui.resize", height: document.body.scrollHeight + 8 });
}

// --- business logic -------------------------------------------------------

const LINK_KEYS = ["jira.key", "jira.url", "jira.status"];
let issueId = null;
let link = null;

async function boot() {
  const context = await call("GET", "/context");
  issueId = context.issue?.id ?? null;
  await refresh();
}

async function refresh(message) {
  if (!issueId) return render({ message: "Open this panel on an issue to link it to Jira." });
  const issue = await call("GET", `/issues/${encodeURIComponent(issueId)}`);
  const metadata = issue.metadata ?? {};
  link = metadata["jira.key"]
    ? {
        key: metadata["jira.key"],
        // Anyone with issues:write can set metadata, so only follow https links.
        url: /^https:\/\//.test(String(metadata["jira.url"] ?? "")) ? metadata["jira.url"] : null,
        status: metadata["jira.status"],
      }
    : null;
  render({ message });
}

async function runHook(hook, input) {
  render({ busy: true });
  try {
    const result = await call("POST", `/hooks/${hook}`, { trigger: "ui", issue_id: issueId, input });
    if (result.status !== "ok") {
      // "refused" means the host never called the handler; anything else means
      // the handler or Jira failed. They need different fixes.
      const where = result.status === "refused" ? "Not run" : "Jira Link failed";
      return refresh(`${where}: ${result.error}`);
    }
    // The handler reports expected problems (an unknown key, no project set)
    // as an ordinary result with an error field.
    await refresh(result.output?.error);
  } catch (error) {
    await refresh(error.message);
  }
}

async function unlink() {
  render({ busy: true });
  try {
    // Each delete is idempotent, so if one fails the refreshed panel shows what
    // is left and Unlink can simply be pressed again.
    for (const key of LINK_KEYS) {
      await call("DELETE", `/issues/${encodeURIComponent(issueId)}/metadata/${encodeURIComponent(key)}`);
    }
    await refresh();
  } catch (error) {
    await refresh(error.message);
  }
}

function render({ busy = false, message } = {}) {
  const disabled = busy ? "disabled" : "";
  let body = "";
  if (issueId && link) {
    body = `
      <p>Linked to ${
        link.url
          ? `<a href="${escapeHtml(link.url)}" target="_blank" rel="noopener noreferrer"><strong>${escapeHtml(link.key)}</strong></a>`
          : `<strong>${escapeHtml(link.key)}</strong>`
      }${
        link.status ? ` · ${escapeHtml(link.status)}` : ""
      }</p>
      <div class="row"><button id="unlink" ${disabled}>Unlink</button></div>`;
  } else if (issueId) {
    body = `
      <div class="row">
        <input id="key" placeholder="OPS-42" aria-label="Jira issue key" ${disabled} />
        <button id="link" ${disabled}>Link</button>
      </div>
      <div class="row"><button id="create" ${disabled}>Create in Jira</button></div>`;
  }
  document.body.innerHTML = `
    <div class="wrap">
      ${body}
      ${busy ? `<p class="lede">Working…</p>` : ""}
      ${message ? `<p class="lede">${escapeHtml(message)}</p>` : ""}
    </div>
  `;
  document.getElementById("link")?.addEventListener("click", () => {
    const key = document.getElementById("key").value.trim();
    if (key) void runHook("link_issue", { key });
  });
  document.getElementById("create")?.addEventListener("click", () => void runHook("create_issue", {}));
  document.getElementById("unlink")?.addEventListener("click", () => void unlink());
  resize();
}

function escapeHtml(value) {
  return String(value).replace(/[&<>"']/g, (character) => ({
    "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
  })[character]);
}

const style = document.createElement("style");
style.textContent = `
  body { margin: 0; font: 13px/1.5 system-ui, sans-serif; color: var(--foreground, #111); }
  .wrap { padding: 12px 14px; display: flex; flex-direction: column; gap: 10px; }
  .row { display: flex; gap: 8px; }
  p { margin: 0; }
  a { color: inherit; }
  .lede { color: var(--muted-foreground, #666); }
  input, button {
    padding: 6px 10px; border-radius: 6px; font: inherit; color: inherit;
    border: 1px solid var(--border, #d4d4d8); background: var(--background, #fff);
  }
  button { cursor: pointer; }
  button:disabled, input:disabled { opacity: .6; cursor: default; }
`;
document.head.appendChild(style);
