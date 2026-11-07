// @vitest-environment node
import { describe, expect, it, vi } from "vitest";
import { applyChosenPath, localV2HostHandler, remoteHostRequests, type LocalV2HostHandlers } from "./local-v2-host";

const scope = { userId: "u1", workspaceId: "w1" };

function setup(user = "u1") {
  const handlers: LocalV2HostHandlers = {
    currentUser: () => user,
    side: vi.fn(async () => ({ ok: true })),
    confirm: vi.fn(async () => ({ approved: true as const, raw: { id: "x" } })),
    settingsHost: vi.fn(async () => ({ theme: "dark" })),
    developerLogs: vi.fn(async () => ({ path: "/tmp/l.json" })),
  };
  return { handlers, handle: localV2HostHandler(handlers) };
}

describe("localV2HostHandler", () => {
  it("applies the Settings write built from validated input, not one sent along", async () => {
    const { handlers, handle } = setup();
    const forged = { method: "POST", path: "/api/workspaces/w1/members", body: { email: "a@example.com", role: "admin" } };
    await expect(handle(scope, "settings.confirm", { operation: "settings_invite_member", scope: { workspace_id: "w1", user_id: "u1" }, input: { email: "a@example.com" }, impact: null, apply: forged })).resolves.toEqual({ approved: true, raw: { id: "x" } });
    expect(handlers.confirm).toHaveBeenCalledWith({ operation: "settings_invite_member", scope, input: { email: "a@example.com" }, impact: null },
      { method: "POST", path: "/api/workspaces/w1/members", body: { email: "a@example.com", role: "member" } }, expect.any(AbortSignal));
  });

  it("accepts host-only operations without an API write", async () => {
    const { handlers, handle } = setup();
    await handle(scope, "settings.confirm", { operation: "settings_device_action", scope: { workspace_id: "w1", user_id: "u1" }, input: { action: "daemon_restart" } });
    expect(vi.mocked(handlers.confirm).mock.calls[0]![1]).toBeNull();
  });

  it("routes device settings, refreshes, Side control and log exports", async () => {
    const { handlers, handle } = setup();
    await handle(scope, "settings.host", { operation: "settings_read_device", scope: { workspace_id: "w1", user_id: "u1" }, input: {} });
    await handle(scope, "settings.refresh", { scope: { workspace_id: "w1", user_id: "u1" } });
    expect(vi.mocked(handlers.settingsHost).mock.calls.map(([request]) => request.operation)).toEqual(["settings_read_device", "settings_refresh"]);
    await handle(scope, "side.control", { op: "select", scope: { userId: "u1", workspaceId: "w1" }, conversationId: "c", tabId: "t" });
    expect(handlers.side).toHaveBeenCalled();
    await handle(scope, "developer_logs.export", { sinceMinutes: 60, conversationId: "c" });
    expect(handlers.developerLogs).toHaveBeenCalledWith(scope, { sinceMinutes: 60, conversationId: "c" }, expect.any(AbortSignal));
  });

  it("refuses requests for another account or workspace", async () => {
    await expect(setup("other").handle(scope, "settings.refresh", {})).rejects.toThrow(/same signed-in account/);
    const { handle } = setup();
    await expect(handle(scope, "settings.host", { operation: "settings_read_device", scope: { workspace_id: "w2", user_id: "u1" }, input: {} })).rejects.toThrow(/another account or workspace/);
    await expect(handle(scope, "side.control", { scope: { userId: "u1", workspaceId: "w2" } })).rejects.toThrow(/another account or workspace/);
    await expect(handle(scope, "settings.confirm", { operation: "settings_x", scope: { workspace_id: "w1", user_id: "u1" }, input: {} })).rejects.toThrow(/Unknown settings operation/);
    await expect(handle(scope, "unknown.kind", {})).rejects.toThrow(/cannot handle/);
  });
});

describe("remoteHostRequests", () => {
  it("routes another device's settings writes and Side without exposing local device settings", async () => {
    const { handlers, handle } = setup();
    const remote = remoteHostRequests(handle, "peer-1", async () => "Mac Studio");
    await remote(scope, "settings.confirm", { operation: "settings_invite_member", scope: { workspace_id: "w1", user_id: "u1" }, input: { email: "a@example.com" } });
    expect(handlers.confirm).toHaveBeenCalledWith(expect.objectContaining({ source: "Mac Studio" }), expect.anything(), expect.any(AbortSignal));
    await remote(scope, "settings.host", { operation: "settings_open", scope: { workspace_id: "w1", user_id: "u1" }, input: {} });
    await expect(remote(scope, "settings.host", { operation: "settings_update_device", scope: { workspace_id: "w1", user_id: "u1" }, input: {} })).rejects.toMatchObject({ code: "device_settings_elsewhere" });
    await expect(remote(scope, "developer_logs.export", {})).rejects.toMatchObject({ code: "unsupported_host_request" });
    await remote(scope, "side.control", { op: "select", scope: { userId: "u1", workspaceId: "w1" }, conversationId: "c", tabId: "t" });
    expect(handlers.side).toHaveBeenCalledWith(expect.objectContaining({ deviceId: "peer-1", conversationId: "c" }), expect.any(AbortSignal));
    expect(handlers.developerLogs).not.toHaveBeenCalled();
  });
});

describe("applyChosenPath", () => {
  it("sends each choice to its route on the chosen device", async () => {
    const request = vi.fn(async () => ({}));
    await applyChosenPath({ request }, scope, { purpose: "folder_project" }, "/repo", "peer-1");
    await applyChosenPath({ request }, scope, { purpose: "conversation_directory", conversationId: "c 1" }, "/repo", "local");
    expect(request.mock.calls).toEqual([
      [scope, "POST", "/folder-projects", { path: "/repo" }, "peer-1"],
      [scope, "POST", "/sessions/c%201/commands", { type: "set_directory", cwd: "/repo" }, "local"],
    ]);
    await expect(applyChosenPath({ request }, scope, { purpose: "other" }, "/repo", "local")).rejects.toThrow();
  });
});
