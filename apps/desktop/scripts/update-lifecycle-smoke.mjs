// electron scripts/update-lifecycle-smoke.mjs --case=<case below>
// Real hidden Electron lifecycle, fake updater/probe; never launches an installer.
import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { EventEmitter } from "node:events";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { app, BrowserWindow } from "electron";
import ts from "typescript";

const scenario = process.argv.find((arg) => arg.startsWith("--case="))?.slice(7) ?? "safe-restart";
assert.ok(["safe-restart", "safe-quit", "quit-during-check", "cancel-then-quit", "daemon-continuation", "will-cancel"].includes(scenario));
const fixture = mkdtempSync(join(tmpdir(), "multica-update-lifecycle-"));
app.setPath("userData", fixture);
writeFileSync(join(fixture, "updater-preferences.json"), JSON.stringify({ automaticUpdates: false }));
const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const require = createRequire(import.meta.url);
const load = (relative, dependencies = {}) => {
  const module = { exports: {} };
  const compiled = ts.transpileModule(readFileSync(join(root, relative), "utf8"), {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
  }).outputText;
  new Function("require", "module", "exports", compiled)(
    (id) => Object.hasOwn(dependencies, id) ? dependencies[id] : require(id), module, module.exports,
  );
  return module.exports;
};

async function run() {
  const timeout = setTimeout(() => { console.error("Update lifecycle smoke timed out"); app.exit(1); }, 30000);
  try {
    await app.whenReady();
    const window = new BrowserWindow({ show: false, webPreferences: { sandbox: true, contextIsolation: true } });
    await window.loadURL("about:blank");
    let phase = "running";
    let probeCount = 0;
    let continuationCount = 0;
    const installs = [];
    const handlers = new Map();
    const updater = Object.assign(new EventEmitter(), {
      quitAndInstall: (silent, relaunch) => {
        assert.equal(phase, "quit", "an installer must not start while quit can still be cancelled");
        installs.push({ silent, relaunch });
      },
      checkForUpdates: async () => { throw new Error("fixture forbids update network access"); },
    });
    let resolveProbe;
    const blocked = { allowed: false, reason: "runtime_running" };
    const check = async () => {
      probeCount++;
      if (scenario === "quit-during-check") return new Promise((resolve) => { resolveProbe = resolve; });
      if (["cancel-then-quit", "daemon-continuation"].includes(scenario) && probeCount > 1) return blocked;
      return { allowed: true };
    };
    app.on("quit", () => { phase = "quit"; });
    const main = load("src/main/updater.ts", {
      electron: { app, ipcMain: { handle: (channel, handler) => handlers.set(channel, handler) } },
      "electron-updater": { autoUpdater: updater },
      "./update-install-guard": { checkWindowsUpdateInstall: check },
      "./updater-preferences": load("src/main/updater-preferences.ts"),
    });
    main.setupAutoUpdater(() => window, check, true);
    await handlers.get("updater:get-preferences")();

    if (["cancel-then-quit", "daemon-continuation"].includes(scenario)) {
      let continued = false;
      app.on("before-quit", (event) => {
        if (continued) return;
        continued = true;
        continuationCount++;
        event.preventDefault();
        // Models an independent listener with its own one-shot quit guard.
        // No real daemon is loaded or stopped by this fixture.
        const schedule = scenario === "daemon-continuation" ? queueMicrotask : setImmediate;
        schedule(() => app.quit());
      });
    }
    if (scenario === "will-cancel") {
      app.on("will-quit", (event) => {
        event.preventDefault();
        // Even a same-turn exit after the veto must not reuse safe-quit intent.
        queueMicrotask(() => app.exit(0));
      });
    }
    app.on("quit", () => {
      const expected = scenario === "safe-restart" ? [{ silent: false, relaunch: true }]
        : scenario === "safe-quit" ? [{ silent: true, relaunch: false }] : [];
      assert.deepEqual(installs, expected);
      assert.equal(probeCount, ["cancel-then-quit", "daemon-continuation"].includes(scenario) ? 2 : 1);
      console.log(JSON.stringify({ passed: true, scenario, electron: process.versions.electron, probeCount, continuationCount, fakeInstallerDispatches: installs, realInstallerRuns: 0, userDaemonsStopped: 0 }));
      clearTimeout(timeout);
    });
    updater.emit("update-downloaded", { version: "0.4.37" });
    if (scenario === "safe-quit") app.quit();
    else {
      const install = handlers.get("updater:install")();
      if (scenario === "quit-during-check") { app.quit(); resolveProbe(blocked); }
      await install;
    }
  } catch (error) {
    console.error(error);
    clearTimeout(timeout);
    app.exit(1);
  }
}

process.on("uncaughtException", (error) => { console.error(error); app.exit(1); });
// Electron's entry must finish evaluating before app.ready; no top-level await.
void run();
