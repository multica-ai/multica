import { createHash, randomUUID } from "node:crypto";
import {
  cp,
  lstat,
  mkdir,
  readFile,
  readdir,
  rename,
  rm,
  writeFile,
} from "node:fs/promises";
import { dirname, join, relative, isAbsolute } from "node:path";
import { z } from "zod";
import { canonicalJson, digest } from "../host/storage.js";
import type { Json } from "../contracts/index.js";
import { isMissing, problem, VerificationError } from "../host/problem.js";

const entrySchema = z
  .object({
    path: z.string(),
    sha256: z.string().regex(/^[a-f0-9]{64}$/),
    bytes: z.number().int().nonnegative(),
  })
  .strict();
export const manifestSchema = z
  .object({
    schemaVersion: z.literal(1),
    key: z.string(),
    inputs: z.record(z.string(), z.union([z.string(), z.boolean()])),
    files: z.array(entrySchema).min(1),
  })
  .strict();
export type BuildInputs = z.infer<typeof manifestSchema>["inputs"];
const hash = (bytes: Uint8Array) =>
  createHash("sha256").update(bytes).digest("hex");
function ownedPath(root: string, path: string) {
  const resolved = join(root, path),
    rel = relative(root, resolved);
  if (isAbsolute(path) || rel.startsWith("..") || isAbsolute(rel))
    throw new Error("Invalid cache path");
  return resolved;
}
async function inventory(
  root: string,
  paths: string[],
): Promise<z.infer<typeof entrySchema>[]> {
  const result: z.infer<typeof entrySchema>[] = [];
  for (const path of paths) {
    const file = ownedPath(root, path),
      info = await lstat(file);
    if (info.isSymbolicLink())
      throw new Error("Build artifacts cannot be symbolic links");
    if (info.isDirectory())
      result.push(
        ...(await inventory(
          root,
          (await readdir(file)).sort().map((name) => join(path, name)),
        )),
      );
    else if (info.isFile()) {
      const bytes = await readFile(file);
      result.push({ path, bytes: bytes.length, sha256: hash(bytes) });
    } else throw new Error("Unsupported build artifact");
  }
  return result;
}
export async function verifyBuildFiles(
  checkout: string,
  manifest: z.infer<typeof manifestSchema>,
) {
  for (const entry of manifest.files) {
    const file = ownedPath(checkout, entry.path),
      info = await lstat(file);
    if (!info.isFile() || info.isSymbolicLink())
      throw new VerificationError(
        problem(
          "identityMismatch",
          "A materialized build artifact changed type",
        ),
      );
    const bytes = await readFile(file);
    if (bytes.length !== entry.bytes || hash(bytes) !== entry.sha256)
      throw new VerificationError(
        problem(
          "identityMismatch",
          "A materialized build artifact differs from the prepared manifest",
        ),
      );
  }
}
export class BuildCache {
  constructor(private readonly root: string) {}
  async materialize(
    checkout: string,
    inputs: BuildInputs,
    paths: string[],
    build: () => Promise<void>,
    signal?: AbortSignal,
  ) {
    const key = digest(canonicalJson(inputs as Json)),
      directory = join(this.root, key),
      lock = `${directory}.lock`;
    await mkdir(this.root, { recursive: true, mode: 0o700 });
    const deadline = Date.now() + 120000;
    for (;;) {
      signal?.throwIfAborted();
      try {
        await mkdir(lock, { mode: 0o700 });
        break;
      } catch (error) {
        if (
          !(
            error instanceof Error &&
            "code" in error &&
            error.code === "EEXIST"
          )
        )
          throw error;
        if (Date.now() >= deadline)
          throw new VerificationError(
            problem(
              "instanceBusy",
              "This build key is locked; inspect the retained build before retrying",
              "inspect",
            ),
          );
        await new Promise((done) => setTimeout(done, 100));
      }
    }
    try {
      await writeFile(
        join(lock, "owner.json"),
        JSON.stringify({
          pid: process.pid,
          key,
          createdAt: new Date().toISOString(),
        }),
        { mode: 0o600 },
      );
      let manifest: z.infer<typeof manifestSchema> | undefined;
      try {
        manifest = manifestSchema.parse(
          JSON.parse(await readFile(join(directory, "manifest.json"), "utf8")),
        );
        if (
          manifest.key !== key ||
          canonicalJson(manifest.inputs) !== canonicalJson(inputs)
        )
          throw new Error("Wrong cache inputs");
        for (const entry of manifest.files) {
          const data = await readFile(ownedPath(directory, entry.path));
          if (data.length !== entry.bytes || hash(data) !== entry.sha256)
            throw new Error("Build cache checksum mismatch");
        }
      } catch (error) {
        if (!isMissing(error)) {
          throw new VerificationError(
            problem(
              "identityMismatch",
              "The cached build manifest or artifact changed; inspect the cache entry",
              "inspect",
            ),
          );
        }
        const present = await lstat(directory).then(
          () => true,
          () => false,
        );
        if (present)
          throw new VerificationError(
            problem(
              "identityMismatch",
              "The cached build is missing its manifest or an artifact; inspect the retained entry",
              "inspect",
            ),
          );
        manifest = undefined;
      }
      if (manifest) {
        for (const entry of manifest.files) {
          const target = ownedPath(checkout, entry.path);
          await mkdir(dirname(target), { recursive: true });
          await cp(ownedPath(directory, entry.path), target);
        }
        return { key, reused: true, manifest };
      }
      await build();
      const files = await inventory(checkout, paths),
        temporary = join(this.root, `.${key}-${randomUUID()}`);
      manifest = manifestSchema.parse({ schemaVersion: 1, key, inputs, files });
      await mkdir(temporary, { mode: 0o700 });
      try {
        for (const entry of files) {
          const target = ownedPath(temporary, entry.path);
          await mkdir(dirname(target), { recursive: true });
          await cp(ownedPath(checkout, entry.path), target);
        }
        await writeFile(
          join(temporary, "manifest.json"),
          JSON.stringify(manifest),
          { mode: 0o600 },
        );
        await rename(temporary, directory);
      } finally {
        await rm(temporary, { recursive: true, force: true });
      }
      return { key, reused: false, manifest };
    } finally {
      await rm(lock, { recursive: true });
    }
  }
}
