// @vitest-environment node
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { BrowserWindow } from "electron";
const fake = vi.hoisted(() => ({ handlers: new Map<string, (...args: unknown[]) => unknown>(), show: vi.fn(async () => ({ response: 0 })), copy: vi.fn() }));
vi.mock("electron", () => ({ clipboard: { writeText: fake.copy }, dialog: { showMessageBox: fake.show }, ipcMain: { handle: (name: string, callback: (...args: unknown[]) => unknown) => fake.handlers.set(name, callback) } }));
import { createDesktopSettingsHost } from "./desktop-settings-host";

beforeEach(() => { fake.handlers.clear(); fake.show.mockClear(); fake.copy.mockClear(); fake.show.mockResolvedValue({ response: 0 }); });
function fixture() {
  let user = "user", generation = 1;
  const mainFrame = {};
  const webContents = { mainFrame, send: vi.fn((_channel, request) => queueMicrotask(() => fake.handlers.get("settings-host:reply")!({ sender: webContents, senderFrame: mainFrame }, { id: request.id, ok: true, result: { status: "ready" } }))) };
  const window = { webContents, isDestroyed: () => false } as unknown as BrowserWindow;
  const apply = vi.fn(async (request: { path: string }) => request.path === "/api/tokens" ? { id: "pat", token: "PRIVATE" } : { id: "member" });
  const host = createDesktopSettingsHost(() => window, () => user, () => generation, apply);
  return { host, window, webContents, apply, changeUser: () => { user = "other"; generation++; } };
}
describe("Settings host execution", () => {
  it("applies existing daemon confirmation requests without asking the user", async () => {
    const f = fixture();
    const request = { operation: "settings_remove_member", scope: { userId: "user", workspaceId: "workspace" }, input: { id: "member" } };
    const write = { method: "DELETE" as const, path: "/api/workspaces/workspace/members/member" };
    expect(await f.host.confirmAndApply(request, write, new AbortController().signal)).toEqual({ approved: true, raw: { id: "member" } });
    expect(fake.show).not.toHaveBeenCalled();
    expect(f.apply).toHaveBeenCalledWith(write, expect.any(AbortSignal), request.scope);
  });
  it("does not approve a host-only action through a dialog", async () => {
    const f = fixture();
    const request = { operation: "settings_device_action", scope: { userId: "user", workspaceId: "workspace" }, input: { action: "daemon_restart" } };
    expect(await f.host.confirmAndApply(request, null, new AbortController().signal)).toEqual({ approved: true, raw: null });
    expect(fake.show).not.toHaveBeenCalled();
    expect(f.apply).not.toHaveBeenCalled();
  });
  it.each(["account change", "cancellation"])("stops a write after %s during scope validation", async (reason) => {
    const f = fixture();
    const controller = new AbortController();
    const send = f.webContents.send.getMockImplementation()!;
    f.webContents.send.mockImplementation((channel, request) => {
      send(channel, request);
      if (reason === "account change") f.changeUser();
      else controller.abort();
    });
    await expect(f.host.confirmAndApply({ operation: "settings_remove_member", scope: { userId: "user", workspaceId: "workspace" }, input: { id: "member" } }, { method: "DELETE", path: "/api/workspaces/workspace/members/member" }, controller.signal)).rejects.toThrow();
    expect(f.apply).not.toHaveBeenCalled();
    expect(fake.show).not.toHaveBeenCalled();
  });
  const tokenRequest = { operation: "settings_create_token", scope: { userId: "user", workspaceId: "workspace" }, input: { name: "Key" } };
  const tokenWrite = { method: "POST" as const, path: "/api/tokens", body: { name: "Key" } };
  it("refuses to display token values after the account changes", async () => {
    const f = fixture();
    fake.show.mockImplementationOnce(async () => { f.changeUser(); return { response: 1 }; });
    await expect(f.host.confirmAndApply!(tokenRequest, tokenWrite, new AbortController().signal)).rejects.toThrow();
    expect(fake.show).toHaveBeenCalledTimes(1);
  });
  it("shows a created token only to the human and copies it only on Copy token", async () => {
    const f = fixture();
    fake.show.mockResolvedValueOnce({ response: 1 });
    expect(await f.host.confirmAndApply!(tokenRequest, tokenWrite, new AbortController().signal)).toEqual({ approved: true, raw: { id: "pat" } });
    expect(fake.copy).not.toHaveBeenCalled();
    fake.show.mockResolvedValueOnce({ response: 0 });
    const outcome = await f.host.confirmAndApply!(tokenRequest, tokenWrite, new AbortController().signal);
    expect(JSON.stringify(outcome)).not.toContain("PRIVATE");
    expect(fake.copy).toHaveBeenCalledWith("PRIVATE");
  });
  it("rejects replies from another renderer and aborts pending host requests", async () => {
    const f = fixture(); f.webContents.send.mockImplementation(() => {});
    const controller = new AbortController();
    const result = f.host.host!({ operation: "settings_read_device", scope: { userId: "user", workspaceId: "workspace" }, input: {} }, controller.signal);
    const id = f.webContents.send.mock.calls[0]![1].id;
    expect(() => fake.handlers.get("settings-host:reply")!({ sender: {}, senderFrame: {} }, { id, ok: true })).toThrow("requesting main window");
    controller.abort(); await expect(result).rejects.toThrow("execution has ended");
  });
});
