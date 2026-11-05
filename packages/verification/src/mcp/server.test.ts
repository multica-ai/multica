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
async function completed(client: Client, jobId: string) {
  const schema = z.object({
    kind: z.literal("ok"),
    value: z.object({
      state: z.string(),
      output: z
        .object({ value: z.object({ verificationId: z.uuid() }) })
        .optional(),
    }),
  });
  for (let i = 0; i < 100; i++) {
    const result = schema.parse(
      payload(
        await client.callTool({
          name: "setup_status",
          arguments: { target: { kind: "job", jobId } },
        }),
      ),
    );
    if (result.value.state === "completed")
      return result.value.output!.value.verificationId;
    await new Promise((resolve) => setTimeout(resolve, 5));
  }
  throw new Error("Verification creation did not complete");
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
      "setup_inspect",
      "setup_status",
      "setup_cancel",
      "verification_record",
      "setup_prepare",
      "setup_start",
      "setup_stop",
      "setup_data",
      "setup_reset",
      "setup_fixture_control",
      "verification_snapshot",
      "verification_step",
      "verification_events",
      "verification_check",
      "verification_evidence",
      "verification_export",
      "page_windows",
      "page_observe",
      "page_action",
      "page_screenshot",
    ]);
    const missing = await client.callTool({
      name: "setup_status",
      arguments: { target: { kind: "job", jobId: "not-a-job" } },
    });
    expect(missing.isError).toBe(true);
    const notFound = await client.callTool({
      name: "setup_status",
      arguments: { target: { kind: "request", requestId: "missing" } },
    });
    expect(payload(notFound)).toMatchObject({
      kind: "error",
      problem: { code: "notFound" },
    });
    expect(JSON.stringify(notFound)).not.toContain(root);
    const unsupported = await client.callTool({
      name: "verification_record",
      arguments: {
        action: { operation: "finalize", verificationId: "not-supported" },
      },
    });
    expect(unsupported.isError).toBe(true);
  });

  it("runs over real stdio, persists a record and reuses the receipt after reconnect", async () => {
    const root = await directory();
    const first = await stdio(root);
    const args = {
      action: {
        operation: "create",
        title: "Independent MCP",
        kind: "smoke",
        requestId: "stdio-create",
      },
    };
    const receipt = acceptedSchema.parse(
      payload(
        await first.callTool({ name: "verification_record", arguments: args }),
      ),
    );
    const verificationId = await completed(first, receipt.jobId);
    expect(
      payload(
        await first.callTool({
          name: "verification_record",
          arguments: { action: { operation: "read", verificationId } },
        }),
      ),
    ).toMatchObject({
      kind: "ok",
      value: { verificationId, title: "Independent MCP" },
    });
    await first.close();
    const second = await stdio(root);
    expect(
      payload(
        await second.callTool({ name: "verification_record", arguments: args }),
      ),
    ).toMatchObject({
      kind: "completed",
      receiptId: receipt.receiptId,
      value: { verificationId },
    });
    const conflict = await second.callTool({
      name: "verification_record",
      arguments: { action: { ...args.action, title: "Changed" } },
    });
    expect(payload(conflict)).toMatchObject({
      kind: "error",
      problem: { code: "requestConflict" },
    });
  });
});
