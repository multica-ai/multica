import { z } from "zod";
import { ApiError } from "../api/client";

const WeixinConflictSchema = z.object({
  code: z.literal("weixin_already_connected"),
  agent_id: z.string().optional().catch(undefined),
  agent_name: z.string().optional().catch(undefined),
});

export function weixinConflict(error: unknown) {
  if (!(error instanceof ApiError) || error.status !== 409) return undefined;
  const parsed = WeixinConflictSchema.safeParse(error.body);
  return parsed.success ? parsed.data : undefined;
}
