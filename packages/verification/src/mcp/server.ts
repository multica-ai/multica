import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";
import {
  readError,
  readOk,
  requestIdSchema,
  resourceIdSchema,
  toJson,
} from "../contracts/index.js";
import type { VerificationHost } from "../host/host.js";
import { toProblem } from "../host/problem.js";
import { prepareInput } from "../setup/environments.js";
import { restoreInput, snapshotInput } from "../host/operations.js";
import { eventsReadInput, stateReadInput } from "../state/service.js";
import {
  pageActionSchema,
  pointSchema,
  targetSchema,
} from "../page/service.js";

async function respond(action: () => Promise<unknown>) {
  try {
    const result = await action();
    return {
      content: [{ type: "text" as const, text: JSON.stringify(result) }],
      structuredContent: { result },
    };
  } catch (error) {
    const result = readError(toProblem(error));
    return {
      isError: true,
      content: [{ type: "text" as const, text: JSON.stringify(result) }],
      structuredContent: { result },
    };
  }
}
const read = (action: () => Promise<unknown>) =>
  respond(async () => readOk(await action()));

const readOnly = {
  readOnlyHint: true,
  destructiveHint: false,
  idempotentHint: true,
  openWorldHint: false,
};
const command = {
  readOnlyHint: false,
  destructiveHint: false,
  idempotentHint: true,
  openWorldHint: false,
};
const pageAction = { ...command, idempotentHint: false };

