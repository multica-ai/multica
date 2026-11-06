// @vitest-environment node
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StdioClientTransport } from "@modelcontextprotocol/sdk/client/stdio.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";
import { afterEach, describe, expect, it } from "vitest";
import { z } from "zod";
import { createVerificationHost } from "../host/host.js";
import { createVerificationMcpServer } from "./server.js";

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
async function stdio(root: string) {
  const client = new Client({ name: "verification-test", version: "1" });
  const entry = fileURLToPath(
    new URL("../../dist/mcp/stdio.js", import.meta.url),
  );
  const transport = new StdioClientTransport({
    command: process.execPath,
    args: [entry, "--project", root, "--state-root", join(root, "state")],
    stderr: "pipe",
  });
  cleanups.push(() => client.close());
  await client.connect(transport);
  return client;
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

  it("runs over real stdio and reuses a job receipt after reconnect", async () => {
    const root = await directory();
    const first = await stdio(root);
    const receipt = acceptedSchema.parse(
      await snapshotRestore(first, "stdio-restore"),
    );
    const job = z
      .object({ kind: z.literal("ok"), value: z.object({ jobId: z.uuid() }) })
      .parse(
        payload(
          await first.callTool({
            name: "job_query",
            arguments: {
              target: { kind: "request", requestId: "stdio-restore" },
            },
          }),
        ),
      );
    expect(job.value.jobId).toBe(receipt.jobId);
    await first.close();
    const second = await stdio(root);
    expect(await snapshotRestore(second, "stdio-restore")).toMatchObject({
      receiptId: receipt.receiptId,
    });
    const conflict = await second.callTool({
      name: "data_restore",
      arguments: {
        environmentId: "00000000-0000-4000-8000-000000000000",
        content: { kind: "workspaceTemplate", issueCount: 2 },
        requestId: "stdio-restore",
      },
    });
    expect(payload(conflict)).toMatchObject({
      kind: "error",
      problem: { code: "requestConflict" },
    });
  });
});
