import { z } from "zod";

// Response reader routes through parseWithFallback so an older/newer backend
// degrades to safe defaults instead of breaking the UI. See CLAUDE.md → API
// Response Compatibility.
export const issueDateTimesSchema = z.object({
  start_time: z.string().nullable().default(null),
  due_time: z.string().nullable().default(null),
});
