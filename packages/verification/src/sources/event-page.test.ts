// @vitest-environment node
import { randomUUID } from "node:crypto";
import { expect, it } from "vitest";
import { stepSchema } from "../contracts/resources.js";
import { pageEvents } from "./event-page.js";
it("binds event pages to observation generations and filters", () => {
  const step = stepSchema.parse({
    schemaVersion: 1,
    stepId: randomUUID(),
    verificationId: randomUUID(),
    environmentId: randomUUID(),
    subjectRef: "conversation",
    description: "events",
    driver: "api",
    state: "ended",
    startedAt: new Date().toISOString(),
    startSnapshotId: randomUUID(),
    startCursors: {},
    endCursors: {},
    generations: { daemon: "generation" },
    coverage: "complete",
    gaps: [],
    events: [1, 2, 3].map((cursor) => ({
      eventId: `${cursor}`,
      source: "daemon",
      instance: "owned",
      generation: "generation",
      cursor,
      type: cursor === 2 ? "other" : "target",
      receivedAt: new Date().toISOString(),
      payload: null,
    })),
  });
  const first = pageEvents(step, { types: ["target"] }, undefined, 1);
  expect(first.items[0]?.cursor).toBe(1);
  expect(first.hasMore).toBe(true);
  expect(
    pageEvents(step, { types: ["target"] }, first.nextCursor, 1).items[0]
      ?.cursor,
  ).toBe(3);
  expect(() => pageEvents(step, {}, first.nextCursor)).toThrow();
  expect(() =>
    pageEvents(
      { ...step, generations: { daemon: "changed" } },
      { types: ["target"] },
      first.nextCursor,
    ),
  ).toThrow();
});
