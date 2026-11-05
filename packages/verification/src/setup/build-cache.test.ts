// @vitest-environment node
import { mkdtemp, mkdir, readFile, writeFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { expect, it, vi } from "vitest";
import { BuildCache, verifyBuildFiles } from "./build-cache.js";
it("serializes identical builds, reuses only immutable artifacts and rejects tampered bytes", async () => {
  const root = await mkdtemp(join(tmpdir(), "verification-cache-"));
  try {
    const first = join(root, "first"),
      second = join(root, "second");
    await mkdir(first);
    await mkdir(second);
    const build = vi.fn(async (checkout: string) => {
      await writeFile(join(checkout, "cli"), "built-cli", { mode: 0o755 });
    });
    const cache = new BuildCache(join(root, "cache")),
      inputs = { source: "fixed", node: "v24", dev: true };
    const [a, b] = await Promise.all([
      cache.materialize(first, inputs, ["cli"], () => build(first)),
      cache.materialize(second, inputs, ["cli"], () => build(second)),
    ]);
    expect(build).toHaveBeenCalledTimes(1);
    expect([a.reused, b.reused].sort()).toEqual([false, true]);
    expect(await readFile(join(second, "cli"), "utf8")).toBe("built-cli");
    await verifyBuildFiles(second, b.manifest);
    await writeFile(join(second, "cli"), "modified binary");
    await expect(verifyBuildFiles(second, b.manifest)).rejects.toMatchObject({
      problem: { code: "identityMismatch" },
    });
    await writeFile(join(root, "cache", a.key, "cli"), "changed");
    await expect(
      cache.materialize(second, inputs, ["cli"], () => build(second)),
    ).rejects.toMatchObject({ problem: { code: "identityMismatch" } });
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
