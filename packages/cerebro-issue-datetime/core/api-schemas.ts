import { z } from "zod";

// Response reader routes through parseWithFallback so an older/newer backend
// degrades to safe defaults instead of breaking the UI. See CLAUDE.md → API
// Response Compatibility. agent_start_kind tolerates enum drift: an unknown
// value downgrades to null (inherit the workspace default), never crashes.
const agentStartKindSchema = z
  .enum(["none", "start", "due"])
  .nullable()
  .catch(null)
  .default(null);

export const issueDateTimesSchema = z.object({
  start_time: z.string().nullable().default(null),
  due_time: z.string().nullable().default(null),
  agent_start_kind: agentStartKindSchema,
});

export const workspaceDateTimeSettingsSchema = z.object({
  default_agent_start_kind: z
    .enum(["none", "start", "due"])
    .catch("none")
    .default("none"),
});
