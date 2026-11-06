import { createHash } from "node:crypto";
import { lstat, readFile, readlink } from "node:fs/promises";
import { join } from "node:path";

/** Files the installed Multica owns under the real home; a verification runtime must never change them. */
export const productionRuntimeFiles = [
  ".multica/desktop-runtime/launch.json",
  ".multica/desktop-runtime/daemon.pid",
  ".multica/desktop-runtime/cli",
  ".multica/profiles/desktop-runtime/config.json",
  ".codex/config.toml",
];

/** Digest of the installed runtime's identity files, following no symlinks. */
export async function productionRuntimeDigest(home: string): Promise<string> {
  const hash = createHash("sha256");
  for (const relative of productionRuntimeFiles) {
    const path = join(home, relative);
    hash.update(`${relative}\0`);
    try {
      const info = await lstat(path);
      hash.update(
        info.isSymbolicLink()
          ? `link:${await readlink(path)}`
          : `file:${createHash("sha256")
              .update(await readFile(path))
              .digest("hex")}`,
      );
    } catch (error) {
      if (
        !(error instanceof Error && "code" in error && error.code === "ENOENT")
      )
        throw error;
      hash.update("absent");
    }
    hash.update("\0");
  }
  return hash.digest("hex");
}
