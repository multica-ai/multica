import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, act } from "@testing-library/react";
import type { ReactNode } from "react";
import { I18nProvider } from "@multica/core/i18n/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@multica/core/api";
import enEditor from "../locales/en/editor.json";
import enBilling from "../locales/en/billing.json";

const mockToastError = vi.hoisted(() => vi.fn());
const mockUploadFile = vi.hoisted(() => vi.fn());

vi.mock("sonner", () => ({
  toast: { error: mockToastError },
}));

vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>("@multica/core/api");
  return { ApiError: actual.ApiError, api: { uploadFile: mockUploadFile } };
});

// The storage notice reads the caller's role; outside a workspace route it
// has none and shows the member copy.
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (select: (state: { user: null }) => unknown) => select({ user: null }),
}));

import { useEditorUpload } from "./use-editor-upload";

function wrapper({ children }: { children: ReactNode }) {
  return (
    <QueryClientProvider client={new QueryClient()}>
      <I18nProvider locale="en" resources={{ en: { editor: enEditor, billing: enBilling } }}>
        {children}
      </I18nProvider>
    </QueryClientProvider>
  );
}

function storageRefusal() {
  return new ApiError("full", 403, "Forbidden", {
    code: "storage_limit_reached",
    used_bytes: 1_000_000_000,
    limit_bytes: 1_000_000_000,
  });
}

// MUL-4808 — `uploadWithToast` only ever toasted if its caller passed
// `onError`, and no composer did. So a failed upload silently removed its
// placeholder and the file simply vanished with no explanation. The gate's
// minimum failure fallback ("drop the placeholder, SAY SO, allow submit
// again") depends on this hook supplying that missing toast.
describe("useEditorUpload", () => {
  beforeEach(() => {
    mockToastError.mockReset();
    mockUploadFile.mockReset();
  });

  it("toasts the filename and reason when an upload fails", async () => {
    mockUploadFile.mockRejectedValue(new Error("Network unreachable"));
    const { result } = renderHook(() => useEditorUpload(), { wrapper });

    let returned: unknown;
    await act(async () => {
      returned = await result.current.uploadWithToast(
        new File(["x"], "diagram.png", { type: "image/png" }),
      );
    });

    // Null is what tells the editor extension to drop the placeholder.
    expect(returned).toBeNull();
    expect(mockToastError).toHaveBeenCalledWith(
      "Couldn't upload diagram.png: Network unreachable",
    );
  });

  it("surfaces the size-limit rejection by name too", async () => {
    const { result } = renderHook(() => useEditorUpload(), { wrapper });
    // The 100 MB guard throws before any request goes out.
    const huge = new File(["x"], "huge.mov", { type: "video/quicktime" });
    Object.defineProperty(huge, "size", { value: 200 * 1024 * 1024 });

    await act(async () => {
      await result.current.uploadWithToast(huge);
    });

    expect(mockToastError).toHaveBeenCalledWith(
      "Couldn't upload huge.mov: File exceeds 100 MB limit",
    );
  });

  it("does not toast on a successful upload", async () => {
    mockUploadFile.mockResolvedValue({
      id: "att-1",
      url: "https://cdn.example/att-1.png",
      markdown_url: "https://api.example/api/attachments/att-1/download",
      filename: "ok.png",
    });
    const { result } = renderHook(() => useEditorUpload(), { wrapper });

    await act(async () => {
      await result.current.uploadWithToast(new File(["x"], "ok.png", { type: "image/png" }));
    });

    expect(mockToastError).not.toHaveBeenCalled();
  });

  // MUL-9489: a storage refusal says what happened and what to do, not the
  // server's raw message.
  it("toasts a storage refusal as the storage notice", async () => {
    mockUploadFile.mockRejectedValue(storageRefusal());
    const { result } = renderHook(() => useEditorUpload(), { wrapper });

    await act(async () => {
      await result.current.uploadWithToast(new File(["x"], "design-v3.fig"));
    });

    expect(mockToastError).toHaveBeenCalledWith(
      "design-v3.fig wasn't uploaded",
      expect.objectContaining({
        description:
          "Attachment storage is full. Ask an admin to upgrade the plan, or delete attachments you no longer need.",
      }),
    );
    expect(result.current.storageRefusal).toBeNull();
  });

  it("holds a storage refusal for an inline notice when the host renders one", async () => {
    mockUploadFile.mockRejectedValue(storageRefusal());
    const { result } = renderHook(() => useEditorUpload({ inlineStorageNotice: true }), { wrapper });

    await act(async () => {
      await result.current.uploadWithToast(new File(["x"], "design-v3.fig"));
    });

    expect(mockToastError).not.toHaveBeenCalled();
    expect(result.current.storageRefusal).toEqual({
      filename: "design-v3.fig",
      usedBytes: 1_000_000_000,
      limitBytes: 1_000_000_000,
    });
    act(() => result.current.dismissStorageRefusal());
    expect(result.current.storageRefusal).toBeNull();
  });
});
