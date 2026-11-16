import { describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { useAttachmentDownloadState } from "./use-attachment-download-state";

describe("useAttachmentDownloadState", () => {
  it("blocks synchronous reentry before React renders the pending state", async () => {
    const pending = Promise.withResolvers<void>();
    const download = vi.fn(() => pending.promise);
    const { result } = renderHook(() => useAttachmentDownloadState(download));

    act(() => {
      result.current.handleDownload();
      result.current.handleDownload();
    });

    expect(download).toHaveBeenCalledOnce();
    expect(result.current.downloading).toBe(true);
    await act(async () => pending.resolve());
    expect(result.current.downloading).toBe(false);
  });

  it("allows retry after rejection", async () => {
    const pending = Promise.withResolvers<void>();
    const download = vi.fn<() => void | Promise<void>>().mockReturnValueOnce(pending.promise);
    const { result } = renderHook(() => useAttachmentDownloadState(download));
    act(() => result.current.handleDownload());

    await act(async () => pending.reject(new Error("download failed")));

    expect(result.current.downloading).toBe(false);
    act(() => result.current.handleDownload());
    expect(download).toHaveBeenCalledTimes(2);
  });

  it("keeps synchronous callbacks immediately available", () => {
    const download = vi.fn();
    const { result } = renderHook(() => useAttachmentDownloadState(download));

    act(() => result.current.handleDownload());
    act(() => result.current.handleDownload());

    expect(result.current.downloading).toBe(false);
    expect(download).toHaveBeenCalledTimes(2);
  });

  it("releases the guard if a synchronous callback throws", () => {
    const error = new Error("download failed");
    const download = vi.fn().mockImplementationOnce(() => {
      throw error;
    });
    const { result } = renderHook(() => useAttachmentDownloadState(download));

    act(() => expect(result.current.handleDownload).toThrow(error));

    expect(result.current.downloading).toBe(false);
    act(() => result.current.handleDownload());
    expect(download).toHaveBeenCalledTimes(2);
  });

  it("retains each file's pending state when the viewer switches files", async () => {
    const first = Promise.withResolvers<void>();
    const second = Promise.withResolvers<void>();
    const download = vi.fn().mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
    const { result, rerender } = renderHook(
      ({ key }) => useAttachmentDownloadState(download, key),
      { initialProps: { key: "first" } },
    );
    act(() => result.current.handleDownload());

    rerender({ key: "second" });
    expect(result.current.downloading).toBe(false);
    act(() => result.current.handleDownload());

    rerender({ key: "first" });
    expect(result.current.downloading).toBe(true);
    act(() => result.current.handleDownload());
    expect(download).toHaveBeenCalledTimes(2);

    await act(async () => first.resolve());
    expect(result.current.downloading).toBe(false);
    rerender({ key: "second" });
    expect(result.current.downloading).toBe(true);
    await act(async () => second.resolve());
    expect(result.current.downloading).toBe(false);
  });
});
