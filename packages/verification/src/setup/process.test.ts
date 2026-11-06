// @vitest-environment node
import { mkdtemp, readFile, rm, stat } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { expect, it } from "vitest";
import { run } from "./process.js";

it("bounds private subprocess logs and marks truncation without exposing bearer credentials", async () => {
  const directory = await mkdtemp(join(tmpdir(), "verification-process-"));
  try {
    const log = join(directory, "run.log");
    await run(
      process.execPath,
      [
        "-e",
        'console.log("Authorization: Bearer private-token"); for(let i=0;i<300;i++) console.log("x".repeat(65536));',
      ],
      { cwd: directory, log },
    );
    expect((await stat(log)).size).toBe(16 * 1024 * 1024);
    expect((await readFile(log, "utf8")).slice(0, 100)).toContain(
      "Bearer [REDACTED]",
    );
    expect(await readFile(`${log}.truncated`, "utf8")).toContain(
      "cannot prove error absence",
    );
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

it("does not pass the parent Multica session's identity or model credentials to children", async () => {
  const directory = await mkdtemp(join(tmpdir(), "verification-process-"));
  const saved = { ...process.env };
  try {
    process.env.MULTICA_DAEMON_P2P_SERVER_URL = "http://production.invalid";
    process.env.ANTHROPIC_API_KEY = "test-created-key";
    process.env.DESKTOP_APP_SUFFIX = "installed";
    const log = join(directory, "env.log");
    await run(
      process.execPath,
      [
        "-e",
        "console.log(JSON.stringify(Object.keys(process.env).filter((k) => /^(MULTICA_|ANTHROPIC_|DESKTOP_APP_SUFFIX$|KEEP_)/.test(k))))",
      ],
      { cwd: directory, log, env: { MULTICA_DEV_ISOLATED: "1", KEEP_ME: "1" } },
    );
    expect(JSON.parse(await readFile(log, "utf8"))).toEqual([
      "MULTICA_DEV_ISOLATED",
      "KEEP_ME",
    ]);
  } finally {
    process.env = saved;
    await rm(directory, { recursive: true, force: true });
  }
});
