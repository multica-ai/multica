import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { CommentDraftKey } from "@multica/core/issues/stores";

// In-memory stand-in for useCommentDraftStore so the hook is tested in
// isolation from localStorage / workspace partitioning.
const store = new Map<string, string>();
const getDraft = vi.fn((k: string) => store.get(k));
const setDraft = vi.fn((k: string, v: string) => void store.set(k, v));
const clearDraft = vi.fn((k: string) => void store.delete(k));

vi.mock("@multica/core/issues/stores", () => {
  const useCommentDraftStore = (selector: (s: unknown) => unknown) =>
    selector({ getDraft, setDraft, clearDraft, drafts: {} });
  useCommentDraftStore.getState = () => ({ getDraft, setDraft, clearDraft });
  return { useCommentDraftStore };
});

const flag = { enabled: true };
vi.mock("@multica/cerebro-feature-flags", () => ({
  useFlagValue: () => flag.enabled,
}));

import { useDescriptionDraft } from "./use-description-draft";

const KEY = "desc:issue-1" as CommentDraftKey;

beforeEach(() => {
  store.clear();
  flag.enabled = true;
  getDraft.mockClear();
  setDraft.mockClear();
  clearDraft.mockClear();
});

afterEach(() => vi.clearAllMocks());

describe("useDescriptionDraft", () => {
  it("has no recoverable draft when nothing is stored", () => {
    const { result } = renderHook(() => useDescriptionDraft("issue-1", "server text"));
    expect(result.current.hasRecoverableDraft).toBe(false);
    expect(result.current.draftValue).toBe("");
  });

  it("has no recoverable draft when the stored draft matches the server value", () => {
    store.set(KEY, "server text");
    const { result } = renderHook(() => useDescriptionDraft("issue-1", "server text"));
    expect(result.current.hasRecoverableDraft).toBe(false);
  });

  it("surfaces a recoverable draft when it differs from the server value", () => {
    store.set(KEY, "half-typed edit");
    const { result } = renderHook(() => useDescriptionDraft("issue-1", "server text"));
    expect(result.current.hasRecoverableDraft).toBe(true);
    expect(result.current.draftValue).toBe("half-typed edit");
  });

  it("persists non-empty input via save()", () => {
    const { result } = renderHook(() => useDescriptionDraft("issue-1", "server text"));
    act(() => result.current.save("typing a new description"));
    expect(setDraft).toHaveBeenCalledWith(KEY, "typing a new description");
  });

  it("clears the stored draft when save() is called with empty input", () => {
    store.set(KEY, "x");
    const { result } = renderHook(() => useDescriptionDraft("issue-1", "server text"));
    act(() => result.current.save("   "));
    expect(clearDraft).toHaveBeenCalledWith(KEY);
  });

  it("discard() drops the stored draft and hides the banner", () => {
    store.set(KEY, "half-typed edit");
    const { result } = renderHook(() => useDescriptionDraft("issue-1", "server text"));
    expect(result.current.hasRecoverableDraft).toBe(true);
    act(() => result.current.discard());
    expect(clearDraft).toHaveBeenCalledWith(KEY);
    expect(result.current.hasRecoverableDraft).toBe(false);
  });

  it("dismissBanner() hides the banner without touching storage", () => {
    store.set(KEY, "half-typed edit");
    const { result } = renderHook(() => useDescriptionDraft("issue-1", "server text"));
    act(() => result.current.dismissBanner());
    expect(clearDraft).not.toHaveBeenCalled();
    expect(result.current.hasRecoverableDraft).toBe(false);
    expect(store.get(KEY)).toBe("half-typed edit");
  });

  it("re-seeds when pointed at a new issue", () => {
    store.set("desc:issue-1" as CommentDraftKey, "draft A");
    store.set("desc:issue-2" as CommentDraftKey, "draft B");
    const { result, rerender } = renderHook(
      ({ id, server }: { id: string; server: string }) => useDescriptionDraft(id, server),
      { initialProps: { id: "issue-1", server: "" } },
    );
    expect(result.current.draftValue).toBe("draft A");
    rerender({ id: "issue-2", server: "" });
    expect(result.current.draftValue).toBe("draft B");
  });

  it("is inert when the feature flag is off", () => {
    flag.enabled = false;
    store.set(KEY, "should be ignored");
    const { result } = renderHook(() => useDescriptionDraft("issue-1", "server text"));
    expect(result.current.hasRecoverableDraft).toBe(false);
    act(() => result.current.save("nope"));
    expect(setDraft).not.toHaveBeenCalled();
  });
});
