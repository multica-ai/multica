// Run with Electron, not Node: electron scripts/native-dictation-smoke.mjs
// A hidden, isolated Chromium fixture; no app profile, agent, audio or SendInput.
import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { createRequire } from "node:module";
import { fileURLToPath, pathToFileURL } from "node:url";
import { app, BrowserWindow } from "electron";
import ts from "typescript";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const require = createRequire(import.meta.url);
const fixture = mkdtempSync(join(tmpdir(), "multica-dictation-smoke-"));
app.setPath("userData", join(fixture, "profile"));
const compile = (relative) => ts.transpileModule(readFileSync(join(root, relative), "utf8"), {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText;
const load = (relative, dependencies = {}) => {
  const module = { exports: {} };
  new Function("require", "module", "exports", compile(relative))(
    (id) => Object.hasOwn(dependencies, id) ? dependencies[id] : require(id), module, module.exports,
  );
  return module.exports;
};
const shared = load("src/shared/dictation.ts");
const preload = join(fixture, "preload.cjs");
writeFileSync(preload, `
  const electron = require("electron");
  const fixtureModule = { exports: {} };
  (function(require, module, exports) { ${compile("src/preload/dictation-activation.ts")} }) (
    (id) => id === "electron" ? electron : ${JSON.stringify(shared)}, fixtureModule, fixtureModule.exports
  );
  fixtureModule.exports.installCodexDictationActivation();
`);
const html = join(fixture, "index.html");
writeFileSync(html, '<button data-native-dictation id="mic">Mic</button><div id="editor" contenteditable="true">draft</div>');

async function run() {
 let window;
 const timeout = setTimeout(() => { console.error("Dictation smoke timed out"); app.exit(1); }, 30000);
 try {
  await app.whenReady();
  window = new BrowserWindow({ show: false, webPreferences: { preload, sandbox: true, contextIsolation: true, webSecurity: false } });
  const handlers = new Map();
  let configReads = 0;
  let nativeDispatches = 0;
  // Native focus/HWND are unit-tested separately. This hidden fixture supplies
  // focus admission so a renderer-world rejection, not background focus, is
  // what protects the config/native boundaries tested below.
  const host = { webContents: window.webContents, isDestroyed: () => false, isFocused: () => true };
  const main = load("src/main/codex-dictation.ts", {
    electron: { BrowserWindow: { fromWebContents: () => host }, ipcMain: { handle: (channel, handler) => handlers.set(channel, handler) } },
    "node:fs/promises": { open: () => { configReads++; throw new Error("fixture prohibits config access"); } },
    "node:child_process": { execFile: () => { nativeDispatches++; throw new Error("fixture prohibits native dispatch"); } },
    "./bundled-cli": { bundledCliPath: () => { throw new Error("fixture prohibits helper lookup"); } },
    "../shared/dictation": shared,
    "./navigation-guard": load("src/main/navigation-guard.ts"),
  });
  main.setupCodexDictation();
  main.registerCodexDictationWindow(host, pathToFileURL(html).toString());
  await window.loadFile(html);
  const wc = window.webContents;
  assert.equal(await wc.executeJavaScript("typeof globalThis.multicaDictationActivation"), "undefined");
  await wc.executeJavaScript('globalThis.fixtureClick = new Promise(resolve => document.addEventListener("click", e => resolve({ trusted: e.isTrusted, target: e.target.id }), { once: true })); void 0');
  wc.focus();
  wc.sendInputEvent({ type: "mouseDown", x: 15, y: 15, button: "left", clickCount: 1 });
  wc.sendInputEvent({ type: "mouseUp", x: 15, y: 15, button: "left", clickCount: 1 });
  assert.deepEqual(await wc.executeJavaScript("globalThis.fixtureClick"), { trusted: true, target: "mic" });
  await wc.executeJavaScript('document.getElementById("editor").focus()');
  const consume = () => wc.executeJavaScriptInIsolatedWorld(shared.CODEX_DICTATION_WORLD, [{ code: "globalThis.multicaDictationActivation.consume()" }], false);
  assert.equal(await consume(), true, "a Chromium mic click can arm the isolated gate");
  assert.equal(await consume(), false, "the grant is one-shot");
  await wc.executeJavaScript(`
    Object.defineProperty(navigator, "userActivation", { value: { isActive: true } });
    Object.defineProperty(document, "activeElement", { value: { isContentEditable: true } });
    globalThis.multicaDictationActivation = { consume: () => true };
    document.getElementById("mic").click();
    document.getElementById("editor").focus();
  `);
  assert.equal(await wc.executeJavaScript("navigator.userActivation.isActive === true && document.activeElement?.isContentEditable === true"), true,
    "the old main-world probe is spoofable");
  assert.equal(await consume(), false);
  if (process.platform === "win32") {
    assert.deepEqual(await handlers.get(shared.CODEX_DICTATION_CHANNEL)({ sender: wc, senderFrame: wc.mainFrame }), { ok: false, reason: "unavailable" });
    const prevented = new Promise((resolve) => wc.once("before-input-event", (event) => resolve(event.defaultPrevented)));
    wc.sendInputEvent({ type: "keyDown", keyCode: "D", modifiers: ["control", "alt", "shift"] });
    assert.equal(await prevented, true, "Chromium's fixed-chord event must be intercepted before renderer dispatch");
    assert.equal(await wc.executeJavaScript('document.getElementById("editor").textContent'), "draft");
  }
  assert.equal(configReads, 0);
  assert.equal(nativeDispatches, 0);
  console.log(JSON.stringify({ passed: true, electron: process.versions.electron, platform: process.platform, checks: ["one-shot Chromium mic activation", "main-world spoof rejected", "synthetic click rejected", "private isolated bridge", "config/native boundaries untouched", "fixed chord intercepted (Windows)"] }));
  window.destroy();
  app.exit(0);
 } catch (error) {
  console.error(error);
  window?.destroy();
  app.exit(1);
 } finally {
  clearTimeout(timeout);
 }
}

// Do not await app.ready at ESM top level: Electron waits for entry evaluation.
void run();
