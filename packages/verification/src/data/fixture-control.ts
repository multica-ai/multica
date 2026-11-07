import { z } from "zod";
import { jsonSchema, resourceIdSchema } from "../contracts/index.js";

const toolSchema = z.union([
  z
    .object({
      id: z.string().min(1),
      name: z.string().min(1),
      input: z.record(z.string(), jsonSchema),
    })
    .strict(),
  z
    .object({
      id: z.string().min(1),
      output: jsonSchema,
      isError: z.boolean().optional(),
    })
    .strict(),
]);

export const fixtureControlInput = z
  .object({
    environmentId: resourceIdSchema,
    conversationId: z.string().min(1),
    action: z.enum([
      "inspect", "release", "chunk", "fail", "tool_start", "tool_result",
    ]),
    text: z.string().max(100000).default(""),
    expectedRequestId: z.string().min(1).optional(),
    tool: toolSchema.optional(),
  })
  .superRefine((input, ctx) => {
    if (input.action !== "inspect" && !input.expectedRequestId)
      ctx.addIssue({
        code: "custom", path: ["expectedRequestId"],
        message: "Mutating fixture control requires expectedRequestId",
      });
    const isTool = input.action === "tool_start" || input.action === "tool_result";
    if (isTool) {
      if (
        !input.tool ||
        (input.action === "tool_start"
          ? !("name" in input.tool)
          : !("output" in input.tool))
      )
        ctx.addIssue({
          code: "custom", path: ["tool"],
          message: "Tool payload must match the action",
        });
      if (input.text !== "")
        ctx.addIssue({
          code: "custom", path: ["text"],
          message: "Tool actions use tool payloads",
        });
    } else if (input.tool) {
      ctx.addIssue({
        code: "custom", path: ["tool"],
        message: "Only tool actions accept tool payloads",
      });
    }
  });

export type FixtureAction = z.infer<typeof fixtureControlInput>["action"];
export type FixtureTool = z.infer<typeof toolSchema>;
