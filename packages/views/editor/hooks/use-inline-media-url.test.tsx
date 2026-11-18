import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { focusManager, onlineManager, QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@multica/core/api/client";
import type { ReactNode } from "react";
import {
  useFreshStreamedMediaURL,
  useResignedInlineMedia,
  __resetInlineMediaBlobCacheForTests,
} from "./use-inline-media-url";

const { getAttachment, getAttachmentForDownload, getAttachmentBlob, getBaseUrl } = vi.hoisted(() => ({
  getAttachment: vi.fn(), getAttachmentForDownload: vi.fn(), getAttachmentBlob: vi.fn(), getBaseUrl: vi.fn(() => "https://api.example.test"),
}));
vi.mock("@multica/core/api", () => ({ api: { getAttachment, getAttachmentForDownload, getAttachmentBlob, getBaseUrl } }));
const id = "11111111-2222-3333-4444-555555555555";
const stable = `/api/attachments/${id}/download`;
const capability = (exp: number, sig = "fresh") => `/api/attachments/${id}/signed-download?exp=${exp}&sig=${sig}`;

function setup() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  return { client, wrapper };
}
afterEach(() => {
  cleanup();
  __resetInlineMediaBlobCacheForTests();
  vi.useRealTimers(); vi.clearAllMocks(); vi.unstubAllGlobals();
  focusManager.setFocused(undefined);
  onlineManager.setOnline(true);
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

describe("registered image viewer access", () => {
  const cached = "https://cdn.example.test/design.png?Signature=expired";
  const useOpenedImage = () =>
    useResignedInlineMedia(id, cached, true, { refreshOnMount: true });
  it("renews a cached signed image before handing it to the viewer", async () => {
    getAttachment.mockResolvedValue({
      id,
      download_url: "https://cdn.example.test/design.png?Signature=fresh",
    });
    const { wrapper } = setup();
    const h = renderHook(useOpenedImage, { wrapper });
    expect(h.result.current.pending).toBe(true);
    expect(h.result.current.url).toBe("");
    await waitFor(() =>
      expect(h.result.current.url).toContain("Signature=fresh"),
    );
    expect(getAttachment).toHaveBeenCalledWith(id);
    expect(getAttachmentBlob).not.toHaveBeenCalled();
  });
  it("reuses a recent signature on reopen and refreshes after the short cache window or signature expiry", async () => {
    vi.useFakeTimers();
    getAttachment.mockImplementation(async () => ({
      id,
      download_url: `https://cdn.example.test/design.png?Signature=${getAttachment.mock.calls.length}`,
    }));
    const { client, wrapper } = setup();
    const first = renderHook(useOpenedImage, { wrapper });
    await act(async () => { await vi.advanceTimersByTimeAsync(10); });
    expect(first.result.current.url).toContain("Signature=1");
    first.unmount();
    await act(async () => { await vi.advanceTimersByTimeAsync(60 * 1000); });
    const recent = renderHook(useOpenedImage, { wrapper });
    expect(recent.result.current.pending).toBe(false);
    expect(recent.result.current.url).toContain("Signature=1");
    expect(getAttachment).toHaveBeenCalledTimes(1);
    recent.unmount();
    await act(async () => { await vi.advanceTimersByTimeAsync(5 * 60 * 1000); });
    const stale = renderHook(useOpenedImage, { wrapper });
    expect(stale.result.current.pending).toBe(true);
    expect(stale.result.current.url).toBe("");
    await act(async () => { await vi.advanceTimersByTimeAsync(10); });
    expect(stale.result.current.url).toContain("Signature=2");
    expect(getAttachment).toHaveBeenCalledTimes(2);
    // Keep the cached entry alive beyond the default 30-minute signature TTL.
    await act(async () => { await vi.advanceTimersByTimeAsync(31 * 60 * 1000); });
    stale.unmount();
    const expired = renderHook(useOpenedImage, { wrapper });
    expect(expired.result.current.url).toBe("");
    expect(expired.result.current.pending).toBe(true);
    await act(async () => { await vi.advanceTimersByTimeAsync(10); });
    expect(expired.result.current.url).toContain("Signature=3");
    expect(getAttachment).toHaveBeenCalledTimes(3);
    expired.unmount();
    client.clear();
  });
  it("waits for current authentication when metadata from an earlier open is stale", async () => {
    getAttachment.mockResolvedValueOnce({
      id,
      download_url: "https://cdn.example.test/first.png?Signature=first",
    });
    const { client, wrapper } = setup();
    const first = renderHook(useOpenedImage, { wrapper });
    await waitFor(() =>
      expect(first.result.current.url).toContain("Signature=first"),
    );
    first.unmount();
    client.setQueryData(["attachment-inline-resign", id], {
      id,
      download_url: "https://cdn.example.test/first.png?Signature=first",
    }, { updatedAt: Date.now() - 6 * 60 * 1000 });
    let finish!: (value: { id: string; download_url: string }) => void;
    getAttachment.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    const second = renderHook(useOpenedImage, { wrapper });
    expect(second.result.current.pending).toBe(true);
    expect(second.result.current.url).toBe("");
    await act(async () => {
      finish({
        id,
        download_url: "https://cdn.example.test/second.png?Signature=second",
      });
    });
    await waitFor(() =>
      expect(second.result.current.url).toContain("Signature=second"),
    );
    expect(getAttachment).toHaveBeenCalledTimes(2);
    client.clear();
  });
  it.each([403, 404])(
    "does not use a cached private image after metadata returns %s",
    async (status) => {
      const { client, wrapper } = setup();
      client.setQueryData(["attachment-inline-resign", id], {
        id,
        download_url: "https://cdn.example.test/previous.png?Signature=private",
      }, { updatedAt: Date.now() - 6 * 60 * 1000 });
      getAttachment.mockRejectedValue(
        new ApiError("not available", status, "Denied"),
      );
      const h = renderHook(useOpenedImage, { wrapper });
      await waitFor(() => expect(h.result.current.failed).toBe(true));
      expect(h.result.current.url).toBe("");
      expect(h.result.current.pending).toBe(false);
      expect(getAttachment).toHaveBeenCalledTimes(1);
      expect(getAttachmentBlob).not.toHaveBeenCalled();
    },
  );
  it("keeps an inline thumbnail lazy when fresh-on-open is not requested", () => {
    const { wrapper } = setup();
    const h = renderHook(() => useResignedInlineMedia(id, cached, true), {
      wrapper,
    });
    expect(h.result.current.url).toBe(cached);
    expect(getAttachment).not.toHaveBeenCalled();
  });
  it("leaves an external image without an attachment identity unchanged", () => {
    const { wrapper } = setup();
    const h = renderHook(
      () =>
        useResignedInlineMedia(undefined, cached, true, {
          refreshOnMount: true,
        }),
      { wrapper },
    );
    expect(h.result.current.url).toBe(cached);
    expect(h.result.current.pending).toBe(false);
    expect(getAttachment).not.toHaveBeenCalled();
  });
});

describe("fresh image access on proxy storage", () => {
  const cached = "https://cdn.example.test/design.png?Signature=expired";
  const useOpenedImage = () =>
    useResignedInlineMedia(id, cached, true, { refreshOnMount: true });
  it("keeps the authenticated byte fallback when metadata has no native signed URL", async () => {
    getAttachment.mockResolvedValue({ id, download_url: stable });
    getAttachmentBlob.mockResolvedValue(
      new Blob(["png"], { type: "image/png" }),
    );
    const create = vi.fn(() => "blob:fresh-image");
    vi.stubGlobal(
      "URL",
      class extends URL {
        static createObjectURL = create;
        static revokeObjectURL = vi.fn();
      },
    );
    const { wrapper } = setup();
    const h = renderHook(useOpenedImage, { wrapper });
    await waitFor(() => expect(h.result.current.url).toBe("blob:fresh-image"));
    expect(h.result.current.pending).toBe(false);
    expect(h.result.current.failed).toBe(false);
    expect(getAttachment).toHaveBeenCalledTimes(1);
    expect(getAttachmentBlob).toHaveBeenCalledWith(id);
  });
  it("does not show a blob from an earlier QueryClient before current authentication succeeds", async () => {
    getAttachment.mockResolvedValueOnce({ id, download_url: stable });
    getAttachmentBlob.mockResolvedValue(
      new Blob(["private png"], { type: "image/png" }),
    );
    vi.stubGlobal(
      "URL",
      class extends URL {
        static createObjectURL = vi.fn(() => "blob:previous-account");
        static revokeObjectURL = vi.fn();
      },
    );
    const first = renderHook(useOpenedImage, { wrapper: setup().wrapper });
    await waitFor(() =>
      expect(first.result.current.url).toBe("blob:previous-account"),
    );
    first.unmount();
    getAttachment.mockRejectedValueOnce(
      new ApiError("membership revoked", 403, "Forbidden"),
    );
    const second = renderHook(useOpenedImage, { wrapper: setup().wrapper });
    expect(second.result.current.url).toBe("");
    await waitFor(() => expect(second.result.current.failed).toBe(true));
    expect(second.result.current.url).toBe("");
    expect(getAttachmentBlob).toHaveBeenCalledTimes(1);
  });
  it("fails instead of rendering the old URL when authenticated image bytes are denied", async () => {
    getAttachment.mockResolvedValue({ id, download_url: stable });
    getAttachmentBlob.mockRejectedValue(
      new ApiError("denied", 403, "Forbidden"),
    );
    const h = renderHook(useOpenedImage, { wrapper: setup().wrapper });
    await waitFor(() => expect(h.result.current.failed).toBe(true));
    expect(h.result.current.url).toBe("");
    expect(getAttachmentBlob).toHaveBeenCalledTimes(1);
  });
  it("refreshes metadata when navigating to an image already cached by an inline thumbnail", async () => {
    const other = "aaaaaaaa-2222-3333-4444-555555555555";
    const { client, wrapper } = setup();
    client.setQueryData(["attachment-inline-resign", other], {
      id: other,
      download_url: "https://cdn.example.test/other.png?Signature=old",
    }, { updatedAt: Date.now() - 6 * 60 * 1000 });
    let finish!: (value: { id: string; download_url: string }) => void;
    getAttachment.mockImplementation((selected: string) =>
      selected === id
        ? Promise.resolve({
            id,
            download_url: "https://cdn.example.test/first.png?Signature=fresh",
          })
        : new Promise((resolve) => {
            finish = resolve;
          }),
    );
    const h = renderHook(
      ({ selected }) =>
        useResignedInlineMedia(selected, cached, true, {
          refreshOnMount: true,
        }),
      { wrapper, initialProps: { selected: id } },
    );
    await waitFor(() => expect(h.result.current.url).toContain("first.png"));
    h.rerender({ selected: other });
    expect(h.result.current.pending).toBe(true);
    expect(h.result.current.url).toBe("");
    await act(async () => {
      finish({
        id: other,
        download_url: "https://cdn.example.test/other.png?Signature=fresh",
      });
    });
    await waitFor(() =>
      expect(h.result.current.url).toContain("other.png?Signature=fresh"),
    );
    expect(getAttachment).toHaveBeenCalledWith(other);
  });
});

describe("image viewer current authorization regressions", () => {
  it("waits for an offline stale refresh and continues after reconnecting", async () => {
    onlineManager.setOnline(false);
    getAttachment.mockResolvedValueOnce({
      id,
      download_url: "https://cdn.example.test/private.png?Signature=reconnected",
    });
    const { client, wrapper } = setup();
    client.setQueryData(["attachment-inline-resign", id], {
      id,
      download_url: "https://cdn.example.test/private.png?Signature=old",
    }, { updatedAt: Date.now() - 6 * 60 * 1000 });
    const hook = renderHook(
      () => useResignedInlineMedia(id, stable, true, { refreshOnMount: true }),
      { wrapper },
    );
    expect(hook.result.current.url).toBe("");
    expect(hook.result.current.pending).toBe(true);
    expect(getAttachment).not.toHaveBeenCalled();
    await act(async () => { onlineManager.setOnline(true); });
    await waitFor(() => expect(hook.result.current.url).toContain("Signature=reconnected"));
    expect(hook.result.current.pending).toBe(false);
    expect(getAttachment).toHaveBeenCalledTimes(1);
  });

  it("uses the owning-workspace metadata endpoint for a pasted download link", async () => {
    getAttachment.mockRejectedValue(
      new ApiError("wrong workspace", 404, "Not Found"),
    );
    getAttachmentForDownload.mockResolvedValue({
      id,
      download_url: "https://cdn.example.test/pasted.png?Signature=new",
    });
    const hook = renderHook(
      () =>
        useResignedInlineMedia(id, stable, true, {
          refreshOnMount: true,
          metadataSource: "markdown-link",
        }),
      { wrapper: setup().wrapper },
    );
    await waitFor(() =>
      expect(hook.result.current.url).toContain("pasted.png"),
    );
    expect(getAttachmentForDownload).toHaveBeenCalledWith(id);
    expect(getAttachment).not.toHaveBeenCalled();
  });

  it("rejects malformed metadata instead of unlocking a previous private blob", async () => {
    getAttachment.mockResolvedValueOnce({ id, download_url: stable });
    getAttachmentBlob.mockResolvedValue(new Blob(["private"]));
    vi.stubGlobal(
      "URL",
      class extends URL {
        static createObjectURL = vi.fn(() => "blob:private");
        static revokeObjectURL = vi.fn();
      },
    );
    const first = renderHook(
      () => useResignedInlineMedia(id, stable, true, { refreshOnMount: true }),
      { wrapper: setup().wrapper },
    );
    await waitFor(() => expect(first.result.current.url).toBe("blob:private"));
    first.unmount();
    getAttachment.mockResolvedValueOnce({ id: "", download_url: "" });
    const second = renderHook(
      () => useResignedInlineMedia(id, stable, true, { refreshOnMount: true }),
      { wrapper: setup().wrapper },
    );
    await waitFor(() => expect(second.result.current.failed).toBe(true));
    expect(second.result.current.url).toBe("");
    expect(getAttachmentBlob).toHaveBeenCalledTimes(1);
  });
});

it("uses a new signed URL despite an earlier failure of the inactive byte path", async () => {
  const { client, wrapper } = setup();
  await client
    .fetchQuery({
      queryKey: ["attachment-inline-blob", id],
      queryFn: () =>
        Promise.reject(new ApiError("old failure", 403, "Forbidden")),
      retry: false,
    })
    .catch(() => {});
  getAttachment.mockResolvedValueOnce({
    id,
    download_url: "https://cdn.example.test/recovered.png?Signature=new",
  });
  const hook = renderHook(
    () => useResignedInlineMedia(id, stable, true, { refreshOnMount: true }),
    { wrapper },
  );
  await waitFor(() =>
    expect(hook.result.current.url).toContain("recovered.png"),
  );
  expect(hook.result.current.failed).toBe(false);
  expect(getAttachmentBlob).not.toHaveBeenCalled();
});
