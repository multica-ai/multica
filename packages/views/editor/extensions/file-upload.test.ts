import { describe, expect, it } from "vitest";
import { getClipboardFiles } from "./file-upload";

function makeFile(name: string, type: string, body = "x"): File {
  return new File([body], name, { type });
}

describe("getClipboardFiles", () => {
  it("returns files from clipboardData.files", () => {
    const img = makeFile("photo.png", "image/png");
    const clipboard = {
      files: [img] as unknown as FileList,
      items: [] as unknown as DataTransferItemList,
    };
    expect(getClipboardFiles(clipboard)).toEqual([img]);
  });

  it("returns image files exposed only through clipboard items", () => {
    const screenshot = makeFile("screenshot.png", "image/png");
    const clipboard = {
      files: [] as unknown as FileList,
      items: [
        { kind: "file", getAsFile: () => screenshot },
      ] as unknown as DataTransferItemList,
    };
    expect(getClipboardFiles(clipboard)).toEqual([screenshot]);
  });

  it("deduplicates the same file when both files and items expose it", () => {
    const screenshot = makeFile("screenshot.png", "image/png");
    const clipboard = {
      files: [screenshot] as unknown as FileList,
      items: [
        { kind: "file", getAsFile: () => screenshot },
      ] as unknown as DataTransferItemList,
    };
    expect(getClipboardFiles(clipboard)).toEqual([screenshot]);
  });

  it("ignores non-file items (kind: string)", () => {
    const clipboard = {
      files: [] as unknown as FileList,
      items: [
        { kind: "string", getAsFile: () => null },
      ] as unknown as DataTransferItemList,
    };
    expect(getClipboardFiles(clipboard)).toEqual([]);
  });

  it("returns empty array for null clipboard", () => {
    expect(getClipboardFiles(null)).toEqual([]);
    expect(getClipboardFiles(undefined)).toEqual([]);
  });

  it("handles items where getAsFile returns null", () => {
    const clipboard = {
      files: [] as unknown as FileList,
      items: [
        { kind: "file", getAsFile: () => null },
      ] as unknown as DataTransferItemList,
    };
    expect(getClipboardFiles(clipboard)).toEqual([]);
  });
});
