// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";
import { setSchemaLogger } from "./schema";
import { noopLogger } from "../logger";

function respond(body: unknown) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), {
    status: 200, headers: { "Content-Type": "application/json" },
  })));
}

const connection = {
  id: "vcs-1", workspace_id: "ws-1", provider: "gongfeng",
  instance_url: "https://git.code.tencent.com", account_login: "alice",
};
const client = new ApiClient("https://api.example.test");

afterEach(() => { vi.unstubAllGlobals(); setSchemaLogger(noopLogger); });

describe("VCS API response compatibility", () => {
  it("accepts Gongfeng and future providers with defaults for older servers", async () => {
    respond({ connections: [connection, { ...connection, id: "vcs-2", provider: "future-provider" }], configured: true });
    const result = await client.listVCSConnections("ws-1");
    expect(result.connections.map((c) => c.provider)).toEqual(["gongfeng", "future-provider"]);
    expect(result).toMatchObject({ available: true, configured: true, can_manage: false });
    expect(result.connections[0]).toMatchObject({ webhook_url: "", webhook_path: "", created_at: "" });
  });

  it("fails closed on malformed lists", async () => {
    respond({ connections: [{ ...connection, id: null }], configured: true, can_manage: true });
    expect(await client.listVCSConnections("ws-1")).toEqual({
      connections: [], available: false, configured: false, can_manage: false,
    });
  });

  it("validates connect writes and keeps the one-time secret out of diagnostics", async () => {
    const warn = vi.fn();
    setSchemaLogger({ ...noopLogger, warn });
    respond({ ...connection, id: null, webhook_secret: "secret-not-for-logs" });
    await expect(client.connectVCS("ws-1", {
      provider: "gongfeng", instance_url: connection.instance_url, access_token: "token",
    })).rejects.toThrow(/Regenerate the webhook secret/);
    expect(warn).toHaveBeenCalledOnce();
    expect(JSON.stringify(warn.mock.calls)).not.toContain("secret-not-for-logs");
    respond({ ...connection, webhook_secret: "valid-secret" });
    await expect(client.connectVCS("ws-1", {
      provider: "gongfeng", instance_url: connection.instance_url, access_token: "token",
    })).resolves.toMatchObject({ provider: "gongfeng", webhook_secret: "valid-secret" });
  });

  it("rejects rotation responses without a usable secret", async () => {
    respond({ ...connection, webhook_secret: "" });
    await expect(client.rotateVCSWebhook("ws-1", "vcs-1")).rejects.toThrow(/Regenerate/);
  });
  it("validates repository pagination and sync status without hiding malformed writes", async () => {
    const repo = { id: 55, path: "acme/team/widget", web_url: "https://git.code.tencent.com/acme/team/widget" };
    respond({ repositories: [repo], selected: [{ ...repo, sync_error: "Permission denied", syncing: true }], next_page: 2 });
    const result = await client.listGongfengRepositories("ws-1", "vcs-1", 1, "team/widget");
    expect(result.next_page).toBe(2);
    expect(result.repositories[0]).toMatchObject({ archived: false, webhook_configured: false, synced_at: null });
    expect(result.selected[0]?.sync_error).toBe("Permission denied");
    expect(vi.mocked(fetch).mock.calls[0]?.[0]).toContain("search=team%2Fwidget");
    respond({ repositories: [{ ...repo, id: -1 }], selected: [] });
    await expect(client.listGongfengRepositories("ws-1", "vcs-1")).rejects.toThrow(/Invalid repository/);
    respond({ ...repo, syncing: true });
    await expect(client.enableGongfengRepository("ws-1", "vcs-1", 55, true)).resolves.toMatchObject({ id: 55, syncing: true });
    expect(vi.mocked(fetch).mock.calls[0]?.[0]).toContain("/55/sync");
    respond({ ...repo, id: "invalid" });
    await expect(client.enableGongfengRepository("ws-1", "vcs-1", 55)).rejects.toThrow(/Reload repositories/);
  });

  it("preserves 204 disconnect compatibility and reports remote cleanup failures", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(null, { status: 204 })));
    await expect(client.deleteVCSConnection("ws-1", "vcs-1")).resolves.toEqual({ webhook_cleanup_error: "" });
    respond({ webhook_cleanup_error: "Permission denied" });
    await expect(client.deleteVCSConnection("ws-1", "vcs-1")).resolves.toEqual({ webhook_cleanup_error: "Permission denied" });
    respond({ webhook_cleanup_error: 42 });
    await expect(client.deleteVCSConnection("ws-1", "vcs-1")).rejects.toThrow(/Invalid disconnect response/);
  });

});
