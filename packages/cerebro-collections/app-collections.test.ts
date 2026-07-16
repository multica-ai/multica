import { beforeEach, describe, expect, it, vi } from "vitest";

const { cerebroRequest } = vi.hoisted(() => ({ cerebroRequest: vi.fn() }));
vi.mock("@multica/core/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/api")>();
  return { ...actual, api: { ...actual.api, cerebroRequest } };
});

import { createCollectionFolder, fetchAppCollectionFolders, moveCollectionFolder } from "./api";

describe("Apps Collections API", () => {
  beforeEach(() => cerebroRequest.mockReset());

  it("normalizes app folders as the Apps grant surface", async () => {
    cerebroRequest.mockResolvedValue({ folders: [{ id: "folder-1", parent_id: null, name: "Operations" }] });
    await expect(fetchAppCollectionFolders()).resolves.toEqual([{
      surface: "app", id: "folder-1", parent_id: null, name: "Operations", group: "Apps",
    }]);
    expect(cerebroRequest).toHaveBeenCalledWith("/api/cerebro/app-folders");
  });

  it("creates and reparents an app Collection through the app-folder backend", async () => {
    cerebroRequest
      .mockResolvedValueOnce({ id: "folder-2", parent_id: null, name: "Finance" })
      .mockResolvedValueOnce(undefined);
    await expect(createCollectionFolder({ surface: "app", name: "Finance", kind: "app", parentId: null })).resolves.toMatchObject({ surface: "app", group: "Apps" });
    await moveCollectionFolder({ surface: "app", folderId: "folder-2", parentId: "folder-1", name: "Finance" });
    expect(cerebroRequest).toHaveBeenNthCalledWith(1, "/api/cerebro/app-folders", expect.objectContaining({ method: "POST" }));
    expect(cerebroRequest).toHaveBeenNthCalledWith(2, "/api/cerebro/app-folders/folder-2", expect.objectContaining({ method: "PATCH", body: JSON.stringify({ name: "Finance", parent_id: "folder-1" }) }));
  });
});
