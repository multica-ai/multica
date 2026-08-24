import { z } from "zod";

// External-boundary schema for the Go backend's invitation endpoints
// (server/internal/handler/invitation.go's InvitationResponse). Unlike
// lib/litellm-schema.ts (a genuinely third-party, independently-versioned
// API), this Go backend and admin are part of the same first-party deploy
// and roll out together via GitOps on merge to main — there's no installed-
// client-lags-server gap like the desktop app has. So this schema is
// deliberately strict, not lenient: every field is required, and a response
// missing any of them is treated as a failure (see agentfarm-api.ts's
// "parse, don't cast" handling and agentfarm-api.test.ts's malformed-2xx
// case) rather than silently degraded.

export const InvitationSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  invitee_email: z.string(),
  role: z.enum(["admin", "member"]),
  status: z.string(),
  created_at: z.string(),
  expires_at: z.string(),
});

export type Invitation = z.infer<typeof InvitationSchema>;

// The Go handler's writeError() shape — {"error": "...", "code"?: "..."}.
export const ApiErrorSchema = z.object({
  error: z.string(),
  code: z.string().nullish(),
});
