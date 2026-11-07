import { randomUUID } from "node:crypto";
import { clipboard, dialog, ipcMain, type BrowserWindow } from "electron";
import type { SettingsHostMessage, SettingsHostReply } from "../shared/settings-host";
import { confirmThenApply, type ConfirmAndApply, type SettingsHostRequest } from "./settings-confirm";
import type { SettingsRequest, SettingsScope } from "@multica/core/settings/operations";
import { SettingsOperationError } from "../shared/settings-operation-error";

/**
 * Existing daemon host requests execute directly after scope validation.
 * Desktop still owns token creation and keeps its secret out of MCP replies.
 */
export function createDesktopSettingsHost(getWindow: () => BrowserWindow | null, getUser: () => string | null | undefined, getGeneration: () => number, apply: (request: SettingsRequest, signal: AbortSignal, scope: SettingsScope) => Promise<unknown>): { host: (request: SettingsHostRequest, signal: AbortSignal) => Promise<unknown>; confirmAndApply: ConfirmAndApply } {
  const pending = new Map<string, { window: BrowserWindow; generation: number; resolve(value: unknown): void; reject(error: Error): void }>();
  ipcMain.handle("settings-host:reply", (event, reply: SettingsHostReply) => {
    if (!reply || typeof reply.id !== "string") throw new Error("Invalid settings reply");
    const entry = pending.get(reply.id);
    if (!entry) return;
    if (event.sender !== entry.window.webContents || event.senderFrame !== event.sender.mainFrame) throw new Error("Settings reply requires the requesting main window");
    if (entry.generation !== getGeneration()) entry.reject(new Error("The account changed during this request"));
    else if (reply.ok) entry.resolve(reply.result);
    else entry.reject(new Error(typeof reply.error === "string" ? reply.error.slice(0, 1000) : "Could not apply device settings"));
  });
  const windowFor = (request: SettingsHostRequest) => {
    const window = getWindow();
    if (!window || window.isDestroyed() || getUser() !== request.scope.userId) throw new SettingsOperationError("settings_host_unavailable", "Open the main app with the same signed-in account to use device settings");
    return window;
  };
  const host = async (request: SettingsHostRequest, signal: AbortSignal): Promise<unknown> => {
    signal.throwIfAborted();
    const window = windowFor(request), id = randomUUID();
    return new Promise((resolve, reject) => {
      const finish = (action: () => void) => { clearTimeout(timer); signal.removeEventListener("abort", abort); pending.delete(id); action(); };
      const cancel = () => { try { window.webContents.send("settings-host:request", { ...request, id, operation: "settings_cancel", input: {} }); } catch { /* A closed renderer has no remaining local writes. */ } };
      const abort = () => { cancel(); finish(() => reject(new SettingsOperationError("execution_ended", "This execution has ended"))); };
      const timer = setTimeout(() => { cancel(); finish(() => reject(new SettingsOperationError("settings_host_timeout", "The settings UI did not respond; no successful change is confirmed"))); }, 90_000);
      pending.set(id, { window, generation: getGeneration(), resolve: (value) => finish(() => resolve(value)), reject: (error) => finish(() => reject(error)) });
      signal.addEventListener("abort", abort, { once: true });
      const message: SettingsHostMessage = { ...request, id };
      try { window.webContents.send("settings-host:request", message); } catch (error) { finish(() => reject(error)); }
      if (signal.aborted) abort();
    });
  };
  return {
    host,
    confirmAndApply: confirmThenApply({ request: apply, confirm: async (request, signal) => {
      windowFor(request);
      const generation = getGeneration();
      await host({ ...request, operation: "settings_assert_scope", input: {} }, signal);
      signal.throwIfAborted();
      if (generation !== getGeneration()) throw new SettingsOperationError("account_changed", "The account changed before applying settings");
      return true;
    }, showSecret: async (name, secret, scope, signal) => {
      signal.throwIfAborted();
      await host({ operation: "settings_assert_scope", scope, input: {} }, signal);
      const window = windowFor({ operation: "settings_create_token", scope, input: {} });
      const result = await dialog.showMessageBox(window, { type: "info", title: "Multica API Token", message: name, detail: `Save this token now. It is shown only here and will not be included in the agent response.\n\n${secret}`, buttons: ["Copy token", "Done"], defaultId: 1, cancelId: 1, noLink: true, signal });
      signal.throwIfAborted();
      windowFor({ operation: "settings_create_token", scope, input: {} });
      if (result.response === 0) clipboard.writeText(secret);
    } }),
  };
}
