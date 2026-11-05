// @vitest-environment node
import { execFile } from "node:child_process";
import { mkdtemp, writeFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { promisify } from "node:util";
import { expect, it } from "vitest";
import { sourceIdentity } from "./source-identity.js";
const exec = promisify(execFile);
it("invalidates prepared source identities for tracked and new code but excludes private environment state", async () => {
  const root = await mkdtemp(join(tmpdir(), "verification-source-"));
  const git = (args: string[]) =>
    exec(
      "git",
      [
        "-c",
        "core.hooksPath=/dev/null",
        "-c",
        "user.name=Verification test",
        "-c",
        "user.email=verification@example.invalid",
        ...args,
      ],
      { cwd: root },
    );
  try {
    await git(["init", "-b", "codex/fixture"]);
    await writeFile(join(root, "code.ts"), "original");
    await writeFile(join(root, ".gitignore"), ".env*\n");
    await git(["add", "."]);
    await git(["commit", "--no-gpg-sign", "-m", "test fixture"]);
    const original = await sourceIdentity(root);
    await writeFile(join(root, ".env.worktree"), "PRIVATE=test-created\n");
    expect(await sourceIdentity(root)).toBe(original);
    await writeFile(join(root, "code.ts"), "changed");
    expect(await sourceIdentity(root)).not.toBe(original);
    await writeFile(join(root, "code.ts"), "original");
    await writeFile(join(root, "new.ts"), "new implementation");
    expect(await sourceIdentity(root)).not.toBe(original);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
