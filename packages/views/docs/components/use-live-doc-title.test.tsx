import { act, cleanup, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import type { Doc } from "@multica/core/types";
import { api } from "@multica/core/api";
import { useLiveDocTitle } from "./use-live-doc-title";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
vi.mock("@multica/core/api", () => ({ api: { updateDoc: vi.fn() } }));

const doc: Doc = {
  id: "doc",
  workspaceId: "ws",
  folderId: null,
  title: "Runbook",
  content: "Body",
  revision: 1,
  versionCount: 1,
  createdByType: "member",
  createdById: "u1",
  updatedByType: "member",
  updatedById: "u1",
  createdAt: "2026-11-09T00:00:00Z",
  updatedAt: "2026-11-09T00:00:00Z",
};

beforeEach(() => {
  vi.useFakeTimers();
  vi.clearAllMocks();
  vi.mocked(api.updateDoc).mockImplementation(async (_id, data) => ({
    ...doc,
    title: (data.title ?? doc.title).trim(),
    revision: data.baseRevision + 1,
  }));
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

function mount(initial: { doc: Doc; enabled: boolean }) {
  const client = new QueryClient();
  return renderHook((props: { doc: Doc; enabled: boolean }) => useLiveDocTitle(props.doc, props.enabled), {
    initialProps: initial,
    wrapper: ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    ),
  });
}

it("saves after a pause, even while collaborators keep bumping the revision", async () => {
  const view = mount({ doc, enabled: true });
  act(() => view.result.current.change("Release runbook"));
  for (let revision = 2; revision <= 4; revision++) {
    await act(async () => {
      await vi.advanceTimersByTimeAsync(200);
    });
    view.rerender({ doc: { ...doc, revision }, enabled: true });
  }
  expect(api.updateDoc).not.toHaveBeenCalled();
  await act(async () => {
    await vi.advanceTimersByTimeAsync(200);
  });
  expect(api.updateDoc).toHaveBeenCalledExactlyOnceWith(doc.id, { baseRevision: 4, title: "Release runbook" });
});

it("saves a title typed just before leaving the page", async () => {
  const view = mount({ doc, enabled: true });
  act(() => view.result.current.change("Renamed"));
  view.unmount();
  expect(api.updateDoc).toHaveBeenCalledExactlyOnceWith(doc.id, { baseRevision: 1, title: "Renamed" });
});

it("saves at once when the field loses focus", async () => {
  const view = mount({ doc, enabled: true });
  act(() => view.result.current.change("Renamed"));
  act(() => view.result.current.blur());
  expect(api.updateDoc).toHaveBeenCalledExactlyOnceWith(doc.id, { baseRevision: 1, title: "Renamed" });
});

it("offers a retry after a failed save", async () => {
  vi.mocked(api.updateDoc).mockRejectedValueOnce(new Error("Offline"));
  const view = mount({ doc, enabled: true });
  act(() => view.result.current.change("Renamed"));
  await act(async () => {
    await vi.advanceTimersByTimeAsync(900);
  });
  expect(view.result.current.failed).toBe(true);
  await act(async () => {
    view.result.current.retry();
  });
  expect(api.updateDoc).toHaveBeenCalledTimes(2);
  expect(view.result.current.failed).toBe(false);
});

it("holds a title typed while connecting and saves it once live", async () => {
  const view = mount({ doc, enabled: false });
  act(() => view.result.current.change("Typed early"));
  await act(async () => {
    await vi.advanceTimersByTimeAsync(2000);
  });
  expect(api.updateDoc).not.toHaveBeenCalled();
  expect(view.result.current.value).toBe("Typed early");
  view.rerender({ doc, enabled: true });
  await act(async () => {
    await vi.advanceTimersByTimeAsync(900);
  });
  expect(api.updateDoc).toHaveBeenCalledExactlyOnceWith(doc.id, { baseRevision: 1, title: "Typed early" });
});

it("hands a title typed while connecting to the solo draft", () => {
  const view = mount({ doc, enabled: false });
  act(() => view.result.current.change("Typed early"));
  let taken: string | null = null;
  act(() => {
    taken = view.result.current.take();
  });
  expect(taken).toBe("Typed early");
  expect(view.result.current.value).toBe(doc.title);
  view.unmount();
  expect(api.updateDoc).not.toHaveBeenCalled();
});
