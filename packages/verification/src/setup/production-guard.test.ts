// @vitest-environment node
import { execFile } from "node:child_process";
import {
  mkdir,
  mkdtemp,
  rm,
  symlink,
  writeFile,
  chmod,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { promisify } from "node:util";
import { afterEach, expect, it } from "vitest";
import { productionRuntimeDigest } from "./production-guard.js";
import { writeDevCli } from "./environments.js";

const exec = promisify(execFile);
const cleanups: string[] = [];
afterEach(async () => {
  for (const path of cleanups.splice(0))
    await rm(path, { recursive: true, force: true });
});
async function directory() {
  const path = await mkdtemp(join(tmpdir(), "verification-guard-"));
  cleanups.push(path);
  return path;
}

it("changes when installed runtime files appear, change or relink", async () => {
  const home = await directory();
  const empty = await productionRuntimeDigest(home);
  await mkdir(join(home, ".multica/desktop-runtime"), { recursive: true });
  await writeFile(join(home, ".multica/desktop-runtime/launch.json"), "{}");
  const launched = await productionRuntimeDigest(home);
  expect(launched).not.toBe(empty);
  expect(await productionRuntimeDigest(home)).toBe(launched);
  await writeFile(
    join(home, ".multica/desktop-runtime/launch.json"),
    '{"ownerId":"other"}',
  );
  const changed = await productionRuntimeDigest(home);
  expect(changed).not.toBe(launched);
  await symlink("/installed/cli", join(home, ".multica/desktop-runtime/cli"));
  const linked = await productionRuntimeDigest(home);
  await rm(join(home, ".multica/desktop-runtime/cli"));
  await symlink("/worktree/cli", join(home, ".multica/desktop-runtime/cli"));
  expect(await productionRuntimeDigest(home)).not.toBe(linked);
});

it("writes a multica-dev command bound to the runtime home and health port", async () => {
  const root = await directory();
  const cli = join(root, "it's cli");
  await writeFile(
    cli,
    '#!/bin/sh\necho "$HOME|$MULTICA_DEV_ISOLATED|$MULTICA_DEV_HEALTH_PORT|$*"\n',
  );
  await chmod(cli, 0o700);
  const command = await writeDevCli(
    root,
    join(root, "runtime home"),
    40123,
    cli,
  );
  expect(command).toBe(join(root, "bin/multica-dev"));
  const { stdout } = await exec(command, ["daemon", "status"], {
    env: { PATH: process.env.PATH },
  });
  expect(stdout.trim()).toBe(
    `${join(root, "runtime home")}|1|40123|--profile desktop-runtime daemon status`,
  );
});
