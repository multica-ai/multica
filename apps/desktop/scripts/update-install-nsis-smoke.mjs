// Windows only: node scripts/update-install-nsis-smoke.mjs --nsis=<makensis.exe>
// Uses production updater/probe,
// real Electron/NsisUpdater and a user-level NSIS file-replacement fixture.
// The downloaded artifact is seeded locally; no network, registry or real
// Multica installation/profile/runtime is used.
import assert from "node:assert/strict";
import { spawn, spawnSync } from "node:child_process";
import { once } from "node:events";
import { copyFileSync, existsSync, mkdirSync, mkdtempSync, readFileSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import { dirname, join, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";
import { createHash } from "node:crypto";

const script = fileURLToPath(import.meta.url);
const desktop = resolve(dirname(script), "..");
const require = createRequire(import.meta.url);
const hash = (path) => createHash("sha256").update(readFileSync(path)).digest("hex");
const nsis = process.argv.find((arg) => arg.startsWith("--nsis="))?.slice(7);
const fixedEnv = { ...process.env };
delete fixedEnv.ELECTRON_RUN_AS_NODE;
const run = (exe, args, options = {}) => spawnSync(exe, args, {
  windowsHide: true, encoding: "utf8", env: fixedEnv, timeout: 30000, ...options,
});

if (process.versions.electron) {
  const config = JSON.parse(readFileSync(process.argv.at(-1), "utf8"));
  const { app, BrowserWindow } = require("electron");
  const { autoUpdater } = require("electron-updater");
  const ts = require("typescript");
  app.setPath("userData", config.userData);
  app.disableHardwareAcceleration();
  const load = (relative, dependencies = {}) => {
    const module = { exports: {} };
    const compiled = ts.transpileModule(readFileSync(join(desktop, relative), "utf8"), {
      compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
    }).outputText;
    new Function("require", "module", "exports", compiled)(
      (id) => Object.hasOwn(dependencies, id) ? dependencies[id] : require(id), module, module.exports,
    );
    return module.exports;
  };
  let cli;
  const fail = (error) => {
    console.error(error);
    cli?.stdin.end();
    app.exit(1);
  };
  process.on("uncaughtException", fail);
  process.on("unhandledRejection", fail);
  async function acceptance() {
    const timer = setTimeout(() => fail(new Error("Native acceptance timed out")), 30000);
    await app.whenReady();
    const window = new BrowserWindow({ show: false, webPreferences: { sandbox: true } });
    await window.loadURL("about:blank");
    cli = spawn(config.cli, ["-e", 'process.stdout.write("ready\\n");process.stdin.resume()'], {
      windowsHide: true, stdio: ["pipe", "pipe", "pipe"],
      env: { SystemRoot: process.env.SystemRoot, TEMP: config.root, TMP: config.root },
    });
    const exited = once(cli, "exit");
    await once(cli.stdout, "data");
    const originalHash = hash(config.cli);
    const baseline = run(config.installer, ["/S"]);
    assert.equal(baseline.error, undefined);
    assert.equal(baseline.status, 2, "NSIS must reproduce the locked executable failure");
    assert.equal(existsSync(config.marker), false);
    assert.equal(cli.exitCode, null, "baseline must not stop the fixture runtime");
    assert.equal(hash(config.cli), originalHash);

    const handlers = new Map();
    const probe = load("src/main/update-install-guard.ts", {
      "./bundled-cli": { bundledCliPath: () => config.cli },
    });
    const main = load("src/main/updater.ts", {
      electron: { app, ipcMain: { handle: (channel, handler) => handlers.set(channel, handler) } },
      "electron-updater": { autoUpdater },
      "./update-install-guard": probe,
      "./updater-preferences": load("src/main/updater-preferences.ts"),
    });
    autoUpdater.checkForUpdates = async () => { throw new Error("Fixture forbids update network access"); };
    autoUpdater.downloadedUpdateHelper = {
      file: config.installer, packageFile: null,
      downloadedFileInfo: { fileName: "fixture-installer.exe", isAdminRightsRequired: false },
    };
    autoUpdater.installDirectory = config.installDir;
    let realInstallerDispatches = 0;
    const install = autoUpdater.doInstall.bind(autoUpdater);
    autoUpdater.doInstall = (options) => {
      assert.equal(options.isSilent, true);
      assert.equal(options.isForceRunAfter, false);
      realInstallerDispatches++;
      return install(options);
    };
    main.setupAutoUpdater(() => window);
    await handlers.get("updater:get-preferences")();
    autoUpdater.emit("update-downloaded", { version: "0.0.2" });
    const blocked = await handlers.get("updater:install")();
    assert.equal(blocked.status, "deferred");
    assert.equal(blocked.reason, "runtime_running");
    assert.equal(realInstallerDispatches, 0);
    assert.equal(existsSync(config.marker), false);
    assert.equal(cli.exitCode, null);
    assert.equal(hash(config.cli), originalHash);
    cli.stdin.end();
    const [exitCode] = await exited;
    assert.equal(exitCode, 0);
    app.on("quit", (_event, code) => {
      assert.equal(code, 0);
      assert.equal(realInstallerDispatches, 1);
      writeFileSync(config.childReport, JSON.stringify({
        electron: process.versions.electron, platform: process.platform, arch: process.arch,
        lockedBaselineInstallerExit: baseline.status, blocked,
        guardedInstallerDispatchesWhileRunning: 0, realInstallerDispatchesAfterStop: realInstallerDispatches,
        userDaemonsStopped: 0, seededLocalDownload: true,
      }, null, 2));
      clearTimeout(timer);
    });
    // Exercise the production safe-quit path, which installs silently.
    app.quit();
  }
  // Electron entry evaluation must finish before app.ready.
  void acceptance().catch(fail);
} else {
  assert.equal(process.platform, "win32");
  assert.ok(nsis && existsSync(nsis), "Pass --nsis=<path to makensis.exe>");
  const root = mkdtempSync(join(tmpdir(), "multica-nsis-acceptance-"));
  const installDir = join(root, "install");
  const cli = join(installDir, "resources", "bin", "multica.exe");
  const userData = join(root, "profile");
  mkdirSync(dirname(cli), { recursive: true });
  mkdirSync(userData);
  copyFileSync(process.execPath, cli);
  writeFileSync(join(userData, "updater-preferences.json"), '{"automaticUpdates":false}');
  const config = {
    root, installDir, cli, userData,
    installer: join(root, "fixture-installer.exe"), marker: join(installDir, "installed-version.txt"),
    childReport: join(root, "child-report.json"),
  };
  const quote = (s) => s.replaceAll("$", "$$").replaceAll('"', '$\\"');
  const nsisFile = join(root, "fixture.nsi");
  writeFileSync(nsisFile, `Unicode true
Name "Multica updater acceptance fixture"
OutFile "${quote(config.installer)}"
RequestExecutionLevel user
SilentInstall silent
AutoCloseWindow true
SetCompress off
InstallDir "${quote(installDir)}"
Section
  SetOutPath "$INSTDIR\\resources\\bin"
  SetOverwrite on
  ClearErrors
  File /oname=multica.exe "${quote(process.execPath)}"
  IfErrors locked
  FileOpen $0 "$INSTDIR\\installed-version.txt" w
  FileWrite $0 "0.0.2"
  FileClose $0
  SetErrorLevel 0
  Goto done
locked:
  SetErrorLevel 2
done:
SectionEnd
`);
  const compiled = run(nsis, ["/V2", nsisFile]);
  assert.equal(compiled.error, undefined);
  assert.equal(compiled.status, 0, compiled.stderr || compiled.stdout);
  const configPath = join(root, "config.json");
  writeFileSync(configPath, JSON.stringify(config));
  const electron = require("electron");
  const result = run(electron, [script, configPath], { timeout: 60000 });
  if (result.stdout) process.stdout.write(result.stdout);
  if (result.stderr) process.stderr.write(result.stderr);
  assert.equal(result.error, undefined);
  assert.equal(result.status, 0);
  const deadline = Date.now() + 30000;
  while (!existsSync(config.marker) && Date.now() < deadline) await new Promise((r) => setTimeout(r, 100));
  assert.equal(readFileSync(config.marker, "utf8"), "0.0.2");
  const installedCli = run(cli, ["--version"]);
  assert.equal(installedCli.status, 0, installedCli.stderr);
  assert.equal(hash(cli), hash(process.execPath));
  const report = {
    passed: true, timestamp: new Date().toISOString(),
    ...JSON.parse(readFileSync(config.childReport, "utf8")),
    installedVersion: "0.0.2", installedFixtureCliVersion: installedCli.stdout.trim(),
    sourceHashes: Object.fromEntries(["updater.ts", "update-install-guard.ts"].map((p) => [p, hash(join(desktop, "src/main", p))])),
    scope: "Real NSIS file replacement through production updater and NsisUpdater; local artifact seeded; no production package/signature/download acceptance",
  };
  const resolvedRoot = realpathSync(root);
  assert.equal(dirname(resolvedRoot).toLowerCase(), realpathSync(tmpdir()).toLowerCase());
  assert.ok(resolvedRoot.toLowerCase().startsWith(resolve(tmpdir(), "multica-nsis-acceptance-").toLowerCase()));
  assert.ok(realpathSync(installDir).startsWith(resolvedRoot + sep));
  rmSync(resolvedRoot, { recursive: true, force: true, maxRetries: 3, retryDelay: 200 });
  console.log(JSON.stringify(report));
}
