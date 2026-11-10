import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { useFreshStreamedMediaURL } from "./use-inline-media-url";

const { getAttachment, getBaseUrl } = vi.hoisted(() => ({
  getAttachment: vi.fn(), getBaseUrl: vi.fn(() => "https://api.example.test"),
}));
vi.mock("@multica/core/api", () => ({ api: { getAttachment, getBaseUrl } }));
const id = "11111111-2222-3333-4444-555555555555";
const stable = `/api/attachments/${id}/download`;
const capability = (exp: number, sig = "fresh") => `/api/attachments/${id}/signed-download?exp=${exp}&sig=${sig}`;

function setup() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  return { client, wrapper };
}
afterEach(() => { cleanup(); vi.useRealTimers(); vi.clearAllMocks(); });

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

  it("renews before the 60-second expiry without a loading gap", async () => {
    vi.useFakeTimers();
    getAttachment.mockImplementation(async () => ({ download_url: capability(Math.floor(Date.now() / 1000) + 60, String(getAttachment.mock.calls.length)) }));
    const { wrapper } = setup();
    const hook = renderHook(() => useFreshStreamedMediaURL(id, stable, true), { wrapper });
    await act(async () => { await vi.advanceTimersByTimeAsync(10); });
    const original = hook.result.current.url;
    expect(original).toContain("/signed-download?");
    await act(async () => { await vi.advanceTimersByTimeAsync(56000); });
    expect(getAttachment).toHaveBeenCalledTimes(2);
    expect(hook.result.current.pending).toBe(false);
    expect(hook.result.current.url).not.toBe(original);
  });

  it("does not keep serving an expired capability when authorization fails", async () => {
    vi.useFakeTimers();
    getAttachment.mockResolvedValueOnce({ download_url: capability(Math.floor(Date.now() / 1000) + 60) });
    getAttachment.mockRejectedValue(new Error("membership revoked"));
    const { wrapper } = setup();
    const hook = renderHook(() => useFreshStreamedMediaURL(id, stable, true), { wrapper });
    await act(async () => { await vi.advanceTimersByTimeAsync(10); });
    await act(async () => { await vi.advanceTimersByTimeAsync(61000); });
    expect(hook.result.current.url).toBe(stable);
    expect(hook.result.current.pending).toBe(false);
  });
});
