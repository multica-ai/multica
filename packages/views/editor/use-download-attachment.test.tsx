import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";

// Hoisted mock for the API singleton: vi.mock factories cannot reference
// outside-of-scope vars, but vi.hoisted runs before the import graph.
const getAttachmentMock = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: { getAttachment: getAttachmentMock },
}));

vi.mock("../platform", () => ({
  openExternal: vi.fn(),
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

vi.mock("../i18n", () => ({
  useT: () => ({ t: (sel: (s: { attachment: { download_failed: string } }) => string) => sel({ attachment: { download_failed: "Couldn't fetch a download link. Try again in a moment." } }) }),
}));

import { useDownloadAttachment } from "./use-download-attachment";
import { openExternal } from "../platform";
import { toast } from "sonner";

const SIGNED_URL =
  "https://static.example.test/file.md?Policy=p&Signature=s&Key-Pair-Id=k";

beforeEach(() => {
  vi.clearAllMocks();
});

afterEach(() => {
  vi.restoreAllMocks();
  // Scrub the desktop bridge between tests so suites don't leak state.
  delete (window as unknown as { desktopAPI?: unknown }).desktopAPI;
});

describe("useDownloadAttachment (web)", () => {
  it("opens a placeholder tab synchronously, then navigates it to the freshly signed URL", async () => {
    getAttachmentMock.mockResolvedValueOnce({
      id: "att-1",
      url: "https://static.example.test/file.md",
      download_url: SIGNED_URL,
      filename: "file.md",
    });

    const placeholder = { opener: window, location: { href: "about:blank" }, close: vi.fn() };
    const openSpy = vi.spyOn(window, "open").mockReturnValue(placeholder as unknown as Window);

    const { result } = renderHook(() => useDownloadAttachment());

    await act(async () => {
      await result.current("att-1");
    });

    // Placeholder MUST be opened synchronously during the click — otherwise
    // popup blockers won't honour the gesture.
    expect(openSpy).toHaveBeenCalledWith("about:blank", "_blank");
    expect(getAttachmentMock).toHaveBeenCalledWith("att-1");
    // Disown the opener and redirect to the signed URL.
    expect(placeholder.opener).toBeNull();
    expect(placeholder.location.href).toBe(SIGNED_URL);
  });

  it("closes the placeholder and shows a toast when the fetch fails", async () => {
    getAttachmentMock.mockRejectedValueOnce(new Error("boom"));
    const placeholder = { opener: window, location: { href: "about:blank" }, close: vi.fn() };
    vi.spyOn(window, "open").mockReturnValue(placeholder as unknown as Window);

    const { result } = renderHook(() => useDownloadAttachment());

    await act(async () => {
      await result.current("att-1");
    });

    expect(placeholder.close).toHaveBeenCalled();
    await waitFor(() => expect(toast.error).toHaveBeenCalled());
  });
});

describe("useDownloadAttachment (desktop)", () => {
  it("skips the placeholder tab and hands the signed URL to openExternal", async () => {
    (window as unknown as { desktopAPI: { openExternal: () => void } }).desktopAPI = {
      openExternal: vi.fn(),
    };
    getAttachmentMock.mockResolvedValueOnce({
      id: "att-1",
      url: "https://static.example.test/file.md",
      download_url: SIGNED_URL,
      filename: "file.md",
    });
    const openSpy = vi.spyOn(window, "open");

    const { result } = renderHook(() => useDownloadAttachment());

    await act(async () => {
      await result.current("att-1");
    });

    // No placeholder — Electron's setWindowOpenHandler would reject
    // about:blank, so we go straight to the platform's IPC bridge.
    expect(openSpy).not.toHaveBeenCalled();
    expect(openExternal).toHaveBeenCalledWith(SIGNED_URL);
  });
});
