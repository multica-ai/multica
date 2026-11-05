// @vitest-environment node
import { mkdtemp, rm } from "node:fs/promises";
import { randomUUID } from "node:crypto";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { expect, it } from "vitest";
import { DeviceLease } from "./device-lease.js";
it("excludes other projects on the same device and retains the lease until owned cleanup is confirmed", async () => {
  const root = await mkdtemp(join(tmpdir(), "verification-device-"));
  try {
    const first = new DeviceLease(
      join(root, "device.lock"),
      join(root, "project-a"),
    );
    const other = new DeviceLease(
      join(root, "device.lock"),
      join(root, "project-b"),
    );
    const id = randomUUID();
    await first.acquire(id);
    await expect(other.acquire(randomUUID())).rejects.toMatchObject({
      problem: { code: "instanceBusy" },
    });
    await other.releaseConfirmed(id);
    expect((await first.inspect())?.environmentId).toBe(id);
    await first.releaseConfirmed(id);
    await other.acquire(randomUUID());
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
