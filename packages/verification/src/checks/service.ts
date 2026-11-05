import { randomUUID } from "node:crypto";
import { z } from "zod";
import {
  checkSchema,
  datasetSchema,
  predicateSchema,
  predicateDefinition,
  stateSnapshotSchema,
  temporalSchema,
  type Check,
  type StateSnapshot,
} from "../contracts/resources.js";
import { jsonSchema, type Json } from "../contracts/index.js";
import type { HostStorage } from "../host/storage.js";
import type { Sources } from "../sources/service.js";
import type { Pages } from "../page/service.js";
import type { EvidenceStore } from "../evidence/store.js";
import { problem, toProblem, VerificationError } from "../host/problem.js";

export const checkInput = z
  .object({
    comparisonKey: z.string().min(1).max(160).optional(),
    verificationId: z.uuid(),
    environmentId: z.uuid(),
    subjectRef: z.string(),
    stepId: z.uuid().optional(),
    predicate: predicateSchema,
    temporal: temporalSchema,
  })
  .strict();
const object = z.record(z.string(), jsonSchema);
export class Checks {
  constructor(
    private readonly storage: HostStorage,
    private readonly sources: Sources,
    private readonly pages: Pages,
    private readonly evidence: EvidenceStore,
  ) {}
  async list(environmentId: string) {
    const capabilities = await this.sources.capabilities(environmentId);
    const environment = await this.sources.environments.get(environmentId);
    return {
      environmentId,
      predicates: predicateSchema.shape.kind.options.map((kind) => {
        const definition = predicateDefinition(kind);
        const { schema, ...metadata } = definition;
        return {
          kind,
          ...metadata,
          schema: z.toJSONSchema(schema),
          availability:
            definition.source === "page"
              ? environment.state === "ready"
                ? "available"
                : "unavailable"
              : capabilities[definition.source] &&
                  !(capabilities[definition.source] as { status?: string })
                    .status
                ? "available"
                : "unavailable",
          requirements:
            kind === "providerFixture.receivedCount"
              ? ["dev daemon", "controlled fake provider receive records"]
              : [definition.source],
        };
      }),
      capabilities,
    };
  }
  async check(
    input: z.infer<typeof checkInput>,
    signal?: AbortSignal,
    checkpoint?: (id: string) => Promise<void>,
  ): Promise<Check> {
    const definition = predicateDefinition(input.predicate.kind);
    if (
      !(definition.operators as readonly string[]).includes(
        input.predicate.operator,
      ) ||
      !definition.temporal.includes(input.temporal.mode)
    )
      throw new VerificationError(
        problem(
          "unsupportedCapability",
          "The predicate does not support this operator or temporal mode",
        ),
      );
    definition.schema.parse(input.predicate);
    const record = await this.storage.read(
      `verifications/${input.verificationId}.json`,
      (await import("../contracts/index.js")).verificationSchema,
    );
    if (
      !record?.environments.some(
        (entry) => entry.environmentId === input.environmentId,
      )
    )
      throw new VerificationError(
        problem(
          "identityMismatch",
          "Bind the environment to the verification before checking",
        ),
      );
    if (input.stepId) {
      const step = await this.sources.get(input.stepId);
      if (
        step.verificationId !== input.verificationId ||
        step.environmentId !== input.environmentId ||
        step.subjectRef !== input.subjectRef
      )
        throw new VerificationError(
          problem(
            "identityMismatch",
            "The check step belongs to another subject or environment",
          ),
        );
    }
    const datasets = [];
    for (const name of await this.storage.list("datasets")) {
      const value = await this.storage.read(`datasets/${name}`, datasetSchema);
      if (
        value?.environmentId === input.environmentId &&
        value.state === "ready" &&
        Object.values(value.refs).includes(
          input.subjectRef.replace(/^(issue|project):/, ""),
        )
      )
        datasets.push(value);
    }
    const dataset = datasets.at(-1);
    if (
      record.plan?.dataSnapshotId &&
      (!dataset || dataset.dataSnapshotId !== record.plan.dataSnapshotId)
    )
      throw new VerificationError(
        problem(
          "identityMismatch",
          "The subject must belong to the plan's restored dataset",
        ),
      );
    const checkId = randomUUID();
    await checkpoint?.(checkId);
    const startedAt = new Date().toISOString(),
      deadline =
        Date.now() +
        (input.temporal.mode === "eventually" ? input.temporal.timeoutMs : 0);
    await this.storage.write(
      `checks/${checkId}.json`,
      checkSchema.parse({
        schemaVersion: 1,
        checkId,
        ...input,
        status: "unavailable",
        startedAt,
        endedAt: startedAt,
        evidenceIds: [],
        problem: problem(
          "outcomeUnknown",
          "This check has not completed",
          "inspect",
        ),
      }),
    );
    let actual: Json | undefined,
      status: Check["status"] = "unavailable",
      failure: Check["problem"],
      snapshot: StateSnapshot | undefined;
    for (;;) {
      signal?.throwIfAborted();
      try {
        if (input.temporal.mode === "throughout") {
          const step = await this.sources.get(input.temporal.stepId);
          if (
            step.verificationId !== input.verificationId ||
            step.environmentId !== input.environmentId ||
            step.subjectRef !== input.subjectRef ||
            step.state !== "ended" ||
            step.coverage !== "complete" ||
            !step.endSnapshotId
          )
            throw new VerificationError(
              problem(
                "coverageIncomplete",
                "The interval needs a matching ended step with complete event coverage",
              ),
            );
          const start = await this.storage.read(
              `state-snapshots/${step.startSnapshotId}.json`,
              stateSnapshotSchema,
            ),
            end = await this.storage.read(
              `state-snapshots/${step.endSnapshotId}.json`,
              stateSnapshotSchema,
            );
          if (!start || !end)
            throw new VerificationError(
              problem("coverageIncomplete", "Interval snapshots are missing"),
            );
          if (input.predicate.kind === "providerFixture.receivedCount")
            actual =
              this.fixtureCount(
                end,
                typeof input.predicate.target === "string"
                  ? input.predicate.target
                  : undefined,
              ) -
              this.fixtureCount(
                start,
                typeof input.predicate.target === "string"
                  ? input.predicate.target
                  : undefined,
              );
          else if (input.predicate.kind === "diagnostic.errorCount") {
            if (
              step.startCursors.main === undefined ||
              step.endCursors.main === undefined
            )
              throw new VerificationError(
                problem(
                  "unsupportedCapability",
                  "The error interval requires main diagnostic coverage",
                ),
              );
            actual = step.events.filter(
              (event) =>
                event.source === "main" &&
                [
                  "console.error",
                  "renderer.crashed",
                  "page.loadFailed",
                ].includes(event.type),
            ).length;
          } else
            throw new VerificationError(
              problem(
                "unsupportedCapability",
                "This predicate does not have an interval checker; use a final-state check",
              ),
            );
        } else if (input.predicate.kind.startsWith("page.")) {
          if (!input.predicate.target)
            throw new VerificationError(
              problem("invalidInput", "A page element reference is required"),
            );
          actual = jsonSchema.parse(
            await this.pages.readCondition(
              input.environmentId,
              input.predicate.target,
              input.predicate.kind,
            ),
          );
        } else {
          if (input.predicate.kind === "diagnostic.errorCount")
            throw new VerificationError(
              problem(
                "unsupportedCapability",
                "Error absence requires a complete observation interval",
              ),
            );
          snapshot = await this.sources.snapshot(
            input.environmentId,
            input.subjectRef,
            ["daemon"],
          );
          const observation = snapshot.observations.find(
            (source) => source.source === "daemon",
          );
          if (!observation || observation.status !== "available")
            throw new VerificationError(
              problem(
                "sourceUnavailable",
                "A valid daemon observation is required",
              ),
            );
          const payload = object.parse(observation.payload);
          if (input.predicate.kind === "queue.itemCount")
            actual = z
              .array(jsonSchema)
              .parse(object.parse(payload.queue).items).length;
          else if (input.predicate.kind === "queue.paused")
            actual = z.boolean().parse(object.parse(payload.queue).paused);
          else if (input.predicate.kind === "providerFixture.receivedCount")
            actual = this.fixtureCount(
              snapshot,
              typeof input.predicate.target === "string"
                ? input.predicate.target
                : undefined,
            );
          else if (input.predicate.kind === "turn.status") {
            const turns = z
              .array(
                z.object({ id: z.string(), status: z.string() }).passthrough(),
              )
              .parse(payload.turns);
            const turn = turns.find(
              (item) => item.id === input.predicate.target,
            );
            if (!turn)
              throw new VerificationError(
                problem(
                  "sourceUnavailable",
                  "The requested turn is not present in the loaded snapshot window",
                ),
              );
            actual = z
              .enum([
                "pending",
                "running",
                "completed",
                "failed",
                "cancelled",
                "interrupted",
              ])
              .parse(turn.status);
          }
        }
        if (typeof actual === "number" && actual < 0)
          throw new VerificationError(
            problem("coverageIncomplete", "The source counter moved backwards"),
          );
        if (actual === undefined)
          throw new VerificationError(
            problem(
              "unsupportedCapability",
              "The predicate has no installed checker",
            ),
          );
        status = compare(
          actual,
          input.predicate.expected,
          input.predicate.operator,
        )
          ? "passed"
          : "failed";
        failure = undefined;
      } catch (error) {
        status = "unavailable";
        failure = toProblem(error);
      }
      if (
        input.temporal.mode !== "eventually" ||
        status === "passed" ||
        Date.now() >= deadline
      )
        break;
      await new Promise<void>((resolve) =>
        setTimeout(resolve, Math.min(100, Math.max(0, deadline - Date.now()))),
      );
    }
    const check = checkSchema.parse({
      schemaVersion: 1,
      checkId,
      ...input,
      ...(input.temporal.mode === "throughout"
        ? { stepId: input.temporal.stepId }
        : {}),
      actual,
      datasetId: dataset?.datasetId,
      dataSnapshotId: dataset?.dataSnapshotId,
      restoreVersion: dataset?.restoreVersion,
      status,
      startedAt,
      endedAt: new Date().toISOString(),
      evidenceIds: [],
      problem: failure,
    });
    if (snapshot) {
      const item = await this.evidence.attach({
        verificationId: input.verificationId,
        environmentId: input.environmentId,
        stepId: input.stepId,
        kind: "snapshot",
        source: "check.snapshot",
        mediaType: "application/json",
        coverage: status === "unavailable" ? "partial" : "complete",
        bytes: Buffer.from(JSON.stringify(snapshot)),
      });
      check.evidenceIds.push(item.evidenceId);
    }
    const item = await this.evidence.attach({
      verificationId: input.verificationId,
      environmentId: input.environmentId,
      stepId: check.stepId,
      kind: "check",
      source: input.predicate.kind,
      mediaType: "application/json",
      coverage: status === "unavailable" ? "partial" : "complete",
      bytes: Buffer.from(JSON.stringify(check)),
    });
    check.evidenceIds.push(item.evidenceId);
    await this.storage.write(`checks/${check.checkId}.json`, check);
    return check;
  }
  private fixtureCount(snapshot: StateSnapshot, target?: string): number {
    const observation = snapshot.observations.find(
      (item) => item.source === "daemon",
    );
    if (!observation || observation.status !== "available")
      throw new VerificationError(
        problem(
          "sourceUnavailable",
          "The fake provider observation is missing",
        ),
      );
    const fixture = z
      .object({ generation: z.string(), received: z.array(z.string()) })
      .parse(object.parse(observation.payload).fixture);
    if (fixture.generation !== observation.generation)
      throw new VerificationError(
        problem(
          "identityMismatch",
          "Fake provider generation differs from the conversation source",
        ),
      );
    return fixture.received.filter((id) => !target || id === target).length;
  }
}
function compare(
  actual: Json,
  expected: Json,
  operator: z.infer<typeof predicateSchema>["operator"],
): boolean {
  if (operator === "eq")
    return JSON.stringify(actual) === JSON.stringify(expected);
  if (
    operator === "contains" &&
    typeof actual === "string" &&
    typeof expected === "string"
  )
    return actual.includes(expected);
  if (
    (operator === "gte" || operator === "lte") &&
    typeof actual === "number" &&
    typeof expected === "number"
  )
    return operator === "gte" ? actual >= expected : actual <= expected;
  throw new VerificationError(
    problem(
      "invalidInput",
      "The operator is not supported for these value types",
    ),
  );
}
