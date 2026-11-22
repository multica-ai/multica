import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { toast } from "sonner";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import { I18nProvider } from "@multica/core/i18n/react";
import { noteInboxAutoArchive, noteInboxBarAction } from "@multica/core/inbox/auto-archive";
import type { KeptInboxRow } from "@multica/core/inbox/open-row";
import { useInboxSelectionStore } from "@multica/core/inbox/selection-store";
import { notificationPreferenceKeys } from "@multica/core/notification-preferences/queries";
import { issueDetailOptions } from "@multica/core/issues/queries";
import type { InboxItem, Issue } from "@multica/core/types";
import { getTestResources } from "../../test/i18n";
import { AUTO_ARCHIVE_ADVANCE_DELAY_MS, useMoveOnAfterAutoArchive } from "./use-move-on-after-auto-archive";

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));

const api = { unarchiveInbox: vi.fn(async (id: string) => ({ id })) };

function item(id: string, issueId: string): InboxItem {
  return {
    id,
    workspace_id: "ws",
    recipient_type: "member",
    recipient_id: "member-1",
    actor_type: "agent",
    actor_id: "agent-1",
    type: "new_comment",
    severity: "info",
    issue_id: issueId,
    title: id,
    body: null,
    issue_status: "in_review",
    issue_priority: "none",
    read: true,
    archived: false,
    created_at: "2026-11-21T08:00:00Z",
    details: null,
  };
}

const archived = (issueId: string): KeptInboxRow => ({ key: issueId, item: item(`n-${issueId}`, issueId), reason: "archived" });
const nextRow = item("n-next", "issue-next");

function setup(
  preferences: Record<string, string>,
  initial: KeptInboxRow | null = null,
  next: InboxItem | null = nextRow,
) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  qc.setQueryData(notificationPreferenceKeys.all("ws"), { workspace_id: "ws", preferences });
  const resources = getTestResources();
  const wrapper = ({ children }: { children: ReactNode }) => (
    <I18nProvider locale="en" resources={resources}>
      <QueryClientProvider client={qc}>{children}</QueryClientProvider>
    </I18nProvider>
  );
  const onNext = vi.fn();
  const view = renderHook(
    ({ settled }: { settled: KeptInboxRow | null }) => useMoveOnAfterAutoArchive({ settled, next, onNext }),
    { wrapper, initialProps: { settled: initial } },
  );
  return {
    onNext,
    settle: (row: KeptInboxRow | null) => view.rerender({ settled: row }),
    cause: () => view.result.current.cause,
    autoAdvance: () => view.result.current.autoAdvance,
    qc,
  };
}

