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
  useFeatureFlag: () => flag.enabled,
}));

import { useCommentDraft } from "./use-comment-draft";

const KEY = "new:issue-1" as CommentDraftKey;

beforeEach(() => {
  store.clear();
  flag.enabled = true;
  getDraft.mockClear();
  setDraft.mockClear();
  clearDraft.mockClear();
});

afterEach(() => vi.clearAllMocks());

describe("useCommentDraft", () => {
  it("seeds defaultValue from the stored draft on mount", () => {
    store.set(KEY, "half-written");
    const { result } = renderHook(() => useCommentDraft(KEY));
    expect(result.current.defaultValue).toBe("half-written");
    expect(result.current.saved).toBe(true);
  });

  it("starts empty and unsaved when there is no stored draft", () => {
    const { result } = renderHook(() => useCommentDraft(KEY));
    expect(result.current.defaultValue).toBe("");
    expect(result.current.saved).toBe(false);
  });

  it("persists non-empty input and flips saved on", () => {
    const { result } = renderHook(() => useCommentDraft(KEY));
    act(() => result.current.save("typing a reply"));
    expect(setDraft).toHaveBeenCalledWith(KEY, "typing a reply");
    expect(result.current.saved).toBe(true);
  });

  it("clears the draft when the input is emptied", () => {
    store.set(KEY, "x");
    const { result } = renderHook(() => useCommentDraft(KEY));
    act(() => result.current.save("   "));
    expect(clearDraft).toHaveBeenCalledWith(KEY);
    expect(result.current.saved).toBe(false);
  });

  it("clear() drops the stored draft and the hint", () => {
    store.set(KEY, "x");
    const { result } = renderHook(() => useCommentDraft(KEY));
    act(() => result.current.clear());
    expect(clearDraft).toHaveBeenCalledWith(KEY);
    expect(result.current.saved).toBe(false);
  });

  it("re-seeds when the composer is pointed at a new surface", () => {
    store.set("new:issue-1" as CommentDraftKey, "draft A");
    store.set("new:issue-2" as CommentDraftKey, "draft B");
    const { result, rerender } = renderHook(({ k }: { k: CommentDraftKey }) => useCommentDraft(k), {
      initialProps: { k: "new:issue-1" as CommentDraftKey },
    });
    expect(result.current.defaultValue).toBe("draft A");
    rerender({ k: "new:issue-2" as CommentDraftKey });
    expect(result.current.defaultValue).toBe("draft B");
    expect(result.current.saved).toBe(true);
  });

  it("is inert when the feature flag is off", () => {
    flag.enabled = false;
    store.set(KEY, "should be ignored");
    const { result } = renderHook(() => useCommentDraft(KEY));
    expect(result.current.defaultValue).toBe("");
    act(() => result.current.save("nope"));
    expect(setDraft).not.toHaveBeenCalled();
    expect(result.current.saved).toBe(false);
  });
});
