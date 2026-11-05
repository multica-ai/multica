import { execFile } from "node:child_process";
import { createHash } from "node:crypto";
import { lstat, readFile, readlink } from "node:fs/promises";
import { join } from "node:path";
import { promisify } from "node:util";
const execute = promisify(execFile);
export async function sourceIdentity(checkout: string): Promise<string> {
  const git = async (args: string[]) =>
    (
      await execute("git", args, {
        cwd: checkout,
        maxBuffer: 32 * 1024 * 1024,
        timeout: 30000,
        env: { ...process.env, GIT_TERMINAL_PROMPT: "0" },
      })
    ).stdout;
  const head = await git(["rev-parse", "HEAD"]);
  const patch = await git([
    "diff",
    "--binary",
    "HEAD",
    "--",
    ".",
    ":(exclude).env*",
    ":(exclude)**/.env*",
  ]);
  const untracked = (
    await git(["ls-files", "--others", "--exclude-standard", "-z"])
  )
    .split("\0")
    .filter(
      (path) =>
        path &&
        !path
          .split(/[\\/]/)
          .some(
            (part) =>
              part.startsWith(".env") || /\.(pem|key|token)$/.test(part),
          ),
    )
    .sort();
  const hash = createHash("sha256").update(head).update(patch);
  for (const path of untracked) {
    const file = join(checkout, path),
      info = await lstat(file);
    hash.update(path).update("\0");
    if (info.isSymbolicLink()) hash.update(await readlink(file));
    else if (info.isFile()) hash.update(await readFile(file));
  }
  return hash.digest("hex");
}
