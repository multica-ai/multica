import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";
import {
  createVerificationInputSchema,
  readError,
  readOk,
  requestIdSchema,
  resourceIdSchema,
  toJson,
} from "../contracts/index.js";
import type { VerificationHost } from "../host/host.js";
import { toProblem } from "../host/problem.js";
import { prepareInput } from "../setup/environments.js";
import { checkInput } from "../checks/service.js";
import { pageActionSchema } from "../page/service.js";
import { summarizeDataset } from "../host/operations.js";

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

export function createVerificationMcpServer(host: VerificationHost): McpServer {
  const server = new McpServer(
    { name: "multica_verification", version: "0.0.0" },
    {
      instructions:
        "Prepare and operate a dev-only Multica verification environment. Read the tool schemas: applications use their own authentication; host-bound source references grant the available data scope. Follow accepted commands with setup_status. Never replay unknown outcomes. Observe before page actions, bind environments to a verification, and end collection before interval assertions. Read evidence before finalizing reports. Production clients have no added diagnostic entry points.",
    },
  );
  server.registerTool(
    "setup_inspect",
    {
      description:
        "Read toolchain requirements from the bound checkout and probe Node, pnpm, Go and Git. Does not install dependencies or prove application readiness.",
      inputSchema: { mode: z.enum(["renderer", "desktop"]).default("desktop") },
      annotations: {
        readOnlyHint: true,
        destructiveHint: false,
        idempotentHint: true,
        openWorldHint: false,
      },
    },
    ({ mode }, extra) =>
      respond(async () => readOk(await host.setup.inspect(mode, extra.signal))),
  );

  server.registerTool(
    "setup_status",
    {
      description:
        "Read a persisted job or receipt in this project's state directory. Use the original requestId if a command response was lost.",
      inputSchema: {
        target: z.discriminatedUnion("kind", [
          z
            .object({
              kind: z.literal("environmentLog"),
              environmentId: resourceIdSchema,
              name: z.enum(["prepare", "backend", "desktop", "stop"]),
              offset: z.number().int().nonnegative().default(0),
              limit: z.number().int().min(1).max(32000).default(8000),
            })
            .strict(),
          z
            .object({ kind: z.literal("job"), jobId: resourceIdSchema })
            .strict(),
          z
            .object({ kind: z.literal("receipt"), receiptId: resourceIdSchema })
            .strict(),
          z
            .object({ kind: z.literal("request"), requestId: requestIdSchema })
            .strict(),
          z
            .object({
              kind: z.literal("environment"),
              environmentId: resourceIdSchema,
            })
            .strict(),
        ]),
      },
      annotations: {
        readOnlyHint: true,
        destructiveHint: false,
        idempotentHint: true,
        openWorldHint: false,
      },
    },
    ({ target }) =>
      respond(async () =>
        readOk(
          target.kind === "environmentLog"
            ? await host.environments.readLog(
                target.environmentId,
                target.name,
                target.offset,
                target.limit,
              )
            : target.kind === "job"
              ? await host.jobs.get(target.jobId)
              : target.kind === "receipt"
                ? await host.jobs.getReceipt(target.receiptId)
                : target.kind === "environment"
                  ? await host.environments.get(target.environmentId)
                  : await host.jobs.byRequest(target.requestId),
        ),
      ),
  );

  server.registerTool(
    "setup_cancel",
    {
      description:
        "Request cooperative cancellation of one job owned by this host. Returns a receipt; query the target job to confirm its actual terminal state. Does not undo existing resources.",
      inputSchema: { jobId: resourceIdSchema, requestId: requestIdSchema },
      annotations: {
        readOnlyHint: false,
        destructiveHint: false,
        idempotentHint: true,
        openWorldHint: false,
      },
    },
    ({ jobId, requestId }) => respond(() => host.jobs.cancel(jobId, requestId)),
  );

  server.registerTool(
    "verification_record",
    {
      description:
        "Create/read a verification, bind an owned environment's comparison role, or finalize an immutable report revision. Passed conclusions require actual checks and required evidence. Waivers require explicit host user context.",
      inputSchema: {
        action: z.discriminatedUnion("operation", [
          createVerificationInputSchema.extend({
            operation: z.literal("create"),
            requestId: requestIdSchema,
          }),
          z
            .object({
              operation: z.literal("read"),
              verificationId: resourceIdSchema,
            })
            .strict(),
          z
            .object({
              operation: z.literal("bindEnvironment"),
              verificationId: resourceIdSchema,
              environmentId: resourceIdSchema,
              role: z.enum(["baseline", "candidate", "smoke"]),
              requestId: requestIdSchema,
            })
            .strict(),
          z
            .object({
              operation: z.literal("finalize"),
              verificationId: resourceIdSchema,
              conclusion: z.enum([
                "passed",
                "failed",
                "inconclusive",
                "waived",
              ]),
              explanation: z.string().max(20000),
              uncovered: z.array(z.string()).max(100),
              waiverRef: z.string().optional(),
              requestId: requestIdSchema,
            })
            .strict(),
        ]),
      },
      annotations: {
        readOnlyHint: false,
        destructiveHint: false,
        idempotentHint: true,
        openWorldHint: false,
      },
    },
    ({ action }) =>
      respond(async () => {
        if (action.operation === "create")
          return host.records.create(
            {
              title: action.title,
              kind: action.kind,
              requireScreenshots: action.requireScreenshots,
            },
            action.requestId,
          );
        if (action.operation === "read")
          return readOk(await host.records.read(action.verificationId));
        const { requestId, operation, ...input } = action;
        return host.jobs.submit(
          `evidence.${operation}`,
          toJson(input),
          requestId,
        );
      }),
  );

  const submit = (
    operation: string,
    value: { requestId: string },
    input: unknown,
  ) => host.jobs.submit(operation, toJson(input), value.requestId);
  const annotations = {
    readOnlyHint: false,
    destructiveHint: false,
    idempotentHint: true,
    openWorldHint: false,
  };
  server.registerTool(
    "setup_prepare",
    {
      description:
        "Prepare an owned worktree from a fixed Git commit, managed Node/pnpm/Go, dependencies and a dev CLI build. includeWorkingChanges explicitly overlays current source changes and records their digest. Does not copy .env or start the app.",
      inputSchema: { ...prepareInput.shape, requestId: requestIdSchema },
      annotations,
    },
    (input) =>
      respond(() => {
        const { requestId: _, ...value } = input;
        return submit("setup.prepare", input, value);
      }),
  );
  for (const name of ["start", "stop"] as const)
    server.registerTool(
      `setup_${name}`,
      {
        description:
          name === "start"
            ? "Start the owned backend and dev Electron, verify the actual diagnostic identity, and use its bundled daemon. Refuses other live runtimes and task markers. Login is through the normal application."
            : "Stop only this environment's confirmed processes and daemon, retaining data and evidence; report unconfirmed cleanup.",
        inputSchema: {
          environmentId: resourceIdSchema,
          requestId: requestIdSchema,
        },
        annotations,
      },
      (input) =>
        respond(() =>
          submit(`setup.${name}`, input, {
            environmentId: input.environmentId,
          }),
        ),
    );

  const selection = z
    .object({
      conversationIds: z.array(z.string()).max(100).optional(),
      projectIds: z.array(z.string()).max(100).optional(),
      issueIds: z.array(z.string()).max(100).optional(),
    })
    .strict();
  server.registerTool(
    "setup_data",
    {
      description:
        "Bind a host-authorized source, inspect its capabilities, capture versioned selected content, restore a local test copy, or create controlled conversation history. Credentials are never tool inputs. Snapshot results return metadata; content remains in the private archive.",
      inputSchema: {
        action: z.discriminatedUnion("operation", [
          z
            .object({
              operation: z.literal("bindSource"),
              sourceRef: z.string(),
              requestId: requestIdSchema,
            })
            .strict(),
          z
            .object({
              operation: z.literal("inspectSource"),
              sourceId: resourceIdSchema,
            })
            .strict(),
          z
            .object({
              operation: z.literal("createSnapshot"),
              sourceId: resourceIdSchema,
              selection,
              requestId: requestIdSchema,
            })
            .strict(),
          z
            .object({
              operation: z.literal("inspectSnapshot"),
              dataSnapshotId: resourceIdSchema,
            })
            .strict(),
          z
            .object({
              operation: z.literal("restoreSnapshot"),
              environmentId: resourceIdSchema,
              dataSnapshotId: resourceIdSchema,
              requestId: requestIdSchema,
            })
            .strict(),
          z
            .object({
              operation: z.literal("bindLiveDataset"),
              environmentId: resourceIdSchema,
              sourceId: resourceIdSchema,
              requestId: requestIdSchema,
            })
            .strict(),
          z
            .object({
              operation: z.literal("createFixture"),
              environmentId: resourceIdSchema,
              template: z.literal("conversation.controlled/v1"),
              historyMessageCount: z.number().int().min(0).max(1000).default(0),
              requestId: requestIdSchema,
            })
            .strict(),
          z
            .object({
              operation: z.literal("resumeImport"),
              datasetId: resourceIdSchema,
              requestId: requestIdSchema,
            })
            .strict(),
          z
            .object({
              operation: z.literal("inspectDataset"),
              datasetId: resourceIdSchema,
            })
            .strict(),
          z
            .object({
              operation: z.literal("resolveReference"),
              datasetId: resourceIdSchema,
              originalId: z.string(),
            })
            .strict(),
        ]),
      },
      annotations,
    },
    ({ action }) =>
      respond(async () => {
        if (action.operation === "inspectSource")
          return readOk(await host.data.inspect(action.sourceId));
        if (action.operation === "inspectSnapshot") {
          const { content: _, ...value } = await host.data.getSnapshot(
            action.dataSnapshotId,
          );
          return readOk(value);
        }
        if (action.operation === "inspectDataset")
          return readOk(
            summarizeDataset(await host.data.get(action.datasetId)),
          );
        if (action.operation === "resolveReference") {
          const dataset = await host.data.get(action.datasetId);
          return readOk({
            datasetId: dataset.datasetId,
            restoreVersion: dataset.restoreVersion,
            targetRef: dataset.refs[action.originalId] ?? null,
          });
        }
        const { requestId, operation, ...input } = action;
        const name = {
          bindSource: "bind",
          createSnapshot: "snapshot",
          restoreSnapshot: "restore",
          bindLiveDataset: "live",
          createFixture: "fixture",
          resumeImport: "resume",
        }[operation];
        if ("template" in input) {
          const { template: _, ...fixture } = input;
          return host.jobs.submit(`data.${name}`, toJson(fixture), requestId);
        }
        return host.jobs.submit(`data.${name}`, toJson(input), requestId);
      }),
  );
  server.registerTool(
    "setup_reset",
    {
      description:
        "Reset an owned snapshot copy. Live sources are never reset. Previous local conversations are stopped and archived; evidence is retained.",
      inputSchema: { datasetId: resourceIdSchema, requestId: requestIdSchema },
      annotations,
    },
    (input) =>
      respond(() =>
        submit("data.reset", input, { datasetId: input.datasetId }),
      ),
  );
  server.registerTool(
    "setup_fixture_control",
    {
      description:
        "Inspect or release/chunk/fail the fake provider of an owned dataset. Never calls a real model.",
      inputSchema: {
        datasetId: resourceIdSchema,
        action: z.enum(["inspect", "release", "chunk", "fail"]),
        text: z.string().max(100000).default(""),
        expectedRequestId: z.string().min(1).optional(),
        requestId: requestIdSchema.optional(),
      },
      annotations,
    },
    (input) =>
      respond(async () =>
        input.action === "inspect"
          ? readOk(await host.data.control(input.datasetId, "inspect"))
          : host.jobs.submit(
              "data.control",
              toJson({
                datasetId: input.datasetId,
                action: input.action,
                text: input.text,
                expectedRequestId: input.expectedRequestId,
              }),
              requestIdSchema.parse(input.requestId),
            ),
      ),
  );

  server.registerTool(
    "verification_snapshot",
    {
      description:
        "Read per-source capabilities or an actual state snapshot. Missing sources remain unavailable; UI fallback defaults are never treated as facts.",
      inputSchema: {
        operation: z.enum(["capabilities", "snapshot"]),
        environmentId: resourceIdSchema,
        subjectRef: z.string().default(""),
        sources: z
          .array(z.enum(["daemon", "main", "renderer", "backend"]))
          .min(1)
          .max(4)
          .default(["daemon"]),
      },
      annotations: {
        ...annotations,
        readOnlyHint: true,
        idempotentHint: false,
      },
    },
    (input) =>
      respond(async () => {
        if (input.operation === "capabilities")
          return readOk(await host.sources.capabilities(input.environmentId));
        const snapshot = await host.sources.snapshot(
          input.environmentId,
          input.subjectRef,
          input.sources,
        );
        return readOk({
          ...snapshot,
          observations: snapshot.observations.map((item) => {
            if (
              item.source !== "daemon" ||
              !item.payload ||
              typeof item.payload !== "object" ||
              Array.isArray(item.payload)
            )
              return item;
            const payload = item.payload;
            return {
              ...item,
              payload: {
                ...payload,
                turns: Array.isArray(payload.turns)
                  ? payload.turns.map((turn) => {
                      const value = z
                        .record(z.string(), z.unknown())
                        .parse(turn);
                      return {
                        id: value.id,
                        request_id: value.request_id,
                        status: value.status,
                      };
                    })
                  : undefined,
              },
            };
          }),
        });
      }),
  );
  server.registerTool(
    "verification_step",
    {
      description:
        "Begin/end bounded step collection. Wait for a completed begin receipt and check coverage before acting. End confirms source watermarks and archives events and snapshots.",
      inputSchema: {
        action: z.discriminatedUnion("operation", [
          z
            .object({
              operation: z.literal("begin"),
              verificationId: resourceIdSchema,
              environmentId: resourceIdSchema,
              subjectRef: z.string(),
              description: z.string().max(2000),
              driver: z.enum(["playwright", "computerUse", "api"]),
              sources: z
                .array(z.enum(["daemon", "main", "renderer", "backend"]))
                .min(1)
                .max(4),
              requestId: requestIdSchema,
            })
            .strict(),
          z
            .object({
              operation: z.literal("inspect"),
              stepId: resourceIdSchema,
            })
            .strict(),
          z
            .object({
              operation: z.literal("list"),
              verificationId: resourceIdSchema,
              cursor: z.number().int().nonnegative().default(0),
              limit: z.number().int().min(1).max(100).default(50),
            })
            .strict(),
          z
            .object({
              operation: z.literal("end"),
              stepId: resourceIdSchema,
              requestId: requestIdSchema,
            })
            .strict(),
        ]),
      },
      annotations,
    },
    ({ action }) =>
      respond(async () => {
        if (action.operation === "inspect") {
          const { events, ...step } = await host.sources.get(action.stepId);
          return readOk({ ...step, eventCount: events.length });
        }
        if (action.operation === "list")
          return readOk(
            await host.sources.list(
              action.verificationId,
              action.cursor,
              action.limit,
            ),
          );
        const { operation, requestId, ...input } = action;
        return host.jobs.submit(
          `sources.${operation}`,
          toJson(input),
          requestId,
        );
      }),
  );
  server.registerTool(
    "verification_events",
    {
      description:
        "Read a bounded page of collected step events with explicit coverage and gaps.",
      inputSchema: {
        stepId: resourceIdSchema,
        cursor: z.number().int().nonnegative().default(0),
        limit: z.number().int().min(1).max(200).default(100),
      },
      annotations: { ...annotations, readOnlyHint: true },
    },
    (input) =>
      respond(async () =>
        readOk(
          await host.sources.events(input.stepId, input.cursor, input.limit),
        ),
      ),
  );
  server.registerTool(
    "verification_check",
    {
      description:
        "List predicates or run a deterministic check. Checks execute in the verification host. Failed/unavailable checks remain factual results and cannot be overridden by the caller.",
      inputSchema: {
        action: z.discriminatedUnion("operation", [
          z.object({ operation: z.literal("list") }).strict(),
          checkInput.extend({
            operation: z.literal("check"),
            requestId: requestIdSchema,
          }),
        ]),
      },
      annotations,
    },
    ({ action }) =>
      respond(async () => {
        if (action.operation === "list") return readOk(host.checks.list());
        const { operation: _, requestId, ...input } = action;
        return host.jobs.submit("checks.check", toJson(input), requestId);
      }),
  );

  server.registerTool(
    "verification_evidence",
    {
      description:
        "Attach authorized evidence files, list a verification's archive, or read checksum-verified evidence. Image reads return actual MCP images. Text reads are bounded.",
      inputSchema: {
        action: z.discriminatedUnion("operation", [
          z
            .object({
              operation: z.literal("attach"),
              verificationId: resourceIdSchema,
              environmentId: resourceIdSchema.optional(),
              stepId: resourceIdSchema.optional(),
              path: z.string(),
              kind: z.enum(["screenshot", "log", "data"]),
              source: z.string().max(200),
              mediaType: z.string().optional(),
              requestId: requestIdSchema,
            })
            .strict(),
          z
            .object({
              operation: z.literal("pin"),
              verificationId: resourceIdSchema,
              pinned: z.boolean(),
              requestId: requestIdSchema,
            })
            .strict(),
          z
            .object({
              operation: z.literal("prune"),
              requestId: requestIdSchema,
            })
            .strict(),
          z
            .object({
              operation: z.literal("list"),
              verificationId: resourceIdSchema,
              cursor: z.number().int().nonnegative().default(0),
              limit: z.number().int().min(1).max(100).default(50),
            })
            .strict(),
          z
            .object({
              operation: z.literal("read"),
              evidenceId: resourceIdSchema,
              offset: z.number().int().nonnegative().default(0),
              limit: z.number().int().min(1).max(32000).default(8000),
            })
            .strict(),
        ]),
      },
      annotations,
    },
    async ({ action }) => {
      if (action.operation === "read") {
        try {
          const { evidence, bytes } = await host.evidence.read(
            action.evidenceId,
          );
          if (evidence.kind === "screenshot")
            return {
              content: [
                {
                  type: "image" as const,
                  data: bytes.toString("base64"),
                  mimeType: evidence.mediaType,
                },
                { type: "text" as const, text: JSON.stringify(evidence) },
              ],
              structuredContent: { evidence },
            };
          const text = bytes.toString("utf8");
          return respond(async () =>
            readOk({
              evidence,
              text: text.slice(action.offset, action.offset + action.limit),
              nextOffset: Math.min(text.length, action.offset + action.limit),
              hasMore: action.offset + action.limit < text.length,
            }),
          );
        } catch (error) {
          return respond(async () => {
            throw error;
          });
        }
      }
      return respond(async () => {
        if (action.operation === "list")
          return readOk(
            await host.evidence.list(
              action.verificationId,
              action.cursor,
              action.limit,
            ),
          );
        const { operation: _, requestId, ...input } = action;
        return host.jobs.submit(
          `evidence.${action.operation}`,
          toJson(input),
          requestId,
        );
      });
    },
  );
  server.registerTool(
    "verification_export",
    {
      description:
        "Export a fixed report revision locally as JSON, Markdown or standalone HTML. Validates archived file checksums. Does not publish or upload.",
      inputSchema: {
        verificationId: resourceIdSchema,
        reportRevision: resourceIdSchema,
        format: z.enum(["json", "markdown", "html", "bundle"]),
        requestId: requestIdSchema,
      },
      annotations,
    },
    (input) =>
      respond(() =>
        submit("evidence.export", input, {
          verificationId: input.verificationId,
          reportRevision: input.reportRevision,
          format: input.format,
        }),
      ),
  );
  server.registerTool(
    "page_windows",
    {
      description:
        "List pages belonging to this environment's confirmed dev Electron connection.",
      inputSchema: { environmentId: resourceIdSchema },
      annotations: { ...annotations, readOnlyHint: true },
    },
    (input) =>
      respond(async () =>
        readOk(await host.pages.windows(input.environmentId)),
      ),
  );
  server.registerTool(
    "page_observe",
    {
      description:
        "Observe a window/frame and obtain fresh element references. Navigation, actions and replacement invalidate references. Password values are masked.",
      inputSchema: {
        environmentId: resourceIdSchema,
        windowRef: resourceIdSchema,
        frameRef: resourceIdSchema.optional(),
        offset: z.number().int().min(0).max(10000).default(0),
        limit: z.number().int().min(1).max(100).default(50),
      },
      annotations: {
        ...annotations,
        readOnlyHint: true,
        idempotentHint: false,
      },
    },
    (input) =>
      respond(async () =>
        readOk(
          await host.pages.observe(
            input.environmentId,
            input.windowRef,
            input.frameRef,
            input.offset,
            input.limit,
          ),
        ),
      ),
  );
  server.registerTool(
    "page_action",
    {
      description:
        "Perform a finite page action in an owned test environment with a ready step and fresh observation. Actions serialize and never report business success merely because a click completed.",
      inputSchema: {
        verificationId: resourceIdSchema,
        environmentId: resourceIdSchema,
        stepId: resourceIdSchema,
        windowRef: resourceIdSchema,
        observationId: resourceIdSchema,
        targetRef: resourceIdSchema,
        action: pageActionSchema,
        requestId: requestIdSchema,
      },
      annotations,
    },
    (input) =>
      respond(() => {
        const { requestId: _, ...value } = input;
        return submit("page.action", input, value);
      }),
  );
  server.registerTool(
    "page_screenshot",
    {
      description:
        "Capture the actual page and register it as step evidence. Does not capture native system dialogs.",
      inputSchema: {
        verificationId: resourceIdSchema,
        environmentId: resourceIdSchema,
        stepId: resourceIdSchema,
        windowRef: resourceIdSchema,
        fullPage: z.boolean().default(false),
        requestId: requestIdSchema,
      },
      annotations,
    },
    (input) =>
      respond(() => {
        const { requestId: _, ...value } = input;
        return submit("page.screenshot", input, value);
      }),
  );
  return server;
}
