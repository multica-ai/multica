// @vitest-environment node
import { spawn } from "node:child_process";
import { randomUUID } from "node:crypto";
import { mkdtemp, mkdir, writeFile, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { expect, it } from "vitest";
import { z } from "zod";
it("retains authenticated ownership and stops its web child after the original client detaches", async () => {
  const root = await mkdtemp(join(tmpdir(), "verification-web-owner-")),
    id = randomUUID(),
    directory = join(root, id),
    token = "a".repeat(64);
  await mkdir(directory);
  const script = join(root, "fake-next.mjs");
  await writeFile(
    script,
    'import {createServer} from "node:http"; const server=createServer((_q,r)=>r.end("test app"));server.listen(Number(process.env.FRONTEND_PORT),"127.0.0.1");',
  );
  const session = join(directory, "session.json");
  await writeFile(
    session,
    JSON.stringify({
      environmentId: id,
      directory,
      commit: "0".repeat(40),
      sourceDigest: "digest",
      apiOrigin: "http://test.invalid",
      token,
    }),
    { mode: 0o600 },
  );
  await writeFile(join(root, `${id}.json`), JSON.stringify({ checkout: root }));
  await writeFile(
    join(directory, "tools.json"),
    JSON.stringify({ node: process.execPath, pnpm: script, env: {} }),
    { mode: 0o600 },
  );
  const { createServer } = await import("node:net");
  const temporary = createServer();
  await new Promise<void>((done) => temporary.listen(0, "127.0.0.1", done));
  const address = temporary.address();
  if (!address || typeof address === "string") throw new Error("missing port");
  const port = address.port;
  await new Promise<void>((done) => temporary.close(() => done()));
  const child = spawn(
    process.execPath,
    [
      fileURLToPath(
        new URL("../../dist/setup/read-only-web.js", import.meta.url),
      ),
      session,
      String(port),
    ],
    { stdio: "ignore" },
  );
  const exited = new Promise<void>((done) => child.once("exit", () => done()));
  try {
    let text = "";
    for (let index = 0; index < 100; index++) {
      text = await readFile(join(directory, "web.json"), "utf8").catch(
        () => "",
      );
      if (text) break;
      await new Promise((done) => setTimeout(done, 20));
    }
    const descriptor = z
      .object({ port: z.number(), pid: z.number(), ownerId: z.string() })
      .parse(JSON.parse(text));
    const base = `http://127.0.0.1:${descriptor.port}`;
    expect((await fetch(`${base}/health`)).status).toBe(403);
    const headers = { Authorization: `Bearer ${token}` };
    const health = await (await fetch(`${base}/health`, { headers })).json();
    expect(health).toMatchObject({
      environmentId: id,
      pid: descriptor.pid,
      ownerId: descriptor.ownerId,
    });
    let ready = false;
    for (let index = 0; index < 100; index++) {
      ready = await fetch(`http://127.0.0.1:${port}`).then(
        (response) => response.ok,
        () => false,
      );
      if (ready) break;
      await new Promise((done) => setTimeout(done, 20));
    }
    expect(ready).toBe(true);
    expect(
      (await fetch(`${base}/shutdown`, { method: "POST", headers })).ok,
    ).toBe(true);
    await exited;
    await expect(fetch(`http://127.0.0.1:${port}`)).rejects.toThrow();
  } finally {
    child.kill("SIGTERM");
    await exited;
    await rm(root, { recursive: true, force: true });
  }
});
