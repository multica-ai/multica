// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

const exportSkillArchive = vi.hoisted(() => vi.fn());
vi.mock("@multica/core/api", () => ({ api: { exportSkillArchive } }));

import { downloadSkillArchive } from "./export-skill";

describe("downloadSkillArchive", () => {
  beforeEach(() => {
    exportSkillArchive.mockReset();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("downloads the archive blob using the server-provided filename", async () => {
    const blob = new Blob(["gzip-bytes"], { type: "application/gzip" });
    exportSkillArchive.mockResolvedValue({
      blob,
      filename: "review-helper.tar.gz",
    });

    const createObjectURL = vi.fn(() => "blob:mock-url");
    const revokeObjectURL = vi.fn();
    vi.stubGlobal("URL", { ...URL, createObjectURL, revokeObjectURL });

    const click = vi.fn();
    const remove = vi.fn();
    const anchor = {
      href: "",
      download: "",
      style: { display: "" },
      click,
      remove,
    } as unknown as HTMLAnchorElement;
    vi.spyOn(document, "createElement").mockReturnValue(anchor as never);
    vi.spyOn(document.body, "appendChild").mockImplementation((node) => node);

    const filename = await downloadSkillArchive("skill-1");

    expect(exportSkillArchive).toHaveBeenCalledTimes(1);
    expect(exportSkillArchive).toHaveBeenCalledWith("skill-1");
    expect(anchor.download).toBe("review-helper.tar.gz");
    expect(anchor.href).toBe("blob:mock-url");
    expect(click).toHaveBeenCalledTimes(1);
    expect(remove).toHaveBeenCalledTimes(1);
    expect(revokeObjectURL).toHaveBeenCalledWith("blob:mock-url");
    expect(filename).toBe("review-helper.tar.gz");
  });
});
