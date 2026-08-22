// Hello Panel — the reference Multica surface.
//
// Plain ES module, no build step: the point is to show the contract, not a
// toolchain. It runs in a sandboxed iframe with an opaque origin, so there is
// no cookie, no localStorage and no access to the page around it. Everything it
// does goes through the host bridge.
//
// In a real plugin you would `import { multica } from "@multica/plugin-sdk"`.
// This file inlines a minimal client so it can be served as a single static
// file with nothing to install.

const pending = new Map();
let port = null;
let sequence = 0;

window.addEventListener("message", (event) => {
  const data = event.data;
  if (!data || data.type !== "multica:plugin-bridge-init" || !event.ports[0]) return;
  // Only the embedder may hand this frame a port, and only once. Sibling frames
  // are mutually opaque but `parent.frames[i]` is an allowed cross-origin
  // access, so another plugin on this page could otherwise deliver its own port
  // and become this surface's "host" — reading everything it writes. Origin is
  // useless here (a sandboxed frame sees "null"), so identity is the window
  // reference.
  if (event.source !== window.parent || port) return;
  port = event.ports[0];
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
  applyTheme(data.theme);
  start();
});

// Announce until the host answers with a port. One announcement is not enough
// in either direction: this frame can finish executing before the embedder's
// effect attaches its listener, and a host waiting on the iframe load event can
// miss that event entirely. Repeating makes the handshake independent of who is
// ready first — the one ordering neither side controls.
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
  // The host clamps this; asking for the content height is the normal case.
  port.postMessage({ id: `resize${Date.now()}`, kind: "ui.resize", height: document.body.scrollHeight + 16 });
}

const NOTE_KEY = "note";

async function start() {
  const root = document.getElementById("root");
  root.innerHTML = `
    <div style="padding:12px 14px; display:grid; gap:10px">
      <div id="who" style="color:var(--muted-foreground)">Loading…</div>
      <div id="title" style="font-weight:600"></div>
      <div style="display:flex; gap:8px">
        <input id="note" placeholder="A note only you can see"
               style="flex:1; padding:6px 8px; border:1px solid var(--border); border-radius:var(--radius,6px);
                      background:var(--background); color:var(--foreground)">
        <button id="save" style="padding:6px 10px">Save</button>
      </div>
      <button id="comment" style="padding:6px 10px; justify-self:start">Post a test comment</button>
      <div id="status" style="color:var(--muted-foreground); min-height:1.2em"></div>
    </div>`;

  const status = (text) => { root.querySelector("#status").textContent = text; resize(); };

  try {
    const context = await call("GET", "/context");
    root.querySelector("#who").textContent =
      `${context.user.name} · ${context.workspace.name}` + (context.issue ? ` · ${context.issue.identifier}` : "");

    const issue = await call("GET", `/issues/${encodeURIComponent(context.issue.id)}`);
    root.querySelector("#title").textContent = issue.title;

    // storage:user — per member, so another member opening the same panel on
    // the same issue sees their own note, not this one.
    const saved = await call("GET", `/storage/user/${NOTE_KEY}`).catch(() => null);
    if (saved) root.querySelector("#note").value = saved.value;

    root.querySelector("#save").onclick = async () => {
      try {
        await call("PUT", `/storage/user/${NOTE_KEY}`, { value: root.querySelector("#note").value });
        status("Saved.");
      } catch (error) {
        status(error.message);
      }
    };

    root.querySelector("#comment").onclick = async () => {
      try {
        // Lands as the signed-in user, marked with this plugin. If the admin
        // did not grant comments:write, this is a 403 that names the scope.
        await call("POST", `/issues/${encodeURIComponent(context.issue.id)}/comments`, {
          content: "Posted from Hello Panel.",
        });
        status("Comment posted.");
      } catch (error) {
        status(error.message);
      }
    };
  } catch (error) {
    status(error.message);
  }
  resize();
}
