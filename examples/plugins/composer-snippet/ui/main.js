// A self-contained composer example: static content in, current draft only.
const port = globalThis.__multicaPluginBridgePortV2;
if (!(port instanceof MessagePort)) throw new Error("Multica surface bridge is unavailable");
delete globalThis.__multicaPluginBridgePortV2;

const snippet = "## Notes\n\n- ";
let sequence = 0;
const pending = new Map();

port.onmessage = ({ data }) => {
  if (data?.kind === "theme") {
    for (const [name, value] of Object.entries(data.theme ?? {})) {
      document.documentElement.style.setProperty(name, value);
    }
    return;
  }

  const request = pending.get(data?.id);
  if (!request) return;
  pending.delete(data.id);
  if (data.ok) request.resolve(data.data);
  else request.reject(new Error(data.error ?? "Plugin call failed"));
};
port.start();

function insertSnippet() {
  const id = `r${++sequence}`;
  const button = document.getElementById("insert");
  const status = document.getElementById("status");
  button.disabled = true;
  status.textContent = "Inserting into draft…";

  const result = new Promise((resolve, reject) => {
    pending.set(id, { resolve, reject });
    port.postMessage({
      id,
      kind: "composer.insert",
      format: "markdown",
      text: snippet,
    });
  });

  result.then(
    () => { status.textContent = "Inserted into draft. Close this dialog to review it."; },
    (error) => {
      const message = error instanceof Error ? error.message : "Insertion failed";
      status.textContent = `${message} Close this dialog and try again.`;
    },
  );
}

const root = document.getElementById("root");
root.innerHTML = `
  <main style="display:grid;gap:12px;padding:16px;color:var(--foreground)">
    <div>
      <strong>Notes template</strong>
      <pre style="margin:8px 0 0;padding:10px;border-radius:var(--radius,6px);background:var(--muted);color:var(--foreground)"></pre>
    </div>
    <button id="insert" type="button"
      style="justify-self:start;padding:8px 12px;border:1px solid var(--border);border-radius:var(--radius,6px);background:var(--background);color:var(--foreground)">
      Insert template
    </button>
    <div id="status" role="status" style="min-height:1.2em;color:var(--muted-foreground)"></div>
  </main>`;
root.querySelector("pre").textContent = snippet;
document.getElementById("insert").addEventListener("click", insertSnippet);
