import { describe, it, expect, vi } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { useComposerSubmit } from "./use-composer-submit";
import type { ContentEditorRef } from "./content-editor";
import type { UploadGate } from "./use-upload-gate";

function editorWith(markdown: string) {
  return {
    current: { getMarkdown: () => markdown } as unknown as ContentEditorRef,
  };
}

/** Editor stub that records the focus calls `afterAccepted` makes. */
function focusTrackingEditor(markdown = "hello") {
  const calls = { focused: 0, blurred: 0 };
  const ref = {
    current: {
      getMarkdown: () => markdown,
      focus: () => { calls.focused += 1; },
      blur: () => { calls.blurred += 1; },
    } as unknown as ContentEditorRef,
  };
  return { ref, calls };
}

/** `afterAccepted` defers a frame; flush it. */
function nextFrame() {
  return new Promise((resolve) => requestAnimationFrame(() => resolve(null)));
}

const openGate: UploadGate = {
  uploading: false,
  onUploadingChange: () => {},
  isBlocked: () => false,
};

const blockedGate: UploadGate = { ...openGate, isBlocked: () => true };

describe("useComposerSubmit", () => {
  it("does not submit empty content", async () => {
    const onSubmit = vi.fn().mockResolvedValue(true);
    const { result } = renderHook(() =>
      useComposerSubmit({ editorRef: editorWith("   \n"), uploadGate: openGate, onSubmit }),
    );
    await act(async () => { await result.current.submit(); });
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("clears (onAccepted) only when the server accepts", async () => {
    const onAccepted = vi.fn();
    const { result } = renderHook(() =>
      useComposerSubmit({
        editorRef: editorWith("hello"),
        uploadGate: openGate,
        onSubmit: vi.fn().mockResolvedValue(true),
        onAccepted,
      }),
    );
    await act(async () => { await result.current.submit(); });
    expect(onAccepted).toHaveBeenCalledTimes(1);
  });

  it("keeps the draft when the server rejects", async () => {
    const onAccepted = vi.fn();
    const { result } = renderHook(() =>
      useComposerSubmit({
        editorRef: editorWith("hello"),
        uploadGate: openGate,
        onSubmit: vi.fn().mockResolvedValue(false),
        onAccepted,
      }),
    );
    await act(async () => { await result.current.submit(); });
    expect(onAccepted).not.toHaveBeenCalled();
  });

  it("keeps the draft when the send throws", async () => {
    const onAccepted = vi.fn();
    const { result } = renderHook(() =>
      useComposerSubmit({
        editorRef: editorWith("hello"),
        uploadGate: openGate,
        onSubmit: vi.fn().mockRejectedValue(new Error("network")),
        onAccepted,
      }),
    );
    await act(async () => { await result.current.submit(); });
    expect(onAccepted).not.toHaveBeenCalled();
  });

  it("blocks while an upload is in flight", async () => {
    const onSubmit = vi.fn().mockResolvedValue(true);
    const { result } = renderHook(() =>
      useComposerSubmit({ editorRef: editorWith("hello"), uploadGate: blockedGate, onSubmit }),
    );
    await act(async () => { await result.current.submit(); });
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("single-flights concurrent submits", async () => {
    let resolve!: (v: boolean) => void;
    const onSubmit = vi.fn().mockImplementation(() => new Promise<boolean>((r) => { resolve = r; }));
    const { result } = renderHook(() =>
      useComposerSubmit({ editorRef: editorWith("hello"), uploadGate: openGate, onSubmit }),
    );
    await act(async () => {
      const a = result.current.submit();
      const b = result.current.submit();
      resolve(true);
      await Promise.all([a, b]);
    });
    expect(onSubmit).toHaveBeenCalledTimes(1);
  });
});

// `afterAccepted` — where the caret goes once a send is accepted. Each surface
// picks its own policy; the mechanics (defer a frame, respect unmount, don't
// steal focus the user moved elsewhere) live in the hook.
describe("useComposerSubmit afterAccepted", () => {
  it("defaults to leaving focus alone", async () => {
    const { ref, calls } = focusTrackingEditor();
    const { result } = renderHook(() =>
      useComposerSubmit({
        editorRef: ref,
        uploadGate: openGate,
        onSubmit: vi.fn().mockResolvedValue(true),
      }),
    );
    await act(async () => { await result.current.submit(); });
    await act(async () => { await nextFrame(); });
    expect(calls.focused).toBe(0);
    expect(calls.blurred).toBe(0);
  });

  it("refocuses the editor after an accepted submit", async () => {
    const { ref, calls } = focusTrackingEditor();
    const { result } = renderHook(() =>
      useComposerSubmit({
        editorRef: ref,
        uploadGate: openGate,
        onSubmit: vi.fn().mockResolvedValue(true),
        afterAccepted: "refocus",
      }),
    );
    await act(async () => { await result.current.submit(); });
    // Deferred on purpose: a synchronous grab loses to a dialog's focus trap.
    expect(calls.focused).toBe(0);
    await act(async () => { await nextFrame(); });
    expect(calls.focused).toBe(1);
  });

  it("blurs the editor when the surface hands attention elsewhere", async () => {
    const { ref, calls } = focusTrackingEditor();
    const { result } = renderHook(() =>
      useComposerSubmit({
        editorRef: ref,
        uploadGate: openGate,
        onSubmit: vi.fn().mockResolvedValue(true),
        afterAccepted: "blur",
      }),
    );
    await act(async () => { await result.current.submit(); });
    await act(async () => { await nextFrame(); });
    expect(calls.blurred).toBe(1);
    expect(calls.focused).toBe(0);
  });

  it("does nothing when the server rejects", async () => {
    const { ref, calls } = focusTrackingEditor();
    const { result } = renderHook(() =>
      useComposerSubmit({
        editorRef: ref,
        uploadGate: openGate,
        onSubmit: vi.fn().mockResolvedValue(false),
        afterAccepted: "refocus",
      }),
    );
    await act(async () => { await result.current.submit(); });
    await act(async () => { await nextFrame(); });
    expect(calls.focused).toBe(0);
  });

  it("resolves a function mode at accept time, not a frame later", async () => {
    const { ref, calls } = focusTrackingEditor();
    let allow = true;
    const { result } = renderHook(() =>
      useComposerSubmit({
        editorRef: ref,
        uploadGate: openGate,
        onSubmit: vi.fn().mockResolvedValue(true),
        afterAccepted: () => (allow ? "refocus" : "none"),
      }),
    );
    await act(async () => { await result.current.submit(); });
    // Flipping after acceptance must not retroactively cancel the decision —
    // chat's flag is only true for the instant around the commit.
    allow = false;
    await act(async () => { await nextFrame(); });
    expect(calls.focused).toBe(1);
  });

  it("does not steal focus the user moved outside the composer", async () => {
    const { ref, calls } = focusTrackingEditor();
    const container = document.createElement("div");
    const outside = document.createElement("input");
    document.body.append(container, outside);
    outside.focus();
    const { result } = renderHook(() =>
      useComposerSubmit({
        editorRef: ref,
        uploadGate: openGate,
        onSubmit: vi.fn().mockResolvedValue(true),
        afterAccepted: "refocus",
        containerRef: { current: container },
      }),
    );
    await act(async () => { await result.current.submit(); });
    await act(async () => { await nextFrame(); });
    expect(calls.focused).toBe(0);
    container.remove();
    outside.remove();
  });

  it("reclaims focus that is still inside the composer", async () => {
    const { ref, calls } = focusTrackingEditor();
    const container = document.createElement("div");
    const sendButton = document.createElement("button");
    container.append(sendButton);
    document.body.append(container);
    sendButton.focus();
    const { result } = renderHook(() =>
      useComposerSubmit({
        editorRef: ref,
        uploadGate: openGate,
        onSubmit: vi.fn().mockResolvedValue(true),
        afterAccepted: "refocus",
        containerRef: { current: container },
      }),
    );
    await act(async () => { await result.current.submit(); });
    await act(async () => { await nextFrame(); });
    expect(calls.focused).toBe(1);
    container.remove();
  });

  it("does not touch an editor whose composer unmounted mid-flight", async () => {
    const { ref, calls } = focusTrackingEditor();
    let resolve!: (v: boolean) => void;
    const { result, unmount } = renderHook(() =>
      useComposerSubmit({
        editorRef: ref,
        uploadGate: openGate,
        onSubmit: () => new Promise<boolean>((r) => { resolve = r; }),
        afterAccepted: "refocus",
      }),
    );
    let pending!: Promise<void>;
    act(() => { pending = result.current.submit(); });
    await act(async () => { resolve(true); await pending; });
    unmount();
    await act(async () => { await nextFrame(); });
    expect(calls.focused).toBe(0);
  });
});
