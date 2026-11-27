import { z } from "zod";

// POST /api/device/inspect: the machine that started a pending device login.
// An older server that omits client_name still yields a usable result.
export const DeviceAuthorizationSchema = z.object({
  client_name: z.string().default(""),
});

// POST /api/device/approve: only an explicit "approved" counts as success.
export const DeviceApprovalSchema = z.object({
  status: z.literal("approved"),
});
