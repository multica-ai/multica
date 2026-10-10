// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";

const resource = {
  id: "resource", project_id: "project", workspace_id: "workspace",
  resource_type: "local_directory", resource_ref: { local_path: "/repo", execution_mode: "worktree" },
  position: 0, created_at: "2026-10-06T00:00:00Z",
};
const measurement = {
  status: "ready", checked_at: "2026-10-06T00:00:00Z", expires_at: "2026-10-06T00:01:30Z",
  file_count: 0, total_bytes: 0, symlink_count: 0, max_files: 2000, max_bytes: 209715200,
  largest_paths: null,
};
async function read(readiness?: unknown) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({
    resources: [{ ...resource, worktree_readiness: readiness }], total: 1,
  }), { headers: { "Content-Type": "application/json" } })));
  return new ApiClient("https://example.test").listProjectResources("project");
}
afterEach(() => vi.unstubAllGlobals());

describe("resource readiness API compatibility", () => {
  it("preserves an older resource without inventing measurements", async () => {
    const result = await read();
    expect(result.resources[0]?.id).toBe("resource");
    expect(result.resources[0]?.worktree_readiness).toBeUndefined();
  });
  it("distinguishes known zero usage and nullable empty paths from missing usage", async () => {
    expect((await read(measurement)).resources[0]?.worktree_readiness).toMatchObject({ status: "ready", file_count: 0 });
    const checking = (await read({ status: "checking", max_files: 2000, max_bytes: 209715200 })).resources[0]?.worktree_readiness;
    expect(checking?.file_count).toBeUndefined();
    expect(checking?.total_bytes).toBeUndefined();
  });
  it.each([
    { ...measurement, status: "future_status" },
    { ...measurement, file_count: -1 },
    { ...measurement, total_bytes: "100" },
    { ...measurement, checked_at: "yesterday" },
    { status: "ready" },
    null,
  ])("keeps the resource but degrades malformed readiness to unavailable: %j", async (value) => {
    const result = await read(value);
    expect(result.resources).toHaveLength(1);
    expect(result.resources[0]?.worktree_readiness).toEqual({ status: "unavailable" });
  });
  it("preserves stale known measurements and additive reason codes", async () => {
    const readiness = { ...measurement, status: "unavailable", reason_code: "future_reason", file_count: 234 };
    expect((await read(readiness)).resources[0]?.worktree_readiness).toMatchObject({ ...readiness, largest_paths: undefined });
  });
});
