import { describe, it, expect, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { useLazyEditor, type LazyEditorHandle } from "./use-lazy-editor";

function makeHandle() {
  return { focus: vi.fn(), focusAtCoords: vi.fn(), uploadFile: vi.fn() };
}

describe("useLazyEditor", () => {
  it("starts static, activates on intent, focuses at the click coords once ready", () => {
    const handle = makeHandle();
    const editorRef = { current: handle as LazyEditorHandle };
    const { result } = renderHook(() => useLazyEditor({ editorRef }));

    expect(result.current.active).toBe(false);
    expect(result.current.ready).toBe(false);

    act(() => result.current.activate({ x: 10, y: 20 }));
    expect(result.current.active).toBe(true);
    expect(result.current.ready).toBe(false);

    act(() => result.current.onReady());
    expect(result.current.ready).toBe(true);
    expect(handle.focusAtCoords).toHaveBeenCalledWith({ x: 10, y: 20 });
  });

  it("mounts immediately when initialActive is set (unsent draft)", () => {
    const editorRef = { current: makeHandle() as LazyEditorHandle };
    const { result } = renderHook(() => useLazyEditor({ editorRef, initialActive: true }));

    expect(result.current.active).toBe(true);
  });

  it("queues uploads against the stand-in and flushes them on ready", () => {
    const handle = makeHandle();
    const editorRef = { current: handle as LazyEditorHandle };
    const { result } = renderHook(() => useLazyEditor({ editorRef }));

    const file = new File(["x"], "x.txt", { type: "text/plain" });
    act(() => result.current.uploadOrQueue([file]));
    // Queuing summons the editor but must not upload into a dead handle.
    expect(result.current.active).toBe(true);
    expect(handle.uploadFile).not.toHaveBeenCalled();

    act(() => result.current.onReady());
    expect(handle.uploadFile).toHaveBeenCalledWith(file);
  });

  it("keeps an editable stand-in alive until IME composition ends", () => {
    let pendingContent = "w";
    const handle = {
      ...makeHandle(),
      focusAtEnd: vi.fn(),
      adoptContent: vi.fn(),
    };
    const editorRef = { current: handle as LazyEditorHandle };
    const { result } = renderHook(() =>
      useLazyEditor({
        editorRef,
        getPendingContent: () => pendingContent,
      }),
    );

    act(() => result.current.activate());
    act(() => result.current.onCompositionStart());
    act(() => result.current.onReady());

    expect(result.current.ready).toBe(false);
    expect(handle.adoptContent).not.toHaveBeenCalled();

    pendingContent = "我";
    act(() => result.current.onCompositionEnd());

    expect(result.current.ready).toBe(true);
    expect(handle.adoptContent).toHaveBeenCalledWith("我");
    expect(handle.focusAtEnd).toHaveBeenCalled();
  });

  it("cancels a queued ready swap when composition starts before its commit", () => {
    let pendingContent = "";
    const handle = {
      ...makeHandle(),
      focusAtEnd: vi.fn(),
      adoptContent: vi.fn(),
    };
    const editorRef = { current: handle as LazyEditorHandle };
    const { result } = renderHook(() =>
      useLazyEditor({
        editorRef,
        getPendingContent: () => pendingContent,
      }),
    );

    act(() => result.current.activate());
    act(() => {
      result.current.onReady();
      result.current.onCompositionStart();
    });

    // React may batch the ready update with the next native input event. The
    // composition-start update must win that batch so the focused shell stays.
    expect(result.current.ready).toBe(false);
    expect(handle.adoptContent).toHaveBeenCalledWith("");
    expect(handle.focusAtEnd).not.toHaveBeenCalled();

    pendingContent = "我";
    act(() => result.current.onCompositionEnd());

    expect(result.current.ready).toBe(true);
    expect(handle.adoptContent).toHaveBeenLastCalledWith("我");
    expect(handle.focusAtEnd).toHaveBeenCalled();
  });

  it("resets to the stand-in during the render that changes resetKey", () => {
    const editorRef = { current: makeHandle() as LazyEditorHandle };
    const { result, rerender } = renderHook(
      ({ key }: { key: string }) => useLazyEditor({ editorRef, resetKey: key }),
      { initialProps: { key: "issue-1" } },
    );

    act(() => result.current.activate());
    act(() => result.current.onReady());
    expect(result.current.ready).toBe(true);

    // Same-render reset: after the rerender that carries the new key, the
    // hook must already report the stand-in state — an effect-based reset
    // would let a keyed editor mount once for the new subject first.
    rerender({ key: "issue-2" });
    expect(result.current.active).toBe(false);
    expect(result.current.ready).toBe(false);
  });
});