beforeEach(() => {
  vi.useFakeTimers();
  vi.clearAllMocks();
  setApiInstance(api as unknown as ApiClient);
  vi.spyOn(document, "hasFocus").mockReturnValue(true);
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe("useMoveOnAfterAutoArchive", () => {
  it("buffers then opens the next notification when your action archived the open one", () => {
    const { onNext, settle, autoAdvance } = setup({ after_auto_archive: "next" });
    noteInboxAutoArchive({ issue_id: "issue-a", reason: "actor_acted" });
    settle(archived("issue-a"));

    expect(onNext).not.toHaveBeenCalled();
    expect(autoAdvance()).toMatchObject({ durationMs: AUTO_ARCHIVE_ADVANCE_DELAY_MS });

    act(() => { vi.advanceTimersByTime(AUTO_ARCHIVE_ADVANCE_DELAY_MS); });
    expect(onNext).toHaveBeenCalledTimes(1);
    expect(toast.success).toHaveBeenCalledWith("Notification archived", expect.anything());
    expect(autoAdvance()).toBeNull();
  });

  it("cancels the auto-advance buffer on explicit cancel, Escape, wheel, outside pointerdown, or editable focus", () => {
    // Explicit cancel
    {
      const { onNext, settle, autoAdvance } = setup({ after_auto_archive: "next" });
      noteInboxAutoArchive({ issue_id: "issue-c1", reason: "actor_acted" });
      settle(archived("issue-c1"));
      act(() => autoAdvance()?.cancel());
      expect(autoAdvance()).toBeNull();
      act(() => { vi.advanceTimersByTime(AUTO_ARCHIVE_ADVANCE_DELAY_MS * 2); });
      expect(onNext).not.toHaveBeenCalled();
      cleanup();
    }

    // Escape key
    {
      const { onNext, settle, autoAdvance } = setup({ after_auto_archive: "next" });
      noteInboxAutoArchive({ issue_id: "issue-c2", reason: "actor_acted" });
      settle(archived("issue-c2"));
      act(() => {
        window.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
      });
      expect(autoAdvance()).toBeNull();
      act(() => { vi.advanceTimersByTime(AUTO_ARCHIVE_ADVANCE_DELAY_MS * 2); });
      expect(onNext).not.toHaveBeenCalled();
      cleanup();
    }

    // Wheel scroll
    {
      const { onNext, settle, autoAdvance } = setup({ after_auto_archive: "next" });
      noteInboxAutoArchive({ issue_id: "issue-c3", reason: "actor_acted" });
      settle(archived("issue-c3"));
      act(() => {
        window.dispatchEvent(new Event("wheel"));
      });
      expect(autoAdvance()).toBeNull();
      act(() => { vi.advanceTimersByTime(AUTO_ARCHIVE_ADVANCE_DELAY_MS * 2); });
      expect(onNext).not.toHaveBeenCalled();
      cleanup();
    }

    // Outside pointerdown cancels; pointerdown inside [data-inbox-bar] does not
    {
      const { onNext, settle, autoAdvance } = setup({ after_auto_archive: "next" });
      const bar = document.createElement("div");
      bar.setAttribute("data-inbox-bar", "settled");
      document.body.appendChild(bar);
      noteInboxAutoArchive({ issue_id: "issue-c4", reason: "actor_acted" });
      settle(archived("issue-c4"));

      act(() => {
        bar.dispatchEvent(new MouseEvent("pointerdown", { bubbles: true }));
      });
      expect(autoAdvance()).not.toBeNull();

      act(() => {
        document.body.dispatchEvent(new MouseEvent("pointerdown", { bubbles: true }));
      });
      expect(autoAdvance()).toBeNull();
      act(() => { vi.advanceTimersByTime(AUTO_ARCHIVE_ADVANCE_DELAY_MS * 2); });
      expect(onNext).not.toHaveBeenCalled();
      bar.remove();
      cleanup();
    }

    // Focusing an editable target cancels
    {
      const { onNext, settle, autoAdvance } = setup({ after_auto_archive: "next" });
      const input = document.createElement("textarea");
      document.body.appendChild(input);
      noteInboxAutoArchive({ issue_id: "issue-c5", reason: "actor_acted" });
      settle(archived("issue-c5"));

      act(() => {
        input.dispatchEvent(new FocusEvent("focusin", { bubbles: true }));
      });
      expect(autoAdvance()).toBeNull();
      act(() => { vi.advanceTimersByTime(AUTO_ARCHIVE_ADVANCE_DELAY_MS * 2); });
      expect(onNext).not.toHaveBeenCalled();
      input.remove();
    }
  });

  it("undoes by restoring the notification and returning its row", async () => {
    const returnRow = vi.spyOn(useInboxSelectionStore.getState(), "returnRow");
    const { settle } = setup({ after_auto_archive: "next" });
    noteInboxAutoArchive({ issue_id: "issue-b", reason: "issue_status_terminal" });
    settle(archived("issue-b"));
    act(() => { vi.advanceTimersByTime(AUTO_ARCHIVE_ADVANCE_DELAY_MS); });
    const action = (vi.mocked(toast.success).mock.calls.at(-1)?.[1] as unknown as { action: { onClick: () => void } }).action;
    await act(async () => action.onClick());
    expect(returnRow).toHaveBeenCalledWith("ws", "issue-b");
    expect(api.unarchiveInbox).toHaveBeenCalledWith("n-issue-b");
  });

  it("stays by default", () => {
    const { onNext, settle, autoAdvance } = setup({});
    noteInboxAutoArchive({ issue_id: "issue-c", reason: "actor_acted" });
    settle(archived("issue-c"));
    expect(autoAdvance()).toBeNull();
    act(() => { vi.advanceTimersByTime(AUTO_ARCHIVE_ADVANCE_DELAY_MS * 2); });
    expect(onNext).not.toHaveBeenCalled();
  });

  it("stays when something other than your action archived it", () => {
    const { onNext, settle, autoAdvance } = setup({ after_auto_archive: "next" });
    settle(archived("issue-d"));
    expect(autoAdvance()).toBeNull();
    act(() => { vi.advanceTimersByTime(AUTO_ARCHIVE_ADVANCE_DELAY_MS * 2); });
    expect(onNext).not.toHaveBeenCalled();
  });

  it("moves on to Inbox Zero when the last notification auto-archives under after_auto_archive=next", () => {
    const { onNext, settle } = setup({ after_auto_archive: "next" }, null, null);
    noteInboxAutoArchive({ issue_id: "issue-last", reason: "actor_acted" });
    settle(archived("issue-last"));
    act(() => { vi.advanceTimersByTime(AUTO_ARCHIVE_ADVANCE_DELAY_MS); });
    expect(onNext).toHaveBeenCalledTimes(1);
    expect(toast.success).toHaveBeenCalledWith("Notification archived", expect.anything());
  });

  it("stays when the action happened in another window", () => {
    vi.mocked(document.hasFocus).mockReturnValue(false);
    const { onNext, settle, autoAdvance } = setup({ after_auto_archive: "next" });
    noteInboxAutoArchive({ issue_id: "issue-e", reason: "actor_acted" });
    settle(archived("issue-e"));
    expect(autoAdvance()).toBeNull();
    act(() => { vi.advanceTimersByTime(AUTO_ARCHIVE_ADVANCE_DELAY_MS * 2); });
    expect(onNext).not.toHaveBeenCalled();
  });

  it("reports the bar action behind an archive it stays on", () => {
    const { settle, cause } = setup({});
    noteInboxBarAction("issue-f", { type: "status", status: "in_progress" });
    noteInboxAutoArchive({ issue_id: "issue-f", reason: "actor_acted" });
    settle(archived("issue-f"));
    expect(cause()).toEqual({ type: "status", status: "in_progress" });
  });

  it("reports no assignment the issue does not show, as one dismissed at its confirmation", () => {
    const { settle, cause, qc } = setup({});
    qc.setQueryData(issueDetailOptions("ws", "issue-h").queryKey, {
      id: "issue-h",
      status: "in_review",
      assignee_type: "member",
      assignee_id: "member-1",
    } as Issue);
    noteInboxBarAction("issue-h", { type: "assignee", assignee: { type: "agent", id: "agent-nova" } });
    noteInboxAutoArchive({ issue_id: "issue-h", reason: "actor_acted" });
    settle(archived("issue-h"));
    expect(cause()).toBeNull();
  });

  it("reports no action when something else archived it, and forgets the action", () => {
    const { settle, cause } = setup({});
    noteInboxBarAction("issue-g", { type: "status", status: "in_progress" });
    settle(archived("issue-g"));
    expect(cause()).toBeNull();
    settle(null);
    noteInboxAutoArchive({ issue_id: "issue-g", reason: "actor_acted" });
    settle(archived("issue-g"));
    expect(cause()).toBeNull();
  });
});
