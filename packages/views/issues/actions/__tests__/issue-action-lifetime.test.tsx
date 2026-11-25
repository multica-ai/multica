import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { setApiInstance } from "@multica/core/api";
import { ApiError, type ApiClient } from "@multica/core/api/client";
import type { Issue, UpdateIssueRequest } from "@multica/core/types";
import { toast } from "sonner";
import { getTestResources } from "../../../test/i18n";
import { useIssueActions } from "../use-issue-actions";
import { useIssueSurfaceActions } from "../../surface/use-issue-surface-actions";
import type { IssueSurfaceMutationOptions } from "../../surface/actions-context";

vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(
    (selector: (state: { user: { id: string } }) => unknown) => selector({ user: { id: "member" } }),
    { getState: () => ({ user: { id: "member" } }) },
  ),
  registerAuthStore: vi.fn(),
  createAuthStore: vi.fn(),
}));
vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useWorkspacePaths: () => ({ issueDetail: (id: string) => `/test/issues/${id}` }),
}));
vi.mock("@multica/core/issue-statuses/hooks", () => ({
  useIssueStatuses: () => ({ entryOf: () => undefined }),
}));
vi.mock("@multica/core/pins", () => ({
  pinListOptions: () => ({ queryKey: ["pins"], queryFn: async () => [] }),
  useCreatePin: () => ({ mutate: vi.fn() }),
  useDeletePin: () => ({ mutate: vi.fn() }),
}));
vi.mock("../../../navigation", () => ({
  useNavigation: () => ({ getShareableUrl: (path: string) => path }),
}));

const issue = {
  id: "issue", workspace_id: "ws", identifier: "TEST-1", title: "Review result",
  status: "in_review", priority: "medium", assignee_type: null, assignee_id: null,
  parent_issue_id: null, project_id: null, description: null, revision: 1,
} as Issue;

type Save = (updates: Partial<UpdateIssueRequest>, options?: IssueSurfaceMutationOptions) => void;
const sources: { name: string; useSave: () => Save }[] = [
  { name: "detail action", useSave: function useDetailSave() { return useIssueActions(issue).updateField; } },
  { name: "surface action", useSave: function useSurfaceSave() {
    const { actions } = useIssueSurfaceActions({ createDefaults: {} });
    return (updates, options) => actions.updateIssue(issue.id, updates, options);
  } },
];

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

const updateIssue = vi.fn();
beforeEach(() => {
  updateIssue.mockReset();
  vi.mocked(toast.error).mockClear();
  setApiInstance({ updateIssue } as unknown as ApiClient);
});
afterEach(cleanup);

