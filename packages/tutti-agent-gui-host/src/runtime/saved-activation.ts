import { z } from "zod";
import { persistedQueuedPromptSchema } from "@multica/core/local-conversation/submission-storage";

/** Persisted activation input is validated before it can create a conversation again. */
export const savedActivationSchema = z.object({
  mode: z.literal("new"),
  agentSessionId: z.string().min(1),
  agentTargetId: z.string().min(1),
  requestId: z.string().min(1),
  clientSubmitId: z.string().min(1),
  cwd: z.string().optional(),
  title: z.string().optional(),
  optimisticTitle: z.string().optional(),
  initialContent:
    persistedQueuedPromptSchema.shape.display.shape.content.optional(),
  runtimeContent:
    persistedQueuedPromptSchema.shape.display.shape.content.optional(),
  initialDisplayPrompt: z.string().optional(),
  isolation: z.literal("worktree").optional(),
  modelExplicit: z.boolean().optional(),
  reasoningEffortExplicit: z.boolean().optional(),
  initialTurnExpected: z.boolean().optional(),
  visible: z.boolean().optional(),
  railSectionKey: z.string().optional(),
  railPlacement: z
    .discriminatedUnion("kind", [
      z.object({
        version: z.literal(1),
        kind: z.literal("conversations"),
        sectionKey: z.literal("conversations"),
      }),
      z.object({
        version: z.literal(1),
        kind: z.literal("project"),
        sectionKey: z.string(),
        projectPath: z.string(),
      }),
    ])
    .optional(),
  settings: z
    .object({
      model: z.string().nullable().optional(),
      reasoningEffort: z.string().nullable().optional(),
      permissionModeId: z.string().nullable().optional(),
      codexSaverMode: z.boolean().nullable().optional(),
      rtkSaverMode: z.boolean().nullable().optional(),
      planMode: z.boolean().nullable().optional(),
      browserUse: z.boolean().nullable().optional(),
      computerUse: z.boolean().nullable().optional(),
      speed: z.string().nullable().optional(),
    })
    .optional(),
});
