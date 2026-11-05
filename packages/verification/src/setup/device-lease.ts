import { randomUUID } from "node:crypto";
import { mkdir, readFile, rename, rm, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { z } from "zod";
import { problem, VerificationError, isMissing } from "../host/problem.js";

const ownerSchema = z
  .object({
    ownerId: z.uuid(),
    hostPid: z.number().int().positive(),
    stateRoot: z.string(),
    environmentId: z.uuid(),
    acquiredAt: z.iso.datetime(),
  })
  .strict();

// Shared across projects and host state directories on this physical device.
export class DeviceLease {
  constructor(
    private readonly root: string,
    private readonly stateRoot: string,
  ) {}
  async inspect() {
    try {
      return ownerSchema.parse(
        JSON.parse(await readFile(join(this.root, "owner.json"), "utf8")),
      );
    } catch (error) {
      if (isMissing(error)) return null;
      throw error;
    }
  }
  async acquire(environmentId: string): Promise<void> {
    await mkdir(join(this.root, ".."), { recursive: true, mode: 0o700 });
    try {
      await mkdir(this.root, { mode: 0o700 });
    } catch (error) {
      if (
        !(error instanceof Error && "code" in error && error.code === "EEXIST")
      )
        throw error;
      const owner = await this.inspect();
      if (
        owner?.stateRoot === this.stateRoot &&
        owner.environmentId === environmentId &&
        owner.hostPid === process.pid
      )
        return;
      throw new VerificationError(
        problem(
          "instanceBusy",
          "The device has a retained verification lease; inspect and stop its owning environment before starting another",
          "inspect",
          owner ? [owner.environmentId] : [],
        ),
      );
    }
    const owner = {
      ownerId: randomUUID(),
      hostPid: process.pid,
      stateRoot: this.stateRoot,
      environmentId,
      acquiredAt: new Date().toISOString(),
    };
    const temporary = join(this.root, `${owner.ownerId}.tmp`);
    await writeFile(temporary, JSON.stringify(ownerSchema.parse(owner)), {
      mode: 0o600,
      flag: "wx",
    });
    await rename(temporary, join(this.root, "owner.json"));
  }
  async releaseConfirmed(environmentId: string): Promise<void> {
    const owner = await this.inspect();
    if (
      !owner ||
      owner.stateRoot !== this.stateRoot ||
      owner.environmentId !== environmentId
    )
      return;
    await rm(this.root, { recursive: true });
  }
}
