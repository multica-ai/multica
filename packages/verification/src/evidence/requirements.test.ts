// @vitest-environment node
import { randomUUID } from "node:crypto";
import { expect, it } from "vitest";
import { verificationSchema } from "../contracts/index.js";
import {
  checkSchema,
  datasetSchema,
  evidenceSchema,
  stepSchema,
} from "../contracts/resources.js";
import { enforcePlan } from "./requirements.js";
it("requires declared boundary checks, the same current dataset and per-step UI evidence", () => {
  const baseline = randomUUID(),
    candidate = randomUUID(),
    snapshot = randomUUID(),
    verificationId = randomUUID();
  const record = verificationSchema.parse({
    schemaVersion: 1,
    verificationId,
    projectRef: "0".repeat(64),
    title: "Fix",
    kind: "bug",
    state: "open",
    createdAt: new Date().toISOString(),
    environments: [
      { environmentId: baseline, role: "baseline" },
      { environmentId: candidate, role: "candidate" },
    ],
    plan: {
      scenario: "queue-once/v1",
      scope: ["queue"],
      dataSnapshotId: snapshot,
      providerScript: "controlled/v1",
      checks: [
        {
          key: "target",
          purpose: "target",
          kind: "queue.itemCount",
          operator: "eq",
          expected: 0,
          temporal: "now",
        },
        {
          key: "boundary",
          purpose: "boundary",
          kind: "queue.paused",
          operator: "eq",
          expected: false,
          temporal: "now",
        },
      ],
      ui: { required: true, colorSchemes: ["light"] },
    },
  });
  const datasets = [baseline, candidate].map((environmentId) =>
    datasetSchema.parse({
      schemaVersion: 1,
      datasetId: randomUUID(),
      environmentId,
      mode: "realSnapshot",
      dataSnapshotId: snapshot,
      restoreVersion: 1,
      providerScript: "controlled/v1",
      refs: { conversationId: "subject" },
      state: "ready",
      resources: ["subject"],
    }),
  );
  const steps = [baseline, candidate].map((environmentId) =>
    stepSchema.parse({
      schemaVersion: 1,
      stepId: randomUUID(),
      verificationId,
      environmentId,
      subjectRef: "subject",
      description: "Scenario",
      driver: "playwright",
      state: "ended",
      startedAt: new Date().toISOString(),
      startSnapshotId: randomUUID(),
      startCursors: {},
      endCursors: {},
      generations: {},
      coverage: "complete",
      gaps: [],
      events: [],
    }),
  );
  const checks = [
    ...record.plan!.checks.map((declaration) => ({ declaration, role: 1 })),
    { declaration: record.plan!.checks[0]!, role: 0 },
  ].map(({ declaration, role }) =>
    checkSchema.parse({
      schemaVersion: 1,
      checkId: randomUUID(),
      comparisonKey: declaration.key,
      verificationId,
      environmentId: datasets[role]!.environmentId,
      stepId: steps[role]!.stepId,
      subjectRef: "subject",
      predicate: {
        kind: declaration.kind,
        operator: declaration.operator,
        expected: declaration.expected,
      },
      temporal: { mode: "now" },
      status: role === 0 ? "failed" : "passed",
      startedAt: new Date().toISOString(),
      endedAt: new Date().toISOString(),
      evidenceIds: [],
      datasetId: datasets[role]!.datasetId,
      dataSnapshotId: snapshot,
      restoreVersion: 1,
    }),
  );
  const images = steps.map((step) =>
    evidenceSchema.parse({
      schemaVersion: 1,
      evidenceId: randomUUID(),
      verificationId,
      environmentId: step.environmentId,
      stepId: step.stepId,
      kind: "screenshot",
      source: "playwright",
      path: "image.png",
      sha256: "0".repeat(64),
      bytes: 10,
      mediaType: "image/png",
      colorScheme: "light",
      capturedAt: new Date().toISOString(),
      receivedAt: new Date().toISOString(),
      coverage: "complete",
    }),
  );
  expect(() =>
    enforcePlan(record, checks, datasets, steps, images),
  ).not.toThrow();
  expect(() =>
    enforcePlan(
      record,
      checks.filter((check) => check.comparisonKey !== "boundary"),
      datasets,
      steps,
      images,
    ),
  ).toThrow(/boundary/);
  expect(() =>
    enforcePlan(
      record,
      checks,
      datasets.map((dataset) => ({ ...dataset, restoreVersion: 2 })),
      steps,
      images,
    ),
  ).toThrow(/restore version/);
  const newStepId = randomUUID();
  const currentChecks = checks
    .filter((check) => check.environmentId === candidate)
    .map((check) => ({
      ...check,
      checkId: randomUUID(),
      restoreVersion: 2,
      stepId: newStepId,
    }));
  const currentDatasets = datasets.map((dataset) =>
    dataset.environmentId === candidate
      ? { ...dataset, restoreVersion: 2 }
      : dataset,
  );
  expect(() =>
    enforcePlan(
      record,
      [...checks, ...currentChecks],
      currentDatasets,
      [...steps, { ...steps[1]!, stepId: newStepId }],
      images,
    ),
  ).toThrow(/screenshot/);
  expect(() => enforcePlan(record, checks, datasets, steps, [])).toThrow(
    /screenshot/,
  );
  expect(() =>
    enforcePlan(
      { ...record, plan: undefined },
      checks,
      datasets,
      steps,
      images,
    ),
  ).toThrow(/plan/);
});
