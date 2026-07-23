import { z } from "zod";

export const commandSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  key: z.string(),
  title: z.string(),
  description: z.string().default(""),
  argv: z.array(z.string()).default([]),
  created_by_id: z.string(),
  created_by_type: z.string(),
  created_at: z.string(),
  updated_at: z.string(),
}).passthrough();

export const commandsListSchema = z.object({ commands: z.array(commandSchema).default([]) }).passthrough();
