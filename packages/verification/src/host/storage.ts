import { createHash, randomUUID } from "node:crypto";
import {
  mkdir,
  open,
  readFile,
  readdir,
  realpath,
  rename,
  rm,
  lstat,
} from "node:fs/promises";
import { join } from "node:path";
import { z } from "zod";
import type { Json } from "../contracts/index.js";
import { isMissing, problem, VerificationError } from "./problem.js";

export function digest(value: string): string {
  return createHash("sha256").update(value).digest("hex");
}
export function canonicalJson(value: Json): string {
  if (Array.isArray(value)) return `[${value.map(canonicalJson).join(",")}]`;
  if (value !== null && typeof value === "object")
    return `{${Object.keys(value)
      .sort()
      .map((key) => `${JSON.stringify(key)}:${canonicalJson(value[key]!)}`)
      .join(",")}}`;
  return JSON.stringify(value);
}

const bindingSchema = z
  .object({
    schemaVersion: z.literal(1),
    projectRoot: z.string(),
    projectRef: z.string(),
  })
  .strict();
const lockSchema = z
  .object({
    schemaVersion: z.literal(1),
    pid: z.number().int().positive(),
    ownerId: z.uuid(),
  })
  .strict();
type Lock = z.infer<typeof lockSchema>;

async function writeAtomic(
  path: string,
  value: unknown,
  assertOpen: () => void = () => undefined,
): Promise<void> {
  const temporary = `${path}.${randomUUID()}.tmp`;
  try {
    const file = await open(temporary, "wx", 0o600);
    try {
      await file.writeFile(JSON.stringify(value) + "\n");
      await file.sync();
    } finally {
      await file.close();
    }
    assertOpen();
    await rename(temporary, path);
  } finally {
    await rm(temporary, { force: true });
  }
}

export class HostStorage {
  private closed = false;
  private constructor(
    readonly root: string,
    readonly projectRoot: string,
    readonly projectRef: string,
    private readonly owner: Lock,
  ) {}

  static async open(options: {
    stateRoot: string;
    projectRoot: string;
  }): Promise<HostStorage> {
    const projectRoot = await realpath(options.projectRoot);
    await mkdir(options.stateRoot, { recursive: true, mode: 0o700 });
    const root = await realpath(options.stateRoot);
    const lockDir = join(root, "host.lock");
    const owner: Lock = {
      schemaVersion: 1,
      pid: process.pid,
      ownerId: randomUUID(),
    };
    try {
      await mkdir(lockDir, { mode: 0o700 });
    } catch (error) {
      if (
        !(error instanceof Error && "code" in error && error.code === "EEXIST")
      )
        throw error;
      // MCP clients routinely kill their stdio servers, so a lock whose recorded
      // process is gone is taken over; a live owner is never displaced.
      const recoveryDir = join(root, "host-recovery.lock");
      try {
        await mkdir(recoveryDir, { mode: 0o700 });
      } catch {
        throw new VerificationError(
          problem(
            "instanceBusy",
            "Another lock recovery is active or needs inspection",
            "inspect",
          ),
        );
      }
      try {
        const stat = await lstat(lockDir);
        if (!stat.isDirectory() || stat.isSymbolicLink())
          throw new VerificationError(
            problem(
              "identityMismatch",
              "The host lock is not an owned directory",
              "userAction",
            ),
          );
        const previous = lockSchema.parse(
          JSON.parse(await readFile(join(lockDir, "owner.json"), "utf8")),
        );
        let alive = true;
        try {
          process.kill(previous.pid, 0);
        } catch (failure) {
          if (
            failure instanceof Error &&
            "code" in failure &&
            failure.code === "ESRCH"
          )
            alive = false;
        }
        if (alive)
          throw new VerificationError(
            problem(
              "instanceBusy",
              "Another verification host is running with this state directory",
              "inspect",
            ),
          );
        // Rename before removing so recovery cannot delete a new host's lock.
        const abandoned = join(
          root,
          `abandoned-lock-${previous.ownerId}-${randomUUID()}`,
        );
        await rename(lockDir, abandoned);
        await rm(abandoned, { recursive: true, force: true });
        await mkdir(lockDir, { mode: 0o700 });
      } finally {
        await rm(recoveryDir, { recursive: true });
      }
    }
    const storage = new HostStorage(
      root,
      projectRoot,
      digest(projectRoot),
      owner,
    );
    try {
      await writeAtomic(join(lockDir, "owner.json"), owner);
      const binding = await storage.read("project.json", bindingSchema);
      if (
        binding &&
        (binding.projectRoot !== projectRoot ||
          binding.projectRef !== storage.projectRef)
      )
        throw new VerificationError(
          problem(
            "identityMismatch",
            "This state directory belongs to another project",
            "userAction",
          ),
        );
      if (!binding)
        await storage.write("project.json", {
          schemaVersion: 1,
          projectRoot,
          projectRef: storage.projectRef,
        });
      for (const directory of [
        "receipts",
        "jobs",
        "environments",
        "worktrees",
      ])
        await mkdir(join(root, directory), { recursive: true, mode: 0o700 });
      return storage;
    } catch (error) {
      await storage.close();
      throw error;
    }
  }

  async read<T>(path: string, schema: z.ZodType<T>): Promise<T | null> {
    try {
      return schema.parse(
        JSON.parse(await readFile(join(this.root, path), "utf8")),
      );
    } catch (error) {
      if (isMissing(error)) return null;
      throw error;
    }
  }
  assertOpen(): void {
    if (this.closed)
      throw new VerificationError(
        problem(
          "interrupted",
          "The previous host no longer owns this state directory",
          "inspect",
        ),
      );
  }
  async write(path: string, value: unknown): Promise<void> {
    this.assertOpen();
    await writeAtomic(join(this.root, path), value, () => this.assertOpen());
  }
  async list(directory: string): Promise<string[]> {
    return (await readdir(join(this.root, directory)))
      .filter((name) => name.endsWith(".json"))
      .sort();
  }
  async close(): Promise<void> {
    this.closed = true;
    const path = join(this.root, "host.lock");
    const actual = await this.read("host.lock/owner.json", lockSchema);
    if (actual?.ownerId === this.owner.ownerId)
      await rm(path, { recursive: true });
  }
}
