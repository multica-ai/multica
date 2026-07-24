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
