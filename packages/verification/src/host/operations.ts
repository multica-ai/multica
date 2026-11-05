import { z } from "zod";
import {
  jsonSchema,
  resourceIdSchema,
  toJson,
  type Json,
} from "../contracts/index.js";
import type { JobService, OperationContext } from "./jobs.js";
import type { DataService } from "../data/service.js";
import type { Sources } from "../sources/service.js";
import type { EvidenceStore } from "../evidence/store.js";
import { Checks, checkInput } from "../checks/service.js";
import { Pages, pageActionSchema } from "../page/service.js";

export function registerOperations(
  jobs: JobService,
  services: {
    data: DataService;
    sources: Sources;
    evidence: EvidenceStore;
    checks: Checks;
    pages: Pages;
  },
) {
  const register = <T extends Json>(
    name: string,
    input: z.ZodType<T>,
    execute: (value: T, context: OperationContext) => Promise<unknown>,
  ) =>
    jobs.register(name, {
      input,
      output: jsonSchema,
      outputSchema: `${name}/v1`,
      run: async (value, context) => {
        const parsed = input.parse(value);
        if (parsed && !Array.isArray(parsed) && typeof parsed === "object")
          for (const key of [
            "verificationId",
            "environmentId",
            "stepId",
            "datasetId",
          ])
            if (
              typeof parsed[key] === "string" &&
              resourceIdSchema.safeParse(parsed[key]).success
            )
              await context.addResource(parsed[key]);
        return toJson(await execute(parsed, context));
      },
    });
  register(
    "data.bind",
    z.object({ sourceRef: z.string() }).strict(),
    ({ sourceRef }) => services.data.bind(sourceRef),
  );
  register(
    "data.snapshot",
    z
      .object({
        sourceId: resourceIdSchema,
        selection: z
          .object({
            conversationIds: z.array(z.string()).max(100).optional(),
            projectIds: z.array(z.string()).max(100).optional(),
            issueIds: z.array(z.string()).max(100).optional(),
          })
          .strict(),
      })
      .strict(),
    async ({ sourceId, selection }, context) => {
      const snapshot = await services.data.snapshot(sourceId, selection);
      await context.addResource(snapshot.dataSnapshotId);
      return services.data.describeSnapshot(snapshot.dataSnapshotId);
    },
  );
  register(
    "data.restore",
    z
      .object({
        environmentId: resourceIdSchema,
        dataSnapshotId: resourceIdSchema,
      })
      .strict(),
    async ({ environmentId, dataSnapshotId }, context) => {
      const dataset = await services.data.restore(
        environmentId,
        dataSnapshotId,
        undefined,
        context,
      );
      await context.addResource(dataset.datasetId);
      return summarizeDataset(dataset);
    },
  );
  register(
    "data.resume",
    z.object({ datasetId: resourceIdSchema }).strict(),
    async ({ datasetId }, context) =>
      summarizeDataset(await services.data.resume(datasetId, context)),
  );
  register(
    "data.live",
    z
      .object({ environmentId: resourceIdSchema, sourceId: resourceIdSchema })
      .strict(),
    ({ environmentId, sourceId }) =>
      services.data.live(environmentId, sourceId),
  );
  register(
    "data.fixture",
    z
      .object({
        environmentId: resourceIdSchema,
        historyMessageCount: z.number().int().min(0).max(1000).default(0),
      })
      .strict(),
    async ({ environmentId, historyMessageCount }, context) => {
      const dataset = await services.data.fixture(
        environmentId,
        historyMessageCount,
        context,
      );
      await context.addResource(dataset.datasetId);
      return summarizeDataset(dataset);
    },
  );
  register(
    "data.workspaceFixture",
    z
      .object({
        environmentId: resourceIdSchema,
        issueCount: z.number().int().min(0).max(20).default(3),
      })
      .strict(),
    async ({ environmentId, issueCount }, context) =>
      summarizeDataset(
        await services.data.workspaceFixture(
          environmentId,
          issueCount,
          context,
        ),
      ),
  );
  register(
    "data.reset",
    z.object({ datasetId: resourceIdSchema }).strict(),
    async ({ datasetId }, context) => {
      await context.addResource(datasetId);
      return summarizeDataset(await services.data.reset(datasetId));
    },
  );
  register(
    "data.control",
    z
      .object({
        datasetId: resourceIdSchema,
        action: z.enum(["release", "chunk", "fail"]),
        text: z.string().max(100000).default(""),
        expectedRequestId: z.string().min(1),
      })
      .strict(),
    ({ datasetId, action, text, expectedRequestId }) =>
      services.data.control(datasetId, action, text, expectedRequestId),
  );
  register(
    "sources.begin",
    z
      .object({
        verificationId: resourceIdSchema,
        environmentId: resourceIdSchema,
        subjectRef: z.string(),
        description: z.string().max(2000),
        driver: z.enum(["playwright", "computerUse", "api"]),
        optionalSources: z
          .array(z.enum(["daemon", "main", "renderer", "backend", "page"]))
          .max(5)
          .default([]),
        sources: z
          .array(z.enum(["daemon", "main", "renderer", "backend", "page"]))
          .min(1)
          .max(5),
      })
      .strict(),
    async (input, context) => {
      const step = await services.sources.begin(input, context);
      await context.addResource(step.stepId);
      return { ...step, events: undefined };
    },
  );
  register(
    "sources.end",
    z.object({ stepId: resourceIdSchema }).strict(),
    async ({ stepId }, context) => {
      await context.addResource(stepId);
      const step = await services.sources.end(stepId);
      return { ...step, events: undefined, eventCount: step.events.length };
    },
  );
  register("checks.check", checkInput, (input, context) =>
    services.checks.check(input, context.signal, context.addResource),
  );
  register(
    "evidence.attach",
    z
      .object({
        verificationId: resourceIdSchema,
        environmentId: resourceIdSchema.optional(),
        stepId: resourceIdSchema.optional(),
        path: z.string(),
        kind: z.enum(["screenshot", "log", "data"]),
        source: z.string().max(200),
        mediaType: z.string().optional(),
        capturedAt: z.iso.datetime().optional(),
        colorScheme: z.enum(["light", "dark"]).optional(),
      })
      .strict(),
    (input) => services.evidence.attachFile(input),
  );
  register(
    "evidence.attachImage",
    z
      .object({
        verificationId: resourceIdSchema,
        environmentId: resourceIdSchema,
        stepId: resourceIdSchema,
        data: z.string().max(44 * 1024 * 1024),
        mediaType: z.enum(["image/png", "image/jpeg"]),
        source: z.string().min(1).max(200),
        capturedAt: z.iso.datetime(),
        colorScheme: z.enum(["light", "dark"]),
      })
      .strict(),
    (input) => services.evidence.attachImage(input),
  );
  register(
    "evidence.bindEnvironment",
    z
      .object({
        verificationId: resourceIdSchema,
        environmentId: resourceIdSchema,
        role: z.enum(["baseline", "candidate", "smoke"]),
      })
      .strict(),
    ({ verificationId, environmentId, role }) =>
      services.evidence.bindEnvironment(verificationId, environmentId, role),
  );
  register(
    "evidence.finalize",
    z
      .object({
        verificationId: resourceIdSchema,
        conclusion: z.enum(["passed", "failed", "inconclusive", "waived"]),
        explanation: z.string().max(20000),
        uncovered: z.array(z.string()).max(100),
        waiverRef: z.string().optional(),
      })
      .strict(),
    async (input) => {
      const report = await services.evidence.finalize(input);
      return {
        reportRevision: report.reportRevision,
        verificationId: report.verificationId,
        conclusion: report.conclusion,
      };
    },
  );
  register(
    "evidence.pin",
    z
      .object({ verificationId: resourceIdSchema, pinned: z.boolean() })
      .strict(),
    ({ verificationId, pinned }) =>
      services.evidence.pin(verificationId, pinned),
  );
  register("evidence.prune", z.object({}).strict(), () =>
    services.evidence.prune(),
  );
  register(
    "evidence.export",
    z
      .object({
        verificationId: resourceIdSchema,
        reportRevision: resourceIdSchema,
        format: z.enum(["json", "markdown", "html", "bundle"]),
      })
      .strict(),
    ({ verificationId, reportRevision, format }) =>
      services.evidence.export(verificationId, reportRevision, format),
  );
  register(
    "page.action",
    z
      .object({
        verificationId: resourceIdSchema,
        environmentId: resourceIdSchema,
        stepId: resourceIdSchema,
        windowRef: resourceIdSchema,
        observationId: resourceIdSchema,
        targetRef: resourceIdSchema,
        action: pageActionSchema,
      })
      .strict(),
    (input) => services.pages.perform(input),
  );
  register(
    "page.screenshot",
    z
      .object({
        verificationId: resourceIdSchema,
        environmentId: resourceIdSchema,
        stepId: resourceIdSchema,
        windowRef: resourceIdSchema,
        fullPage: z.boolean().default(false),
      })
      .strict(),
    ({ verificationId, environmentId, stepId, windowRef, fullPage }) =>
      services.pages.capture(
        verificationId,
        environmentId,
        stepId,
        windowRef,
        fullPage,
      ),
  );
}
export function summarizeDataset(
  dataset: Awaited<ReturnType<DataService["get"]>>,
) {
  const refs = Object.fromEntries(Object.entries(dataset.refs).slice(0, 50));
  if (dataset.refs.conversationId)
    refs.conversationId = dataset.refs.conversationId;
  return {
    ...dataset,
    refs,
    referenceCount: Object.keys(dataset.refs).length,
    resources: dataset.resources.slice(0, 100),
    resourceCount: dataset.resources.length,
  };
}
