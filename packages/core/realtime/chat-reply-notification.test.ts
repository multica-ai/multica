// @vitest-environment node
import { QueryClient } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { notificationPreferenceKeys } from "../notification-preferences/queries";
import type { NotificationPreferences } from "../types/notification-preference";
import type { ChatReplyNotificationPayload } from "../types/events";
import { workspaceKeys } from "../workspace/queries";
import { createChatReplyNotifier } from "./use-realtime-sync";

function reply(overrides: Partial<ChatReplyNotificationPayload> = {}): ChatReplyNotificationPayload {
  return {
    chat_session_id: "session-1", message_id: "message-1", body: "Review is complete",
    workspace_id: "ws-a", title: "Review changes", ...overrides,
  };
}

describe("chat reply OS notifications", () => {
  let qc: QueryClient;
  let userId: string | undefined;
  let notify: ReturnType<typeof createChatReplyNotifier>;
  const showNotification = vi.fn();
  const hasFocus = vi.fn();
  const workspaces = [{ id: "ws-a", slug: "workspace-a" }, { id: "ws-b", slug: "workspace-b" }];

  function preferences(value: NotificationPreferences) {
    qc.setQueryData(notificationPreferenceKeys.all("ws-a"), { workspace_id: "ws-a", preferences: value });
  }

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    qc.setQueryData(workspaceKeys.list(), workspaces);
    preferences({});
    userId = "user-a";
    notify = createChatReplyNotifier(qc, () => userId);
    showNotification.mockReset();
    hasFocus.mockReset().mockReturnValue(false);
    vi.stubGlobal("desktopAPI", { showNotification });
    vi.stubGlobal("document", { hasFocus });
  });

  afterEach(() => {
    qc.clear();
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it.each([
    [{}, true],
    [{ comments: "muted" }, true],
    [{ chat_replies: "muted" }, false],
    [{ system_notifications: "muted" }, false],
  ] as [NotificationPreferences, boolean][])("respects source-workspace preferences %j", async (prefs, visible) => {
    preferences(prefs);
    qc.setQueryData(notificationPreferenceKeys.all("ws-b"), { preferences: { system_notifications: "muted" } });
    await notify(reply());
    expect(showNotification).toHaveBeenCalledTimes(visible ? 1 : 0);
    if (visible) expect(showNotification).toHaveBeenCalledWith({
      slug: "workspace-a", itemId: "chat:message-1", chatSessionId: "session-1", title: "Review changes", body: "Review is complete",
    });
  });

  it("does not generate inbox side effects", async () => {
    const invalidate = vi.spyOn(qc, "invalidateQueries");
    await notify(reply());
    expect(invalidate).not.toHaveBeenCalled();
    expect(qc.getQueryCache().getAll().some((query) => query.queryKey.includes("inbox"))).toBe(false);
  });

  it("suppresses focused replies and does not replay them after switching away", async () => {
    hasFocus.mockReturnValue(true);
    await notify(reply());
    hasFocus.mockReturnValue(false);
    await notify(reply());
    expect(showNotification).not.toHaveBeenCalled();
  });

  it("deduplicates completion replay while allowing the next reply", async () => {
    await Promise.all([notify(reply()), notify(reply())]);
    expect(showNotification).toHaveBeenCalledTimes(1);
    await notify(reply({ message_id: "message-2" }));
    expect(showNotification).toHaveBeenCalledTimes(2);
  });

  it("supports attachment-only replies", async () => {
    await notify(reply({ body: "" }));
    expect(showNotification).toHaveBeenCalledWith(expect.objectContaining({ body: "", chatSessionId: "session-1" }));
  });

  it("ignores alerts without a source workspace or message", async () => {
    await notify(reply({ workspace_id: "" }));
    await notify(reply({ message_id: "" }));
    expect(showNotification).not.toHaveBeenCalled();
  });

  it("drops a pending alert after the account changes", async () => {
    let resolveWorkspaces!: (value: unknown) => void;
    vi.spyOn(qc, "ensureQueryData").mockImplementationOnce(() => new Promise((resolve) => { resolveWorkspaces = resolve; }));
    const pending = notify(reply());
    userId = "user-b";
    resolveWorkspaces(workspaces);
    await pending;
    expect(showNotification).not.toHaveBeenCalled();
  });

  it("does not guess a workspace when the source is unavailable", async () => {
    qc.setQueryData(workspaceKeys.list(), [workspaces[1]]);
    await notify(reply());
    expect(showNotification).not.toHaveBeenCalled();
  });
});
