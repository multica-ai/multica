import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { focusManager, QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@multica/core/api/client";
import type { ReactNode } from "react";
import { useFreshStreamedMediaURL } from "./use-inline-media-url";

const { getAttachment, getAttachmentBlob, getBaseUrl } = vi.hoisted(() => ({
  getAttachment: vi.fn(), getAttachmentBlob: vi.fn(), getBaseUrl: vi.fn(() => "https://api.example.test"),
}));
vi.mock("@multica/core/api", () => ({ api: { getAttachment, getAttachmentBlob, getBaseUrl } }));
const id = "11111111-2222-3333-4444-555555555555";
const stable = `/api/attachments/${id}/download`;
const capability = (exp: number, sig = "fresh") => `/api/attachments/${id}/signed-download?exp=${exp}&sig=${sig}`;

function setup() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  return { client, wrapper };
}
afterEach(() => {
  cleanup(); vi.useRealTimers(); vi.clearAllMocks(); vi.unstubAllGlobals();
  focusManager.setFocused(undefined);
  getBaseUrl.mockReturnValue("https://api.example.test");
});

describe("short-lived streamed attachment capabilities", () => {
  it("resolves a relative capability against the API origin and fetches again on reopen", async () => {
    getAttachment.mockResolvedValue({ download_url: capability(Math.floor(Date.now() / 1000) + 60) });
    const { wrapper } = setup();
    const first = renderHook(() => useFreshStreamedMediaURL(id, stable, true), { wrapper });
    await waitFor(() => expect(first.result.current.url).toContain("https://api.example.test/api/attachments/"));
    first.unmount();
    const second = renderHook(() => useFreshStreamedMediaURL(id, stable, true), { wrapper });
    await waitFor(() => expect(getAttachment).toHaveBeenCalledTimes(2));
    expect(second.result.current.url).toContain("/signed-download?");
  });

  it("keeps an open stream stable beyond TTL and on window focus", async () => {
    vi.useFakeTimers();
    getAttachment.mockImplementation(async () => ({ download_url: capability(Math.floor(Date.now() / 1000) + 60, String(getAttachment.mock.calls.length)) }));
    const { wrapper } = setup();
    const hook = renderHook(() => useFreshStreamedMediaURL(id, stable, true), { wrapper });
    await act(async () => { await vi.advanceTimersByTimeAsync(10); });
    const original = hook.result.current.url;
    expect(original).toContain("/signed-download?");
    await act(async () => {
      await vi.advanceTimersByTimeAsync(65000);
      focusManager.setFocused(false);
      focusManager.setFocused(true);
      await vi.advanceTimersByTimeAsync(10);
    });
    expect(getAttachment).toHaveBeenCalledTimes(1);
    expect(hook.result.current.pending).toBe(false);
    expect(hook.result.current.url).toBe(original);
  });

  it("does not retry a permission denial during expiry recovery", async () => {
    vi.useFakeTimers();
    getAttachment.mockResolvedValueOnce({ download_url: capability(Math.floor(Date.now() / 1000) + 60) });
    getAttachment.mockRejectedValue(new ApiError("membership revoked", 403, "Forbidden"));
    const { wrapper } = setup();
    const hook = renderHook(() => useFreshStreamedMediaURL(id, stable, true), { wrapper });
    await act(async () => { await vi.advanceTimersByTimeAsync(10); });
    await act(async () => { await vi.advanceTimersByTimeAsync(61000); });
    await act(async () => { expect(await hook.result.current.refreshIfExpired()).toBe(false); });
    await act(async () => { await vi.advanceTimersByTimeAsync(10); });
    expect(getAttachment).toHaveBeenCalledTimes(2);
    await act(async () => { expect(await hook.result.current.refreshIfExpired()).toBe(false); });
    expect(getAttachment).toHaveBeenCalledTimes(2);
    expect(hook.result.current.pending).toBe(false);
  });

  it("uses a stable authenticated PDF URL on same-origin web only for proxy storage", async () => {
    getBaseUrl.mockReturnValue("");
    getAttachment.mockResolvedValue({ download_url: capability(Math.floor(Date.now() / 1000) + 60) });
    const { wrapper } = setup();
    const hook = renderHook(() => useFreshStreamedMediaURL(id, stable, true, true), { wrapper });
    await waitFor(() => expect(hook.result.current.pending).toBe(false));
    expect(hook.result.current.url).toBe(stable);
    expect(getAttachment).toHaveBeenCalledWith(id);
    expect(getAttachmentBlob).not.toHaveBeenCalled();
  });

  it.each([
    ["S3 presign", "https://storage.example.test/manual.pdf?X-Amz-Signature=fresh"],
    ["CloudFront", "https://cdn.example.test/manual.pdf?Signature=fresh&Key-Pair-Id=test"],
  ])("uses the fresh inline URL for same-origin web PDFs with %s", async (_mode, inlineURL) => {
    getBaseUrl.mockReturnValue("");
    getAttachment.mockResolvedValue({ download_url: inlineURL, attachment_download_url: `${inlineURL}&response-content-disposition=attachment` });
    const { wrapper } = setup();
    const hook = renderHook(() => useFreshStreamedMediaURL(id, stable, true, true), { wrapper });
    await waitFor(() => expect(hook.result.current.url).toBe(inlineURL));
    expect(hook.result.current.pending).toBe(false);
    expect(getAttachment).toHaveBeenCalledWith(id);
    expect(getAttachmentBlob).not.toHaveBeenCalled();
  });

  it("fetches a cross-origin proxy PDF once and releases its typed blob on close", async () => {
    const create = vi.fn((_blob: Blob) => "blob:private-pdf");
    const revoke = vi.fn();
    vi.stubGlobal("URL", class extends URL { static createObjectURL = create; static revokeObjectURL = revoke; });
    getAttachment.mockResolvedValue({ download_url: capability(Math.floor(Date.now() / 1000) + 60) });
    getAttachmentBlob.mockResolvedValue(new Blob(["%PDF"], { type: "text/html" }));
    const { wrapper } = setup();
    const hook = renderHook(() => useFreshStreamedMediaURL(id, stable, true, true), { wrapper });
    await waitFor(() => expect(hook.result.current.url).toBe("blob:private-pdf"));
    expect(create.mock.calls[0]![0].type).toBe("application/pdf");
    vi.useFakeTimers();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(65000);
      focusManager.setFocused(false); focusManager.setFocused(true);
      await vi.advanceTimersByTimeAsync(10);
    });
    expect(hook.result.current.url).toBe("blob:private-pdf");
    expect(getAttachment).toHaveBeenCalledTimes(1);
    expect(getAttachmentBlob).toHaveBeenCalledTimes(1);
    hook.unmount();
    expect(revoke).toHaveBeenCalledWith("blob:private-pdf");
  });

  it("cancels the PDF byte request when its viewer closes during loading", async () => {
    getAttachment.mockResolvedValue({ download_url: capability(Math.floor(Date.now() / 1000) + 60) });
    getAttachmentBlob.mockImplementation(() => new Promise(() => {}));
    const { wrapper } = setup();
    const hook = renderHook(() => useFreshStreamedMediaURL(id, stable, true, true), { wrapper });
    await waitFor(() => expect(getAttachmentBlob).toHaveBeenCalledTimes(1));
    const signal = getAttachmentBlob.mock.calls[0]![1] as AbortSignal;
    expect(signal.aborted).toBe(false);
    hook.unmount();
    expect(signal.aborted).toBe(true);
  });
});
