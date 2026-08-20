// Triage Notify — the surface half.
//
// This panel exists to demonstrate the `ui` trigger: a button the plugin drew
// itself, inside its own frame, that asks the HOST to call the plugin's backend.
//
// Note what it does NOT do. It does not fetch triage.example.com directly, even
// though that is the plugin's own server. Going through the host is what makes
// the call signed, rate limited, bounded to the granted net: domains, recorded
// for the admin, and issued a short-lived callback token. A surface that called
// its own backend would have none of that — and the user would have no record
// that it happened at all.

const pending = new Map();
let port = null;
let sequence = 0;

window.addEventListener("message", (event) => {
  const data = event.data;
  if (!data || data.type !== "multica:plugin-bridge-init" || !event.ports[0]) return;
  // Only the embedder may hand this frame a port, and only once. Sibling frames
  // are mutually opaque but `parent.frames[i]` is an allowed cross-origin
  // access, so another plugin on this page could otherwise deliver its own port
  // and become this surface's "host". Origin is useless here — a sandboxed
  // frame sees "null" — so identity is the window reference.
  if (event.source !== window.parent || port) return;
  port = event.ports[0];
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
  applyTheme(data.theme);
  boot();
});

(function announce(attempts) {
  if (port || attempts > 50) return;
  window.parent.postMessage({ type: "multica:plugin-surface-ready" }, "*");
  setTimeout(() => announce(attempts + 1), 120);
})(0);

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

let context = null;

async function boot() {
  context = await call("GET", "/context");
  render({ state: "idle" });
}

/**
 * The `ui` trigger.
 *
 * The host checks that this plugin's manifest actually declared `ui` on this
 * hook, then makes the call on our behalf. The result comes back here, so the
 * person who pressed the button sees what happened — which is the whole reason
 * ui and manual block and `event` does not.
 */
async function triage() {
  render({ state: "running" });
  try {
    const result = await call("POST", "/hooks/triage_issue", {
      trigger: "ui",
      issue_id: context.issue?.id,
      input: { issue_id: context.issue?.id, reason: "requested from the panel" },
    });
    if (result.status === "ok") {
      render({ state: "done", output: result.output });
    } else {
      // "refused" means the host never made the call — a scope nobody granted,
      // a rate limit, a disabled install. Saying "the service failed" would
      // send the reader to debug the wrong system.
      render({
        state: "error",
        message: result.status === "refused"
          ? `Not run: ${result.error}`
          : `Triage service failed: ${result.error}`,
      });
    }
  } catch (error) {
    render({ state: "error", message: error.message });
  }
}

function render(view) {
  const disabled = view.state === "running";
  document.body.innerHTML = `
    <div class="wrap">
      <p class="lede">${
        context?.issue
          ? "Ask the triage service to suggest an owner and priority for this issue."
          : "Open this panel on an issue to triage it."
      }</p>
      ${context?.issue ? `<button id="run" ${disabled ? "disabled" : ""}>${
        disabled ? "Triaging…" : "Triage this issue"
      }</button>` : ""}
      ${view.state === "done" ? `<p class="ok">Suggested: <strong>${escapeHtml(
        String(view.output?.priority ?? "unknown"),
      )}</strong> → <strong>${escapeHtml(String(view.output?.owner ?? "unknown"))}</strong>. A comment was posted.</p>` : ""}
      ${view.state === "error" ? `<p class="bad">${escapeHtml(view.message)}</p>` : ""}
    </div>
  `;
  document.getElementById("run")?.addEventListener("click", () => void triage());
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
  .lede { margin: 0; color: var(--muted-foreground, #666); }
  button {
    align-self: flex-start; padding: 6px 12px; border-radius: 6px; cursor: pointer;
    border: 1px solid var(--border, #d4d4d8); background: var(--background, #fff);
    color: inherit; font: inherit;
  }
  button:disabled { opacity: .6; cursor: default; }
  .ok, .bad { margin: 0; }
  .bad { color: var(--destructive, #b91c1c); }
`;
document.head.appendChild(style);
