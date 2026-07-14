import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";

const installation = {
  id: "installation-1",
  workspace_id: "workspace-1",
  app_id: "app-1",
  bot_open_id: "bot-1",
  installer_user_id: "user-1",
  status: "active",
  installed_at: "2026-07-13T00:00:00Z",
  created_at: "2026-07-13T00:00:00Z",
  updated_at: "2026-07-13T00:00:00Z",
};

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("ApiClient Lark notification events", () => {
  it("lists installations from the expected URL and parses notification defaults", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      installations: [installation],
      configured: true,
      install_supported: true,
    }), { status: 200, headers: { "Content-Type": "application/json" } }));
    vi.stubGlobal("fetch", fetchMock);

    const result = await new ApiClient("https://api.example.test")
      .listLarkInstallations("workspace-1");

    expect(fetchMock).toHaveBeenCalledWith(
      "https://api.example.test/api/workspaces/workspace-1/lark/installations",
      expect.any(Object),
    );
    expect(result.installations[0]?.notification_event_types).toEqual([
      "issue_assigned", "mentioned", "task_failed", "quick_create_failed", "autopilot_paused",
    ]);
    expect(result).toMatchObject({ configured: true, install_supported: true });
  });

  it("updates notification events with PUT and the expected JSON body", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      ...installation,
      notification_event_types: ["mentioned"],
    }), { status: 200, headers: { "Content-Type": "application/json" } }));
    vi.stubGlobal("fetch", fetchMock);

    const result = await new ApiClient("https://api.example.test")
      .updateLarkNotificationEvents("workspace-1", ["mentioned"]);

    expect(fetchMock).toHaveBeenCalledWith(
      "https://api.example.test/api/workspaces/workspace-1/lark/notification-events",
      expect.objectContaining({
        method: "PUT",
        body: JSON.stringify({ event_types: ["mentioned"] }),
      }),
    );
    expect(result.id).toBe("installation-1");
  });

  it("rejects a malformed update response instead of returning a successful fallback", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({ status: "active" }), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    })));

    await expect(new ApiClient("https://api.example.test")
      .updateLarkNotificationEvents("workspace-1", ["mentioned"]))
      .rejects.toThrow("Invalid Lark notification events response: missing workspace id");
  });
});
