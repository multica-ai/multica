import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, act, waitFor } from "@testing-library/react";
import {
  serializeTrayImages,
  useImageTray,
  type ImageTrayItem,
} from "./use-image-tray";

// jsdom has no object-URL API — stub it so the hook can create blob previews.
beforeEach(() => {
  let n = 0;
  Object.defineProperty(URL, "createObjectURL", {
    configurable: true,
    value: () => `blob:test-${n++}`,
  });
  Object.defineProperty(URL, "revokeObjectURL", {
    configurable: true,
    value: () => {},
  });
});

function imageFile(name: string): File {
  return new File(["x"], name, { type: "image/png" });
}

function item(over: Partial<ImageTrayItem>): ImageTrayItem {
  return {
    localId: "id",
    blobUrl: "blob:x",
    filename: "a.png",
    file: imageFile("a.png"),
    status: "completed",
    uploadedUrl: "https://cdn/a.png",
    ...over,
  };
}

describe("serializeTrayImages", () => {
  it("numbers completed images 1-based in tray order", () => {
    const { markdown, attachmentIds } = serializeTrayImages([
      item({ localId: "1", uploadedUrl: "https://cdn/1.png", attachmentId: "att1" }),
      item({ localId: "2", uploadedUrl: "https://cdn/2.png", attachmentId: "att2" }),
    ]);
    expect(markdown).toBe("![image 1](https://cdn/1.png)\n![image 2](https://cdn/2.png)");
    expect(attachmentIds).toEqual(["att1", "att2"]);
  });

  it("skips uploading/failed images and re-numbers the rest", () => {
    const { markdown, attachmentIds } = serializeTrayImages([
      item({ localId: "1", status: "uploading", uploadedUrl: undefined, attachmentId: undefined }),
      item({ localId: "2", uploadedUrl: "https://cdn/2.png", attachmentId: "att2" }),
      item({ localId: "3", status: "failed", uploadedUrl: undefined, attachmentId: undefined }),
    ]);
    expect(markdown).toBe("![image 1](https://cdn/2.png)");
    expect(attachmentIds).toEqual(["att2"]);
  });

  it("returns empty when nothing is completed", () => {
    expect(serializeTrayImages([])).toEqual({ markdown: "", attachmentIds: [] });
  });
});

describe("useImageTray", () => {
  it("only accepts image files, uploads them, and marks completed", async () => {
    const upload = vi.fn().mockResolvedValue({
      id: "att1",
      link: "https://cdn/a.png",
      filename: "a.png",
    });
    const { result } = renderHook(() => useImageTray(upload));

    act(() => {
      result.current.addFiles([
        imageFile("a.png"),
        new File(["x"], "notes.pdf", { type: "application/pdf" }),
      ]);
    });

    // Non-image filtered out; the image shows immediately as uploading.
    expect(result.current.items).toHaveLength(1);
    expect(result.current.items[0]!.filename).toBe("a.png");

    await waitFor(() => expect(result.current.items[0]!.status).toBe("completed"));
    expect(result.current.items[0]!.uploadedUrl).toBe("https://cdn/a.png");
    expect(result.current.items[0]!.attachmentId).toBe("att1");
    expect(result.current.hasCompleted).toBe(true);
    expect(upload).toHaveBeenCalledTimes(1);
  });

  it("marks an item failed when the upload returns null", async () => {
    const upload = vi.fn().mockResolvedValue(null);
    const { result } = renderHook(() => useImageTray(upload));
    act(() => result.current.addFiles([imageFile("a.png")]));
    await waitFor(() => expect(result.current.items[0]!.status).toBe("failed"));
  });

  it("removes an item by localId", async () => {
    const upload = vi.fn().mockResolvedValue({ id: "1", link: "u", filename: "a.png" });
    const { result } = renderHook(() => useImageTray(upload));
    act(() => result.current.addFiles([imageFile("a.png")]));
    const id = result.current.items[0]!.localId;
    act(() => result.current.remove(id));
    expect(result.current.items).toHaveLength(0);
  });

  it("takeForEmbed returns the file and drops it from the tray", async () => {
    const upload = vi.fn().mockResolvedValue({ id: "1", link: "u", filename: "a.png" });
    const { result } = renderHook(() => useImageTray(upload));
    const f = imageFile("a.png");
    act(() => result.current.addFiles([f]));
    const id = result.current.items[0]!.localId;
    let taken: File | null = null;
    act(() => {
      taken = result.current.takeForEmbed(id);
    });
    expect(taken).toBe(f);
    expect(result.current.items).toHaveLength(0);
  });
});
