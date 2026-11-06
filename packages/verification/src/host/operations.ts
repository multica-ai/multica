import { z } from "zod";
import { jsonSchema, resourceIdSchema, toJson } from "../contracts/index.js";
import type { JobService } from "./jobs.js";
import type { DataService } from "../data/service.js";

export const snapshotInput = z
  .object({
    sourceRef: z.string().min(1).max(160),
    selection: z
      .object({
        conversationIds: z.array(z.string()).max(100).optional(),
        projectIds: z.array(z.string()).max(100).optional(),
        issueIds: z.array(z.string()).max(100).optional(),
      })
      .strict(),
  })
  .strict();
export const restoreInput = z
  .object({
    environmentId: resourceIdSchema,
    content: z.discriminatedUnion("kind", [
      z
        .object({
          kind: z.literal("snapshot"),
          dataSnapshotId: resourceIdSchema,
        })
        .strict(),
      z
        .object({
          kind: z.literal("conversationTemplate"),
          historyMessageCount: z.number().int().min(0).max(1000).default(0),
        })
        .strict(),
      z
        .object({
          kind: z.literal("workspaceTemplate"),
          issueCount: z.number().int().min(0).max(20).default(3),
        })
        .strict(),
    ]),
  })
  .strict();

export function registerOperations(jobs: JobService, data: DataService) {
  jobs.register("data.snapshot", {
    input: snapshotInput,
    output: jsonSchema,
    outputSchema: "dataSnapshot/v1",
    run: async (input, context) => {
      const { sourceRef, selection } = snapshotInput.parse(input);
      const snapshot = await data.snapshot(sourceRef, selection);
      await context.addResource(snapshot.dataSnapshotId);
      return toJson(await data.describeSnapshot(snapshot.dataSnapshotId));
    },
  });
  jobs.register("data.restore", {
    input: restoreInput,
    output: jsonSchema,
    outputSchema: "dataset/v1",
    run: async (input, context) => {
      const { environmentId, content } = restoreInput.parse(input);
      await context.addResource(environmentId);
      const dataset =
        content.kind === "snapshot"
          ? await data.restore(environmentId, content.dataSnapshotId, context)
          : content.kind === "conversationTemplate"
            ? await data.fixture(
                environmentId,
                content.historyMessageCount,
                context,
              )
            : await data.workspaceFixture(
                environmentId,
                content.issueCount,
                context,
              );
      return toJson(dataset);
    },
  });
}
