import { act, renderHook, cleanup } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { StrictMode, useEffect, type ReactNode } from "react";
import type { Doc } from "@multica/core/types";
import { api } from "@multica/core/api";
import { setDocDraft, useDocDraftStore } from "@multica/core/docs";
import {
  clearClientSessionData,
  setCurrentWorkspace,
} from "@multica/core/platform";
import { useDocDraft } from "./use-doc-draft";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-review" }));
const auth = vi.hoisted(() => ({
  user: { id: "u1" } as { id: string } | null,
}));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(vi.fn(), { getState: () => auth }),
}));
vi.mock("@multica/core/api", () => ({
  api: { updateDoc: vi.fn(), getDoc: vi.fn() },
  ApiError: class ApiError extends Error {
    constructor(
      message: string,
      public status: number,
      public statusText: string,
      public body: unknown,
    ) {
      super(message);
    }
  },
}));

const doc: Doc = {
  id: "doc-review",
  workspaceId: "ws-review",
  folderId: null,
  title: "Runbook",
  content: "Original",
  revision: 1,
  versionCount: 1,
  createdByType: "member",
  createdById: "u1",
  updatedByType: "member",
  updatedById: "u1",
  createdAt: "2026-11-09T00:00:00Z",
  updatedAt: "2026-11-09T00:00:00Z",
};

beforeEach(async () => {
  vi.useFakeTimers();
  vi.clearAllMocks();
  auth.user = { id: "u1" };
  setCurrentWorkspace("review", "ws-review");
  await Promise.resolve();
  useDocDraftStore.getState().clearDraft();
  vi.mocked(api.updateDoc).mockImplementation(async (_id, data) => ({
    ...doc,
    ...data,
    revision: data.baseRevision + 1,
    versionCount: data.baseRevision + 1,
  }));
});
afterEach(() => {
  auth.user = null;
  cleanup();
  setCurrentWorkspace(null, null);
  vi.clearAllTimers();
  vi.useRealTimers();
});
function mount(strict = false) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const view = renderHook(() => useDocDraft(doc), {
    wrapper: ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>
        {strict ? <StrictMode>{children}</StrictMode> : children}
      </QueryClientProvider>
    ),
  });
  return { ...view, client };
}

function deferredSave() {
  let resolve!: (value: Doc) => void;
  const promise = new Promise<Doc>((done) => {
    resolve = done;
  });
  vi.mocked(api.updateDoc).mockReturnValueOnce(promise);
  return resolve;
}

it("saves after the debounce while the editor stays mounted", async () => {
  const view = mount();
  act(() => view.result.current.change({ content: "Edited" }));
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1600);
  });
  expect(api.updateDoc).toHaveBeenCalledWith(doc.id, {
    title: doc.title,
    content: "Edited",
    baseRevision: 1,
  });
});

it("saves the last edit when navigating away during the debounce", async () => {
  const view = mount();
  act(() => view.result.current.change({ content: "Edited" }));
  await act(async () => {
    await vi.advanceTimersByTimeAsync(100);
  });
  view.unmount();
  await act(async () => {
    await vi.advanceTimersByTimeAsync(5000);
  });
  expect(api.updateDoc).toHaveBeenCalledWith(doc.id, {
    title: doc.title,
    content: "Edited",
    baseRevision: 1,
  });
  expect(api.updateDoc).toHaveBeenCalledTimes(1);
  expect(useDocDraftStore.getState().draft.entries).toEqual({});
});

it("keeps the debounce while typing and saves both the final title and content on exit", async () => {
  const view = mount(true);
  act(() => view.result.current.change({ title: "Renamed" }));
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1000);
  });
  act(() => view.result.current.change({ content: "Final edit" }));
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1000);
  });
  expect(api.updateDoc).not.toHaveBeenCalled();
  view.unmount();
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0);
  });
  expect(api.updateDoc).toHaveBeenCalledExactlyOnceWith(doc.id, {
    title: "Renamed",
    content: "Final edit",
    baseRevision: 1,
  });
});

it("includes the final update published by child editor cleanup", async () => {
  const client = new QueryClient();
  function EditorCleanup({
    change,
  }: {
    change: ReturnType<typeof useDocDraft>["change"];
  }) {
    useEffect(() => () => change({ content: "Last editor update" }), [change]);
    return null;
  }
  function Editor() {
    const { change } = useDocDraft(doc);
    return <EditorCleanup change={change} />;
  }
  const { render } = await import("@testing-library/react");
  const view = render(
    <QueryClientProvider client={client}>
      <Editor />
    </QueryClientProvider>,
  );
  view.unmount();
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0);
  });
  expect(api.updateDoc).toHaveBeenCalledExactlyOnceWith(doc.id, {
    title: doc.title,
    content: "Last editor update",
    baseRevision: 1,
  });
});

