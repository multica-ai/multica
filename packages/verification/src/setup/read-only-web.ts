import { createServer } from "node:http";
import { randomUUID, timingSafeEqual } from "node:crypto";
import { readFile, writeFile, rename } from "node:fs/promises";
import { join } from "node:path";
import { z } from "zod";
import { launch, type RunningProcess } from "./process.js";
import { readPrivateFile } from "../sources/bindings.js";
const sessionSchema = z
  .object({
    environmentId: z.uuid(),
    directory: z.string(),
    commit: z.string(),
    sourceDigest: z.string(),
    apiOrigin: z.url(),
    token: z.string().length(64),
  })
  .passthrough();
async function main() {
  const path = process.argv[2],
    port = z.coerce.number().int().positive().parse(process.argv[3]);
  if (!path) throw new Error("Private session path required");
  const session = sessionSchema.parse(JSON.parse(await readPrivateFile(path)));
  const tools = z
    .object({
      node: z.string(),
      pnpm: z.string(),
      env: z.record(z.string(), z.string().optional()),
    })
    .parse(
      JSON.parse(await readPrivateFile(join(session.directory, "tools.json"))),
    );
  const environment = z
    .object({ checkout: z.string() })
    .parse(
      JSON.parse(
        await readFile(
          join(session.directory, "../", `${session.environmentId}.json`),
          "utf8",
        ),
      ),
    );
  const ownerId = randomUUID();
  let child: RunningProcess | undefined, stopping: Promise<void> | undefined;
  const stop = () =>
    (stopping ??= (async () => {
      await child?.stop();
      await new Promise<void>((done) => server.close(() => done()));
    })());
  const server = createServer((request, response) => {
    const presented = Buffer.from(
        request.headers.authorization?.replace(/^Bearer /, "") ?? "",
      ),
      expected = Buffer.from(session.token);
    if (
      request.headers.origin ||
      !/^(127\.0\.0\.1|localhost):\d+$/.test(request.headers.host ?? "") ||
      presented.length !== expected.length ||
      !timingSafeEqual(presented, expected)
    ) {
      response.writeHead(403).end();
      return;
    }
    response.setHeader("Content-Type", "application/json");
    if (request.method === "GET" && request.url === "/health") {
      response.end(
        JSON.stringify({
          environmentId: session.environmentId,
          ownerId,
          pid: process.pid,
          childPid: child?.pid ?? null,
          commit: session.commit,
          sourceDigest: session.sourceDigest,
          apiOrigin: session.apiOrigin,
        }),
      );
      return;
    }
    if (request.method === "POST" && request.url === "/shutdown") {
      response.end(JSON.stringify({ accepted: true }));
      setTimeout(() => {
        void stop();
      }, 25);
      return;
    }
    response.writeHead(404).end();
  });
  await new Promise<void>((done, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", done);
  });
  try {
    child = await launch(
      tools.node,
      [
        tools.pnpm,
        "--filter",
        "@multica/web",
        "exec",
        "next",
        "dev",
        "--webpack",
        "--hostname",
        "127.0.0.1",
        "--port",
        String(port),
      ],
      {
        cwd: environment.checkout,
        env: {
          ...tools.env,
          FRONTEND_PORT: String(port),
          REMOTE_API_URL: "http://127.0.0.1:1",
          NEXT_PUBLIC_API_URL: "",
          NEXT_PUBLIC_WS_URL: "",
        },
        log: join(session.directory, "web.log"),
      },
    );
    const address = server.address();
    if (!address || typeof address === "string")
      throw new Error("Missing control port");
    const temporary = join(session.directory, `web-${ownerId}.tmp`);
    await writeFile(
      temporary,
      JSON.stringify({
        schemaVersion: 1,
        environmentId: session.environmentId,
        ownerId,
        pid: process.pid,
        port: address.port,
        webPort: port,
        commit: session.commit,
        sourceDigest: session.sourceDigest,
        apiOrigin: session.apiOrigin,
      }),
      { mode: 0o600 },
    );
    await rename(temporary, join(session.directory, "web.json"));
    process.once("SIGTERM", () => {
      void stop();
    });
    process.once("SIGINT", () => {
      void stop();
    });
    await child.exited;
    await stop();
  } catch (error) {
    await stop();
    throw error;
  }
}
void main().catch(() => {
  process.stderr.write(
    "The owned read-only web runtime stopped; inspect its private log.\n",
  );
  process.exitCode = 1;
});
