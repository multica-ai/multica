// Release Checklist — a plugin with real state, not a hello-world.
//
// The workspace defines the checklist once in plugin settings; each issue keeps
// its own tick state in workspace-scoped plugin storage, and completing the list
// posts a sign-off comment authored by whoever ticked the last box.
//
// Everything below runs in a sandboxed iframe with an opaque origin: no cookies,
// no localStorage, no access to the page around it. State lives in
// multica.storage (server-side), and the comment goes through the host bridge on
// the user's own session.

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

/** Items come from workspace config, so one team edits them in one place. */
function parseItems(config) {
  return String(config.items ?? "")
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean);
}

/** State is per issue, keyed so two issues never share ticks. */
const stateKey = (issueId) => `checklist:${issueId}`;

/**
 * Ticks are stored as the item TEXT, not an index. If someone edits the
 * checklist in settings, an index-keyed state would silently re-point every
 * tick at a different item; keying by text loses the tick instead, which is the
 * honest outcome.
 */
function readState(raw) {
  if (!raw) return new Set();
  try {
    const parsed = JSON.parse(raw);
    return new Set(Array.isArray(parsed?.done) ? parsed.done : []);
  } catch {
    return new Set();
  }
}

function renderSignoff(template, items) {
  return String(template ?? "").replace("{{items}}", items.map((item) => `- ${item}`).join("\n"));
}

// --- rendering ------------------------------------------------------------

// Styles live here rather than in a stylesheet so the whole surface is one
// static file. Everything visual is expressed in the tokens the host pushes in,
// so the panel follows the product's theme without shipping its own palette.
const STYLES = `
.wrap { padding: 12px 14px; display: grid; gap: 10px; }
.head { display: flex; align-items: baseline; gap: 8px; }
.count { font-weight: 600; }
.muted { color: var(--muted-foreground); font-size: var(--text-caption, 12px); }
.list { list-style: none; margin: 0; padding: 0; display: grid; gap: 6px; }
.list label { display: flex; align-items: center; gap: 8px; cursor: pointer; }
.struck { text-decoration: line-through; color: var(--muted-foreground); }
.foot { display: flex; align-items: center; gap: 10px; border-top: 1px solid var(--border); padding-top: 10px; }
button {
  padding: 5px 10px; border-radius: var(--radius, 6px);
  border: 1px solid var(--border); background: var(--background); color: var(--foreground);
  font: inherit; cursor: pointer;
}
button:disabled { opacity: .5; cursor: not-allowed; }
`;

function installStyles() {
  const style = document.createElement("style");
  style.textContent = STYLES;
  document.head.appendChild(style);
}

const el = (id) => document.getElementById(id);

async function boot() {
  installStyles();
  const root = el("root");
  root.innerHTML = `<div class="wrap"><div id="status" class="muted">Loading…</div></div>`;
  resize();

  let context;
  try {
    context = await call("GET", "/context");
  } catch (error) {
    return fail(`Could not read context: ${error.message}`);
  }
  if (!context.issue) return fail("This panel only works on an issue.");

  const items = parseItems(context.config);
  if (items.length === 0) {
    return fail("No checklist items configured yet — set them in Settings → Plugins.");
  }

  let done;
  try {
    const stored = await call("GET", `/storage/workspace/${encodeURIComponent(stateKey(context.issue.id))}`);
    done = readState(stored?.value);
  } catch (error) {
    if (error.status !== 404) return fail(`Could not read saved state: ${error.message}`);
    done = new Set();
  }

  render(context, items, done);
}

function fail(message) {
  el("root").innerHTML = `<div class="wrap"><div class="muted">${escapeHtml(message)}</div></div>`;
  resize();
}

function escapeHtml(value) {
  return String(value).replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]);
}

function render(context, items, done) {
  const complete = items.every((item) => done.has(item));
  const strict = context.config.strict === true;

  el("root").innerHTML = `
    <div class="wrap">
      <div class="head">
        <span class="count">${done.size} / ${items.length}</span>
        <span class="muted">${escapeHtml(context.issue.identifier)}</span>
      </div>
      <ul class="list">
        ${items.map((item, index) => `
          <li>
            <label>
              <input type="checkbox" data-index="${index}" ${done.has(item) ? "checked" : ""}>
              <span class="${done.has(item) ? "struck" : ""}">${escapeHtml(item)}</span>
            </label>
          </li>`).join("")}
      </ul>
      <div class="foot">
        <button id="signoff" ${strict && !complete ? "disabled" : ""}>Post sign-off</button>
        <span id="status" class="muted">${complete ? "All items complete." : ""}</span>
      </div>
    </div>`;

  const status = (text) => { el("status").textContent = text; resize(); };

  for (const box of document.querySelectorAll('input[type="checkbox"]')) {
    box.addEventListener("change", async () => {
      const item = items[Number(box.dataset.index)];
      if (box.checked) done.add(item); else done.delete(item);
      try {
        // storage:workspace — the whole team sees the same ticks on this issue.
        await call("PUT", `/storage/workspace/${encodeURIComponent(stateKey(context.issue.id))}`,
          { value: JSON.stringify({ done: [...done] }) });
        render(context, items, done);
      } catch (error) {
        if (box.checked) done.delete(item); else done.add(item);
        box.checked = !box.checked;
        status(error.message);
      }
    });
  }

  el("signoff").addEventListener("click", async () => {
    const outstanding = items.filter((item) => !done.has(item));
    if (strict && outstanding.length > 0) return status(`${outstanding.length} item(s) still open.`);
    status("Posting…");
    try {
      // Lands as the signed-in user, recorded as posted through this plugin.
      await call("POST", `/issues/${encodeURIComponent(context.issue.id)}/comments`, {
        content: renderSignoff(context.config.signoff_template, items.filter((item) => done.has(item))),
      });
      status("Sign-off posted.");
    } catch (error) {
      // 403 here means the admin did not grant comments:write — a different fix
      // from a network failure, so it is surfaced verbatim.
      status(error.message);
    }
  });

  resize();
}