it("drains edits made during an in-flight save after navigation using its new revision", async () => {
  const resolve = deferredSave();
  const view = mount();
  act(() => view.result.current.change({ content: "First edit" }));
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1500);
  });
  act(() =>
    view.result.current.change({ title: "Renamed", content: "Final edit" }),
  );
  view.unmount();
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0);
  });
  expect(api.updateDoc).toHaveBeenCalledTimes(1);

  await act(async () => {
    resolve({ ...doc, content: "First edit", revision: 2 });
  });

  expect(api.updateDoc).toHaveBeenNthCalledWith(2, doc.id, {
    title: "Renamed",
    content: "Final edit",
    baseRevision: 2,
  });
  expect(useDocDraftStore.getState().draft.entries).toEqual({});
});

it.each(["error", "conflict"])(
  "preserves the local draft on %s without retrying on unmount",
  async (kind) => {
    const { ApiError } = await import("@multica/core/api");
    vi.mocked(api.updateDoc).mockRejectedValue(
      kind === "conflict"
        ? new ApiError("Changed", 409, "Conflict", {
            code: "revision_conflict",
          })
        : new Error("Offline"),
    );
    const view = mount();
    act(() => view.result.current.change({ content: "Local edit" }));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1500);
    });
    expect(view.result.current.status).toBe(kind);
    view.unmount();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000);
    });
    expect(api.updateDoc).toHaveBeenCalledTimes(1);
    expect(
      useDocDraftStore.getState().draft.entries["ws-review:doc-review"],
    ).toMatchObject({
      content: "Local edit",
      baseContent: "Original",
      baseRevision: 1,
    });
  },
);

it("retains the draft if the navigation flush conflicts and preserves it when reloading", async () => {
  const { ApiError } = await import("@multica/core/api");
  vi.mocked(api.updateDoc).mockRejectedValue(
    new ApiError("Changed", 409, "Conflict", { code: "revision_conflict" }),
  );
  const view = mount();
  act(() => view.result.current.change({ content: "Local edit" }));
  view.unmount();
  await act(async () => {
    await vi.advanceTimersByTimeAsync(5000);
  });
  expect(api.updateDoc).toHaveBeenCalledTimes(1);
  const reopened = mount();
  expect(reopened.result.current.draft.content).toBe("Local edit");
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1500);
  });
  expect(reopened.result.current.status).toBe("conflict");
  vi.mocked(api.getDoc).mockResolvedValue({
    ...doc,
    content: "Remote edit",
    revision: 2,
  });
  await act(async () => {
    await reopened.result.current.reload();
  });
  expect(reopened.result.current.draft).toMatchObject({
    content: "Remote edit",
    baseRevision: 2,
    backup: { title: doc.title, content: "Local edit" },
  });
});

it("does not flush or resurrect drafts after session cleanup", async () => {
  const view = mount();
  act(() => view.result.current.change({ content: "A's draft" }));
  act(() => {
    clearClientSessionData(view.client);
    auth.user = null;
  });
  view.unmount();
  // A pending child editor callback must not restore the erased draft.
  act(() => view.result.current.change({ content: "A's pending update" }));
  await act(async () => {
    await vi.advanceTimersByTimeAsync(5000);
  });
  expect(api.updateDoc).not.toHaveBeenCalled();
  expect(useDocDraftStore.getState().draft.entries).toEqual({});
});

it("does not repopulate another account's cache or draft from a late save response", async () => {
  const resolve = deferredSave();
  const view = mount();
  act(() => view.result.current.change({ content: "A's edit" }));
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1500);
  });
  act(() => {
    clearClientSessionData(view.client);
    auth.user = { id: "u2" };
    setDocDraft("ws-review:doc-review", {
      title: doc.title,
      content: "B's edit",
      baseTitle: doc.title,
      baseContent: doc.content,
      baseRevision: 1,
    });
  });
  const nextAccountDrafts = useDocDraftStore.getState().draft.entries;
  view.unmount();
  await act(async () => {
    resolve({ ...doc, content: "A's edit", revision: 2 });
  });
  expect(view.client.getQueryCache().getAll()).toEqual([]);
  expect(useDocDraftStore.getState().draft.entries).toEqual(nextAccountDrafts);
  expect(api.updateDoc).toHaveBeenCalledTimes(1);
});

it("does not send the old document to a different workspace on exit", async () => {
  const view = mount();
  act(() => view.result.current.change({ content: "A's draft" }));
  act(() => setCurrentWorkspace("other", "ws-other"));
  view.unmount();
  await act(async () => {
    await vi.advanceTimersByTimeAsync(5000);
  });
  expect(api.updateDoc).not.toHaveBeenCalled();
});

it("settles an in-flight save without writing the old workspace's draft into the new one", async () => {
  const resolve = deferredSave();
  const view = mount();
  act(() => view.result.current.change({ content: "A's edit" }));
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1500);
  });
  act(() => setCurrentWorkspace("other", "ws-other"));
  await act(async () => {
    resolve({ ...doc, content: "A's edit", revision: 2 });
  });
  expect(view.result.current.status).toBe("saved");
  expect(view.client.getQueryCache().getAll()).toEqual([]);
  expect(useDocDraftStore.getState().draft.entries).toEqual({});
});
