// The human side of Deploy Sentinel: an issue panel that answers "what changed"
// without making anybody leave the issue.
//
// The host renders the surrounding document and injects its own theme, so this
// file is only the behaviour. It runs in a sandboxed iframe with an opaque
// origin: no host cookies, no host storage, and no credential of any kind. Every
// call below goes over the bridge, where the host re-issues it as the signed-in
// user and checks the scopes this plugin was granted.

import { multica } from "https://esm.sh/@multica/plugin-sdk@1";

const root = document.getElementById("root");

function render(html) {
  root.innerHTML = html;
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
    const issue = await multica.issue.get();
    // The service name is a workspace-level setting rather than something
    // typed per issue: everyone investigating the same service should get the
    // same answer.
    const service = (await multica.storage.workspace.get("default_service")) ?? "checkout-api";

    const result = await multica.hooks.invoke("correlate_deploys", {
      service,
      window_minutes: 120,
    });

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
    const result = await multica.hooks.invoke("request_rollback", { deploy_id: deployId, reason });
    render(result.status === "filed"
      ? `<p>Filed <code>${escapeHTML(result.change_id)}</code>. ${escapeHTML(result.next_step)}</p>`
      : `<p class="error">${escapeHTML(result.reason)}</p>`);
  } catch (error) {
    render(`<p class="error">${escapeHTML(error.message)}</p>`);
  }
});

multica.ui.resize(360);
correlate();