export function createVerificationMcpServer(host: VerificationHost): McpServer {
  const server = new McpServer(
    { name: "multica_verification", version: "0.0.0" },
    {
      instructions:
        "Operate dev-only Multica environments and read their data. Every tool is independent. Asynchronous tools return a job; read it with job_query. Tools report what they did and observed; deciding whether behaviour is correct is the caller's job.",
    },
  );

  server.registerTool(
    "env_inspect",
    {
      description:
        "Inspect declared tools, disk, GUI session, build dependencies, managed database, ports and Computer Use prerequisites on this device.",
      inputSchema: { mode: z.enum(["renderer", "desktop"]).default("desktop") },
      annotations: readOnly,
    },
    ({ mode }, extra) => read(() => host.setup.inspect(mode, extra.signal)),
  );
  server.registerTool(
    "env_start",
    {
      description:
        "Start: prepare an owned worktree at a Git commit, build it, and launch its backend and dev Electron (or a read-only web page for liveReadOnly). Returns a job; its resourceRefs carry the environmentId as soon as preparation begins. The job waits in awaitingUser until the application is logged in. The runtime uses its own HOME and daemon health port, so it can run beside an installed Multica.",
      inputSchema: { ...prepareInput.shape, requestId: requestIdSchema },
      annotations: command,
    },
    ({ requestId, ...input }) =>
      respond(() => host.jobs.submit("env.start", toJson(input), requestId)),
  );
  server.registerTool(
    "env_stop",
    {
      description:
        "Start stopping this environment's own processes and daemon. Data stays on disk. Returns a job.",
      inputSchema: {
        environmentId: resourceIdSchema,
        requestId: requestIdSchema,
      },
      annotations: command,
    },
    ({ environmentId, requestId }) =>
      respond(() => host.jobs.submit("env.stop", { environmentId }, requestId)),
  );
  server.registerTool(
    "env_status",
    {
      description:
        "Read an environment's recorded state and the currently observed application, daemon and backend identities.",
      inputSchema: { environmentId: resourceIdSchema },
      annotations: readOnly,
    },
    ({ environmentId }) => read(() => host.environments.status(environmentId)),
  );
  server.registerTool(
    "env_log_read",
    {
      description:
        "Read a page of an environment's prepare, backend, desktop or stop log. Credential-like text is masked.",
      inputSchema: {
        environmentId: resourceIdSchema,
        name: z.enum(["prepare", "backend", "desktop", "stop"]),
        offset: z.number().int().nonnegative().default(0),
        limit: z.number().int().min(1).max(32000).default(8000),
      },
      annotations: readOnly,
    },
    ({ environmentId, name, offset, limit }) =>
      read(() => host.environments.readLog(environmentId, name, offset, limit)),
  );
  server.registerTool(
    "job_query",
    {
      description:
        "Read an asynchronous job by jobId, or by the requestId it was started with.",
      inputSchema: {
        target: z.discriminatedUnion("kind", [
          z
            .object({ kind: z.literal("job"), jobId: resourceIdSchema })
            .strict(),
          z
            .object({ kind: z.literal("request"), requestId: requestIdSchema })
            .strict(),
        ]),
      },
      annotations: readOnly,
    },
    ({ target }) =>
      read(async () =>
        target.kind === "job"
          ? host.jobs.get(target.jobId)
          : (await host.jobs.byRequest(target.requestId)).job,
      ),
  );
  server.registerTool(
    "job_cancel",
    {
      description:
        "Request cooperative cancellation of a job. Read the job afterwards for its actual terminal state.",
      inputSchema: { jobId: resourceIdSchema, requestId: requestIdSchema },
      annotations: command,
    },
    ({ jobId, requestId }) => respond(() => host.jobs.cancel(jobId, requestId)),
  );

  server.registerTool(
    "data_snapshot",
    {
      description:
        "Start capturing selected conversations, projects or issues from a host-bound source. Credentials are never tool inputs. Returns a job whose output describes the snapshot.",
      inputSchema: { ...snapshotInput.shape, requestId: requestIdSchema },
      annotations: command,
    },
    ({ requestId, ...input }) =>
      respond(() =>
        host.jobs.submit("data.snapshot", toJson(input), requestId),
      ),
  );
  server.registerTool(
    "data_snapshot_read",
    {
      description:
        "Read a page of a stored snapshot's entities, relationships or attachments.",
      inputSchema: {
        dataSnapshotId: resourceIdSchema,
        section: z
          .enum(["entities", "relationships", "attachments"])
          .default("entities"),
        cursor: z.number().int().nonnegative().default(0),
        limit: z.number().int().min(1).max(100).default(50),
      },
      annotations: readOnly,
    },
    ({ dataSnapshotId, section, cursor, limit }) =>
      read(() =>
        host.data.describeSnapshot(dataSnapshotId, section, cursor, limit),
      ),
  );
  server.registerTool(
    "data_restore",
    {
      description:
        "Start importing a fresh copy of a snapshot or template into an environment. Each new requestId creates another copy with new ids; the job output maps original ids to copied ids.",
      inputSchema: { ...restoreInput.shape, requestId: requestIdSchema },
      annotations: command,
    },
    ({ requestId, ...input }) =>
      respond(() => host.jobs.submit("data.restore", toJson(input), requestId)),
  );
  server.registerTool(
    "fixture_control",
    {
      description:
        "Inspect, or release/chunk/fail the pending request of, the fake provider behind a conversation in an owned test environment. Never calls a real model.",
      inputSchema: {
        environmentId: resourceIdSchema,
        conversationId: z.string().min(1),
        action: z.enum(["inspect", "release", "chunk", "fail"]),
        text: z.string().max(100000).default(""),
        expectedRequestId: z.string().min(1).optional(),
      },
      annotations: command,
    },
    (input) =>
      read(() =>
        host.data.control(
          input.environmentId,
          input.conversationId,
          input.action,
          input.text,
          input.expectedRequestId,
        ),
      ),
  );

  server.registerTool(
    "state_read",
    {
      description:
        "Read the current state reported by one source: daemon conversation snapshot (beforeTurnIndex pages older turns), Electron main, renderer conversation model, backend issue/project, or read-only page runtime.",
      inputSchema: stateReadInput.shape,
      annotations: { ...readOnly, idempotentHint: false },
    },
    (input) => read(() => host.state.read(input)),
  );
  server.registerTool(
    "events_read",
    {
      description:
        "Read events after a cursor from the daemon's durable conversation log, the Electron main diagnostic buffer, or the read-only page runtime. Returns the source's generation, cursor and retention bounds unchanged.",
      inputSchema: eventsReadInput.shape,
      annotations: { ...readOnly, idempotentHint: false },
    },
    (input) => read(() => host.state.events(input)),
  );

  server.registerTool(
    "page_windows",
    {
      description: "List the environment's application windows.",
      inputSchema: { environmentId: resourceIdSchema },
      annotations: readOnly,
    },
    ({ environmentId }) => read(() => host.pages.windows(environmentId)),
  );
  server.registerTool(
    "page_observe",
    {
      description:
        "List visible interactive elements of a window (the first window when windowId is omitted). Password values are masked.",
      inputSchema: {
        environmentId: resourceIdSchema,
        windowId: z.string().min(1).max(200).optional(),
        frameUrl: z.string().max(2000).optional(),
        offset: z.number().int().min(0).max(10000).default(0),
        limit: z.number().int().min(1).max(100).default(50),
      },
      annotations: { ...readOnly, idempotentHint: false },
    },
    (input) =>
      read(() =>
        host.pages.observe(
          input.environmentId,
          input.windowId,
          input.frameUrl,
          input.offset,
          input.limit,
        ),
      ),
  );
  server.registerTool(
    "page_read",
    {
      description:
        "Read the elements matching a locator: count, and per match visibility, enabled state, text, value and bounds.",
      inputSchema: {
        environmentId: resourceIdSchema,
        target: targetSchema,
        limit: z.number().int().min(1).max(100).default(20),
      },
      annotations: { ...readOnly, idempotentHint: false },
    },
    ({ environmentId, target, limit }) =>
      read(() => host.pages.read(environmentId, target, limit)),
  );
  server.registerTool(
    "page_action",
    {
      description:
        "Perform one click, fill, select, pressKey or scroll on a locator match (pass index when it matches several) or on window coordinates. Reports only that the input was delivered.",
      inputSchema: {
        environmentId: resourceIdSchema,
        target: z.union([targetSchema, pointSchema]),
        action: pageActionSchema,
      },
      annotations: pageAction,
    },
    ({ environmentId, target, action }) =>
      read(() => host.pages.perform(environmentId, target, action)),
  );
  server.registerTool(
    "page_screenshot",
    {
      description: "Capture a window and return the image.",
      inputSchema: {
        environmentId: resourceIdSchema,
        windowId: z.string().min(1).max(200).optional(),
        fullPage: z.boolean().default(false),
      },
      annotations: { ...readOnly, idempotentHint: false },
    },
    async ({ environmentId, windowId, fullPage }) => {
      try {
        const shot = await host.pages.screenshot(
          environmentId,
          windowId,
          fullPage,
        );
        const metadata = {
          colorScheme: shot.colorScheme,
          capturedAt: shot.capturedAt,
        };
        return {
          content: [
            {
              type: "image" as const,
              data: shot.png.toString("base64"),
              mimeType: "image/png",
            },
            { type: "text" as const, text: JSON.stringify(metadata) },
          ],
          structuredContent: { result: readOk(metadata) },
        };
      } catch (error) {
        return respond(async () => {
          throw error;
        });
      }
    },
  );
  return server;
}
