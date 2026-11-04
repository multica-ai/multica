// Explicit Electron regression check: isolated profile, fake data, no agent CLI or backend.
// Run after `node packages/tutti-agent-gui-host/scripts/build-e2e-harness.mjs <dir>`, with that directory.
import assert from "node:assert/strict";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { _electron as electron, expect } from "@playwright/test";
import ts from "typescript";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
// Resolve Electron from the desktop package that declares and installs it.
const desktopRequire = createRequire(join(root, "apps/desktop/package.json"));
const gui = resolve(process.argv[2] ?? join(root, "apps/web/public/tutti-agent-gui"), "index.html");
await readFile(gui); // Fail early if the production GUI has not been built.
const temporary = await mkdtemp(join(tmpdir(), "multica-agent-gui-file-"));
let application;
let page;
try {
  const helper = await readFile(join(root, "packages/tutti-agent-gui-host/e2e-harness/frame-messaging.ts"), "utf8");
  await writeFile(join(temporary, "frame-messaging.js"), ts.transpileModule(helper, {
    compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 },
  }).outputText);
  await writeFile(join(temporary, "host.html"), `<html><body>
    <input id="search" aria-label="Search conversations" placeholder="Search conversations">
    <iframe id="unrelated"></iframe><iframe id="gui" title="Agent GUI" style="width:100%;height:90vh"></iframe>
    <script type="module">
      import { isAgentGuiFrameMessage, agentGuiMessageTargetOrigin } from './frame-messaging.js';
      const gui = document.getElementById('gui');
      const workspace = { agents: [], sessions: [], local: true, executionTargets: [
        { id: 'local:codex', provider: 'codex', name: 'Codex', iconUrl: '', canInvoke: true, runtimeBound: true, runtimeOnline: true }
      ] };
      const snapshot = { ...workspace, workspaceId: 'smoke-workspace', workspaceSlug: 'smoke',
        userId: 'smoke-user', locale: 'en', theme: 'light', activeSessionId: null, selectedAgentId: 'local:codex', hostVisible: true, hostActive: true };
      window.evidence = { origin: location.origin, messages: [], rejectedSources: 0, requests: [], connected: false, rendered: false };
      window.addEventListener('message', event => {
        if (!isAgentGuiFrameMessage(event, gui.contentWindow)) {
          if (event.data?.type === 'multica:agent-gui:ready') window.evidence.rejectedSources++;
          return;
        }
        window.evidence.messages.push({ type: event.data?.type, origin: event.origin });
        if (event.data?.type === 'multica:agent-gui:rendered') window.evidence.rendered = true;
        if (event.data?.type !== 'multica:agent-gui:ready' || event.data.version !== 1) return;
        const channel = new MessageChannel();
        window.setHostActive = (active, newConversationSequence) => {
          snapshot.hostActive = active;
          if (newConversationSequence !== undefined) snapshot.newConversationRequest = { sequence: newConversationSequence, projectPath: null };
          channel.port1.postMessage({ type: 'event', event: { type: 'init', snapshot } });
        };
        window.appendPrompt = prompt => channel.port1.postMessage({
          type: 'event', event: { type: 'appendPrompt', sequence: 1, prompt }
        });
        channel.port1.onmessage = ({ data }) => {
          if (data.type !== 'request') return;
          window.evidence.requests.push(data.method);
          const result = data.method === 'workspace.load' ? workspace
            : data.method === 'models.list' ? { supported: true, models: [] } : null;
          channel.port1.postMessage({ type: 'response', id: data.id, result });
          window.evidence.connected = true;
        };
        channel.port1.start();
        gui.contentWindow.postMessage({ type: 'multica:agent-gui:connect', snapshot }, agentGuiMessageTargetOrigin(), [channel.port2]);
      });
      document.getElementById('unrelated').srcdoc = '<script>parent.postMessage({type:"multica:agent-gui:ready",version:1},"*")<\\/script>';
      gui.src = ${JSON.stringify(pathToFileURL(gui).href)};
    </script>
  </body></html>`);
  const main = join(temporary, "main.cjs");
  await writeFile(main, `const { app, BrowserWindow } = require('electron');
    app.setPath('userData', ${JSON.stringify(join(temporary, "profile"))});
    app.whenReady().then(() => {
      const window = new BrowserWindow({ show: false, width: 1000, height: 800,
        webPreferences: { sandbox: true, contextIsolation: true, nodeIntegration: false } });
      window.webContents.session.webRequest.onBeforeRequest({ urls: ['http://*/*', 'https://*/*'] }, (_details, callback) => callback({ cancel: true }));
      window.loadFile(${JSON.stringify(join(temporary, "host.html"))});
    });`);
  const { ELECTRON_RUN_AS_NODE: _ignored, ...environment } = process.env;
  application = await electron.launch({ executablePath: desktopRequire("electron"), args: [main], env: environment });
  page = await application.firstWindow();
  const errors = [];
  page.on("pageerror", error => errors.push(error.message));
  for (let round = 0; round < 2; round++) {
    if (round) await page.reload();
    await expect.poll(() => page.evaluate(() => globalThis.evidence?.connected), { timeout: 25_000 }).toBe(true);
    await expect.poll(() => page.evaluate(() => globalThis.evidence?.rendered), { timeout: 25_000 }).toBe(true);
    const frame = page.frameLocator("#gui");
    await expect(frame.getByRole("heading", { name: "What can Codex help you with?" })).toBeVisible();
    await expect(frame.getByRole("alert")).toHaveCount(0);
    const evidence = await page.evaluate(() => globalThis.evidence);
    assert.equal(evidence.origin, "file://");
    assert.ok(evidence.messages.some(item => item.type === "multica:agent-gui:ready" && item.origin === "null"));
    assert.ok(evidence.requests.includes("storage.read"));
    assert.ok(evidence.requests.includes("workspace.load"));
    assert.equal(evidence.rejectedSources, 1);
    console.log(JSON.stringify({ round, ...evidence }));
    const editor = frame.getByTestId("agent-gui-composer-editor");
    await page.evaluate(() => globalThis.setHostActive(false));
    await expect(frame.locator('[data-agent-gui-active="false"]')).toHaveCount(1);
    await page.getByRole("textbox", { name: "Search conversations" }).click();
    await page.evaluate(() => globalThis.setHostActive(true));
    await expect(frame.locator('[data-agent-gui-active="true"]')).toHaveCount(1);
    // Wait past both focus frames in the real file-origin child document.
    await editor.evaluate(() => new Promise(resolve => {
      globalThis.requestAnimationFrame(() => globalThis.requestAnimationFrame(() => globalThis.requestAnimationFrame(resolve)));
    }));
    await expect(page.getByRole("textbox", { name: "Search conversations" })).toBeFocused();
    await page.keyboard.type("Find my conversation");
    await expect(page.getByRole("textbox", { name: "Search conversations" })).toHaveValue("Find my conversation");
    await expect(editor).toHaveText("");
    await page.evaluate(() => globalThis.setHostActive(true, 1));
    await expect(editor).toBeFocused();
    await editor.fill("Explicit draft focus still works");
    await expect(editor).toContainText("Explicit draft focus still works");
    await editor.fill("");
    await page.getByRole("textbox", { name: "Search conversations" }).click();
    await page.evaluate(() => globalThis.appendPrompt("Appended draft focus still works"));
    await expect(editor).toBeFocused();
    await expect(editor).toContainText("Appended draft focus still works");
    await editor.fill("");
    await page.evaluate(() => globalThis.setHostActive(false));
    await expect(frame.locator('[data-agent-gui-active="false"]')).toHaveCount(1);
    await page.getByRole("textbox", { name: "Search conversations" }).click();
    await page.evaluate(() => {
      globalThis.document.getElementById("search").blur();
      globalThis.setHostActive(true);
    });
    await expect(editor).toBeFocused();
    console.log(`PASS: round ${round} preserves parent search focus across host activation and honors explicit draft focus`);
  }
  assert.deepEqual(errors, []);
  console.log("PASS: packaged file-origin handshake, RPC, rendered view, reload and unrelated-frame rejection");
} catch (error) {
  console.error(error);
  if (page) {
    console.error(await page.evaluate(() => ({ evidence: globalThis.evidence, child: globalThis.document.querySelector("#gui")?.contentDocument?.body.innerText.slice(0, 1200) })).catch(() => null));
  }
  throw error;
} finally {
  // Draft guards must not keep this isolated test application alive.
  await application?.evaluate(({ app }) => app.exit(0)).catch(() => undefined);
  await application?.close();
  await rm(temporary, { recursive: true, force: true });
}
