import { describe, expect, it, vi } from "vitest";

vi.mock("expo", () => ({ requireOptionalNativeModule: () => null }));

const { parseStoredSnapshot, snapshotStoreKey } = await import("./snapshot-store");

describe("stored conversation snapshots", () => {
  it("ignore anything that is not a snapshot", () => {
    expect(parseStoredSnapshot(null)).toBeNull();
    expect(parseStoredSnapshot("not json")).toBeNull();
    expect(parseStoredSnapshot(JSON.stringify({ version: 1 }))).toBeNull();
  });

  it("are separate per account, workspace, device and conversation", () => {
    const scope = { userId: "u", workspaceId: "w", deviceId: "d" };
    const keys = [snapshotStoreKey(scope, "c"), snapshotStoreKey({ ...scope, userId: "x" }, "c"), snapshotStoreKey({ ...scope, workspaceId: "x" }, "c"), snapshotStoreKey({ ...scope, deviceId: "x" }, "c"), snapshotStoreKey(scope, "x")];
    expect(new Set(keys).size).toBe(keys.length);
  });
});
