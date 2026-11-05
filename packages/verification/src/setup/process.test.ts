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