for (const source of sources) {
  describe(source.name, () => {
    function setup(client?: QueryClient) {
      const qc = client ?? new QueryClient({ defaultOptions: {
        queries: { retry: false, staleTime: Infinity }, mutations: { retry: false },
      } });
      const wrapper = ({ children }: { children: ReactNode }) => (
        <I18nProvider locale="en" resources={getTestResources()}>
          <QueryClientProvider client={qc}>{children}</QueryClientProvider>
        </I18nProvider>
      );
      return Object.assign(renderHook(source.useSave, { wrapper }), { qc });
    }

    it("does not navigate from a delayed success after leaving the initiating view", async () => {
      const request = deferred<Issue>();
      updateIssue.mockReturnValueOnce(request.promise);
      let page = "issue";
      const callbacks = { onSuccess: vi.fn(() => { page = "next issue"; }), onError: vi.fn(), onSettled: vi.fn() };
      const hook = setup();
      act(() => hook.result.current({ priority: "high" }, callbacks));
      await waitFor(() => expect(updateIssue).toHaveBeenCalledWith(issue.id, { priority: "high" }));
      expect(callbacks.onSuccess).not.toHaveBeenCalled();
      hook.unmount();
      page = "agents";
      const saved = { ...issue, priority: "high" as const, revision: 2 };
      await act(async () => request.resolve(saved));
      expect(callbacks.onSuccess).not.toHaveBeenCalled();
      expect(callbacks.onSettled).not.toHaveBeenCalled();
      expect(page).toBe("agents");
      expect(callbacks.onError).not.toHaveBeenCalled();
      expect(toast.error).not.toHaveBeenCalled();
    });

    it("reports a rejected save after unmount without updating the closed view", async () => {
      const request = deferred<Issue>();
      updateIssue.mockReturnValueOnce(request.promise);
      const callbacks = { onSuccess: vi.fn(), onError: vi.fn(), onSettled: vi.fn() };
      const hook = setup();
      act(() => hook.result.current({ priority: "high" }, callbacks));
      await waitFor(() => expect(updateIssue).toHaveBeenCalledTimes(1));
      hook.unmount();
      const error = new Error("The issue could not be saved");
      await act(async () => request.reject(error));
      await waitFor(() => expect(toast.error).toHaveBeenCalledExactlyOnceWith(error.message));
      expect(callbacks.onError).not.toHaveBeenCalled();
      expect(callbacks.onSettled).not.toHaveBeenCalled();
      expect(callbacks.onSuccess).not.toHaveBeenCalled();
    });

    it("reports an earlier failure without running superseded UI callbacks", async () => {
      const first = deferred<Issue>();
      const second = deferred<Issue>();
      updateIssue.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
      const firstCallbacks = { onError: vi.fn(), onSettled: vi.fn() };
      const secondCallbacks = { onSuccess: vi.fn(), onSettled: vi.fn() };
      const hook = setup();
      act(() => hook.result.current({ priority: "high" }, firstCallbacks));
      await waitFor(() => expect(updateIssue).toHaveBeenCalledTimes(1));
      act(() => hook.result.current({ priority: "low" }, secondCallbacks));
      await waitFor(() => expect(updateIssue).toHaveBeenCalledTimes(2));
      const saved = { ...issue, priority: "low" as const, revision: 2 };
      await act(async () => second.resolve(saved));
      await waitFor(() => expect(secondCallbacks.onSuccess).toHaveBeenCalledExactlyOnceWith(saved));
      const error = new Error("The earlier update failed");
      await act(async () => first.reject(error));
      await waitFor(() => expect(toast.error).toHaveBeenCalledExactlyOnceWith(error.message));
      expect(firstCallbacks.onError).not.toHaveBeenCalled();
      expect(firstCallbacks.onSettled).not.toHaveBeenCalled();
      expect(secondCallbacks.onSettled).toHaveBeenCalledTimes(1);
    });

    it("does not let a closed editor submit its queued description after a newer editor saves", async () => {
      const first = deferred<Issue>();
      let persisted = "D1";
      updateIssue.mockImplementation((_id: string, update: Partial<UpdateIssueRequest>) => {
        if (update.description === "D1") return first.promise;
        persisted = update.description!;
        return Promise.resolve({ ...issue, description: persisted, revision: 3 });
      });
      const oldEditor = setup();
      const saveFromOldEditor = oldEditor.result.current;
      act(() => saveFromOldEditor({ description: "D1" }, {
        onSuccess: () => saveFromOldEditor({ description: "D2", description_base: "D1" }),
      }));
      await waitFor(() => expect(updateIssue).toHaveBeenCalledTimes(1));
      oldEditor.unmount();
      const newEditor = setup(oldEditor.qc);
      act(() => newEditor.result.current({ description: "D3" }));
      await waitFor(() => expect(persisted).toBe("D3"));
      await act(async () => first.resolve({ ...issue, description: "D1", revision: 2 }));
      expect(updateIssue).toHaveBeenCalledTimes(2);
      expect(persisted).toBe("D3");
    });

    it("keeps the newer title conflict when an older successful response arrives", async () => {
      const first = deferred<Issue>();
      const second = deferred<Issue>();
      updateIssue.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
      let conflictDraft: string | null = null;
      const hook = setup();
      act(() => hook.result.current({ title: "First edit" }, {
        onSuccess: () => { conflictDraft = null; },
      }));
      await waitFor(() => expect(updateIssue).toHaveBeenCalledTimes(1));
      act(() => hook.result.current({ title: "Newer edit" }, {
        onError: () => { conflictDraft = "Newer edit"; },
      }));
      await waitFor(() => expect(updateIssue).toHaveBeenCalledTimes(2));
      await act(async () => second.reject(new ApiError("Title changed", 409, "Conflict", { code: "revision_conflict" })));
      await waitFor(() => expect(conflictDraft).toBe("Newer edit"));
      await act(async () => first.resolve({ ...issue, title: "First edit", revision: 2 }));
      expect(conflictDraft).toBe("Newer edit");
      expect(toast.error).toHaveBeenCalledTimes(1);
    });
  });
}
