import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import { createQueryClient } from "@multica/core/query-client";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import { I18nProvider } from "@multica/core/i18n/react";
import { toast } from "sonner";
import enInbox from "../../locales/en/inbox.json";
import { useUnsubscribeArchive } from "./use-unsubscribe-archive";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-review" }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
afterEach(() => { cleanup(); vi.clearAllMocks(); });

it.each([[false, false], [false, true], [true, false], [true, true]])("reports completion (navigation=%s, failure=%s)", async (navigate, fail) => {
  const qc = createQueryClient();
  let finish!: () => void;
  let reject!: (error: Error) => void;
  const request = new Promise<void>((resolve, rejectRequest) => { finish = resolve; reject = rejectRequest; });
  const unsubscribe = vi.fn(() => request);
  const archive = vi.fn(async () => ({}));
  setApiInstance({
    unsubscribeFromIssue: unsubscribe,
    archiveInbox: archive,
    listSubscribedIssueIds: vi.fn(async () => []),
  } as unknown as ApiClient);
  const { result, unmount } = renderHook(() => useUnsubscribeArchive(), {
    wrapper: ({ children }) => <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={{ en: { inbox: enInbox } }}>{children}</I18nProvider>
    </QueryClientProvider>,
  });
  act(() => result.current({ id: "notification-review", issueId: "issue-review" }));
  await waitFor(() => expect(unsubscribe).toHaveBeenCalled());
  if (navigate) unmount(); // IssueDetailRoute keys the containing IssuePeekHost by canonicalId.
  await act(async () => { if (fail) reject(new Error("request failed")); else finish(); });
  await waitFor(() => expect(qc.getMutationCache().getAll()[0]?.state.status).toBe(fail ? "error" : "success"));
  if (fail) {
    expect(archive).not.toHaveBeenCalled();
    expect(toast.error).toHaveBeenCalledOnce();
  } else {
    expect(archive).toHaveBeenCalledOnce();
    expect(toast.success).toHaveBeenCalledWith("Unsubscribed and archived", expect.objectContaining({
      action: expect.objectContaining({ label: "Undo" }),
    }));
  }
});

it("undoes from the toast after the page is gone by restoring, not subscribing", async () => {
  const qc = createQueryClient();
  const subscribeToIssue = vi.fn(async () => {});
  const undoUnsubscribeFromIssue = vi.fn(async () => {});
  const unarchiveInbox = vi.fn(async () => ({}));
  setApiInstance({
    unsubscribeFromIssue: vi.fn(async () => {}),
    archiveInbox: vi.fn(async () => ({})),
    subscribeToIssue,
    undoUnsubscribeFromIssue,
    unarchiveInbox,
    listSubscribedIssueIds: vi.fn(async () => []),
  } as unknown as ApiClient);
  const { result, unmount } = renderHook(() => useUnsubscribeArchive(), {
    wrapper: ({ children }) => <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={{ en: { inbox: enInbox } }}>{children}</I18nProvider>
    </QueryClientProvider>,
  });
  act(() => result.current({ id: "notification-review", issueId: "issue-review" }));
  unmount();
  await waitFor(() => expect(toast.success).toHaveBeenCalledOnce());
  const options = vi.mocked(toast.success).mock.calls[0]![1] as unknown as { action: { onClick: () => void } };
  await act(async () => options.action.onClick());
  await waitFor(() => expect(unarchiveInbox).toHaveBeenCalledWith("notification-review"));
  expect(undoUnsubscribeFromIssue).toHaveBeenCalledWith("issue-review");
  expect(subscribeToIssue).not.toHaveBeenCalled();
});
