import { describe, expect, it, vi } from "vitest";
import { runBulkImport, type BulkTask } from "./use-bulk-skill-import";

const skill = (id: string) => ({ id, name: id }) as any;

describe("runBulkImport", () => {
  it("imports payload + url tasks and reports success", async () => {
    const tasks: BulkTask[] = [
      { key: "a", name: "a", kind: "payload", data: { name: "a" } },
      { key: "b", name: "b", kind: "url", url: "u", importName: "b" },
    ];
    const deps = {
      createSkill: vi.fn(async () => skill("a")),
      importSkill: vi.fn(async () => skill("b")),
      onProgress: vi.fn(),
    };
    const results = await runBulkImport(tasks, deps, { current: false });
    expect(results.map((r) => r.status)).toEqual(["success", "success"]);
    expect(deps.createSkill).toHaveBeenCalledOnce();
    expect(deps.importSkill).toHaveBeenCalledOnce();
  });

  it("maps a 409 conflict to skipped, other errors to failed", async () => {
    const tasks: BulkTask[] = [
      { key: "a", name: "a", kind: "payload", data: { name: "a" } },
      { key: "b", name: "b", kind: "payload", data: { name: "b" } },
    ];
    const deps = {
      createSkill: vi
        .fn()
        .mockRejectedValueOnce(new Error("409 already exists"))
        .mockRejectedValueOnce(new Error("network boom")),
      importSkill: vi.fn(),
      onProgress: vi.fn(),
    };
    const results = await runBulkImport(tasks, deps, { current: false });
    expect(results.find((r) => r.key === "a")!.status).toBe("skipped");
    expect(results.find((r) => r.key === "b")!.status).toBe("failed");
  });
});
