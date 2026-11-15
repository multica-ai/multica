import { z } from "zod";

export const GitHubConnectionEventSchema = z.object({
  installation: z.object({ id: z.string(), workspace_id: z.string() }),
  connection_id: z.string().optional(),
});
