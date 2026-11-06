// @vitest-environment node
import { mkdtemp, rm } from "node:fs/promises";
import { createServer } from "node:net";
import { request } from "node:http";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StreamableHTTPClientTransport } from "@modelcontextprotocol/sdk/client/streamableHttp.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";
import { afterEach, describe, expect, it } from "vitest";
import { z } from "zod";
import { createVerificationHost } from "../host/host.js";
import { createVerificationMcpServer } from "./server.js";
import { HealthMonitor } from "../daemon/health.js";
import { serveDaemon } from "../daemon/server.js";

async function freePort(): Promise<number> {
  const server = createServer();
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  if (!address || typeof address === "string") throw new Error("No port");
  await new Promise<void>((resolve) => server.close(() => resolve()));
  return address.port;
}

const cleanups: (() => Promise<unknown>)[] = [];
afterEach(async () => {
  for (const cleanup of cleanups.splice(0).reverse()) await cleanup();
});
const payload = (result: Awaited<ReturnType<Client["callTool"]>>) => {
  const schema = z.object({ result: z.unknown() });
  return schema.parse(result.structuredContent).result;
};
const acceptedSchema = z.object({
  kind: z.literal("accepted"),
  jobId: z.uuid(),
  receiptId: z.uuid(),
});

async function directory() {
  const root = await mkdtemp(join(tmpdir(), "verification-mcp-"));
  cleanups.push(() => rm(root, { recursive: true, force: true }));
  return root;
}
async function snapshotRestore(client: Client, requestId: string) {
  return payload(
    await client.callTool({
      name: "data_restore",
      arguments: {
        environmentId: "00000000-0000-4000-8000-000000000000",
        content: { kind: "workspaceTemplate", issueCount: 1 },
        requestId,
      },
    }),
  );
}

describe("verification MCP transport", () => {
  it("publishes only installed capabilities and maps validation failures without exposing private error text", async () => {
    const root = await directory();
    const host = await createVerificationHost({
      projectRoot: root,
      stateRoot: join(root, "state"),
    });
    cleanups.push(() => host.close());
    const server = createVerificationMcpServer(host);
    const client = new Client({ name: "test", version: "1" });
    const [clientSide, serverSide] = InMemoryTransport.createLinkedPair();
    await server.connect(serverSide);
    await client.connect(clientSide);
    cleanups.push(() => client.close());
    cleanups.push(() => server.close());
    expect((await client.listTools()).tools.map((tool) => tool.name)).toEqual([
      "env_inspect",
      "env_start",
      "env_stop",
      "env_status",
      "env_log_read",
      "job_query",
      "job_cancel",
      "data_snapshot",
      "data_snapshot_read",
      "data_restore",
      "fixture_control",
      "state_read",
      "events_read",
      "page_windows",
      "page_observe",
      "page_read",
      "page_action",
      "page_screenshot",
    ]);
    const missing = await client.callTool({
      name: "job_query",
      arguments: { target: { kind: "job", jobId: "not-a-job" } },
    });
    expect(missing.isError).toBe(true);
    const notFound = await client.callTool({
      name: "job_query",
      arguments: { target: { kind: "request", requestId: "missing" } },
    });
    expect(payload(notFound)).toMatchObject({
      kind: "error",
      problem: { code: "notFound" },
    });
    expect(JSON.stringify(notFound)).not.toContain(root);
    const unsupported = await client.callTool({
      name: "page_action",
      arguments: {
        environmentId: "00000000-0000-4000-8000-000000000000",
        target: { locator: { kind: "xpath", value: "//button" } },
        action: { kind: "click" },
      },
    });
    expect(unsupported.isError).toBe(true);
  });

  it("serves MCP and the console from one loopback daemon without credentials", async () => {
    const root = await directory();
    const host = await createVerificationHost({
      projectRoot: root,
      stateRoot: join(root, "state"),
    });
    cleanups.push(() => host.close());
    const monitor = new HealthMonitor(host, {
      stateRoot: root,
      intervalMs: 60_000,
      maxEnvironments: 2,
    });
    const port = await freePort();
    const daemon = await serveDaemon({
      host,
      monitor,
      port,
      info: {
        pid: process.pid,
        startedAt: new Date().toISOString(),
        commit: "test",
      },
    });
    cleanups.push(() => daemon.close());
    const base = `http://127.0.0.1:${port}`;
    expect(
      (await fetch(`${base}/mcp`, { method: "POST", body: "{}" })).status,
    ).toBe(400);
    const misdirected = await new Promise<number>((resolve, reject) =>
      request(
        {
          host: "127.0.0.1",
          port,
          path: "/console/api/overview",
          headers: { Host: `evil.test:${port}` },
        },
        (response) => {
          response.resume();
          resolve(response.statusCode ?? 0);
        },
      )
        .on("error", reject)
        .end(),
    );
    expect(misdirected).toBe(421);
    const client = new Client({ name: "agent", version: "1" });
    await client.connect(
      new StreamableHTTPClientTransport(new URL(daemon.info.mcpUrl)),
    );
    cleanups.push(() => client.close());
    expect((await client.listTools()).tools).toHaveLength(18);
    const receipt = acceptedSchema.parse(
      await snapshotRestore(client, "http-restore"),
    );
    expect(await snapshotRestore(client, "http-restore")).toMatchObject({
      receiptId: receipt.receiptId,
    });
    const overview = await fetch(`${base}/console/api/overview`);
    expect(await overview.json()).toMatchObject({
      daemon: { mcpUrl: `${base}/mcp` },
      environments: [],
    });
    const forged = await fetch(
      `${base}/console/api/environments/00000000-0000-4000-8000-000000000000/stop`,
      {
        method: "POST",
        headers: { Origin: "http://evil.test" },
      },
    );
    expect(forged.status).toBe(403);
  });
});
