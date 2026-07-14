import { QueryClient } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient, setApiInstance } from "../api";
import { updateLarkNotificationEventsMutationOptions } from "./mutations";
import { larkKeys } from "./queries";

const installation = {
  id: "installation-1",
  workspace_id: "workspace-1",
  app_id: "app-1",
  bot_open_id: "bot-1",
  installer_user_id: "user-1",
  status: "active",
  notification_event_types: ["mentioned" as const],
  installed_at: "2026-07-13T00:00:00Z",
  created_at: "2026-07-13T00:00:00Z",
  updated_at: "2026-07-13T00:00:00Z",
};

afterEach(() => {
  vi.restoreAllMocks();
});

const executeMutation = (queryClient: QueryClient) => queryClient.getMutationCache()
  .build(queryClient, updateLarkNotificationEventsMutationOptions("workspace-1", queryClient))
  .execute(["mentioned"]);

describe("updateLarkNotificationEventsMutationOptions", () => {
  it("invalidates the workspace installation query after success", async () => {
    const client = new ApiClient("https://api.example.test");
    setApiInstance(client);
    vi.spyOn(client, "updateLarkNotificationEvents").mockResolvedValue(installation);
    const queryClient = new QueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");

    await executeMutation(queryClient);

    expect(invalidate).toHaveBeenCalledWith({ queryKey: larkKeys.installations("workspace-1") });
  });

  it("propagates API rejection without invalidating", async () => {
    const error = new Error("request failed");
    const client = new ApiClient("https://api.example.test");
    setApiInstance(client);
    vi.spyOn(client, "updateLarkNotificationEvents").mockRejectedValue(error);
    const queryClient = new QueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");

    await expect(executeMutation(queryClient)).rejects.toBe(error);
    expect(invalidate).not.toHaveBeenCalled();
  });

  it("does not invalidate when a malformed response is rejected by the client", async () => {
    const error = new Error("Invalid Lark notification events response: missing installation id");
    const client = new ApiClient("https://api.example.test");
    setApiInstance(client);
    vi.spyOn(client, "updateLarkNotificationEvents").mockRejectedValue(error);
    const queryClient = new QueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");

    await expect(executeMutation(queryClient)).rejects.toBe(error);
    expect(invalidate).not.toHaveBeenCalled();
  });
});
