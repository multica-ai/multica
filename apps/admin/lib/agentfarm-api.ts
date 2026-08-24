import "server-only";
import { InvitationSchema, ApiErrorSchema, type Invitation } from "./agentfarm-schema";

/**
 * Thin client for the Go backend's invitation write path, authenticated as
 * the bot account via BOT_PAT — the same pattern gandalf's workspace-create
 * flow uses (~/WORK/gandalf/src/services/workspace-create.service.ts), reused
 * per the plan's chosen approach instead of adding a new Go-side auth
 * concept. BOT_PAT arrives for free via gitops/base/secret-store.yaml's
 * /agentfarm/tools/* glob — no new secret wiring needed.
 *
 * Unlike gandalf (which runs outside the cluster and must hit the public
 * origin), admin can reach the backend in-cluster — see
 * gitops/base/service-backend.yaml's agentfarm-backend ClusterIP Service.
 */

const BASE = (process.env.AGENTFARM_API_BASE_URL || "http://localhost:8080").replace(/\/$/, "");

export type CreateInvitationResult =
  | { ok: true; invitation: Invitation }
  | { ok: false; status: number; message: string };

/**
 * POST /api/workspaces/{id}/members as the bot PAT.
 *
 * This is the LAST-RESORT leg of the invite flow, not the primary error
 * handling mechanism: the route handler runs its own fresh feasibility
 * checks (lib/invite-eligibility.ts + getPendingInvitations/getWorkspaceMembers)
 * immediately before calling this, so a non-ok response here should only
 * ever happen from a genuine race (someone else invited/joined in the
 * seconds between our check and this call) — it is handled, not assumed
 * unreachable, but the UI should rarely if ever surface it.
 */
export async function createWorkspaceInvitation(
  workspaceId: string,
  email: string,
  role: "admin" | "member",
): Promise<CreateInvitationResult> {
  const botPat = process.env.BOT_PAT;
  if (!botPat) {
    // The route handler's LBYL eligibility check should already have caught
    // this — see invite-eligibility.ts's "pat-missing" reason — but this is
    // a reusable boundary client, so it fails fast here too rather than
    // sending a Bearer header with an empty token to the Go API.
    return { ok: false, status: 500, message: "BOT_PAT is not configured" };
  }
  const res = await fetch(`${BASE}/api/workspaces/${workspaceId}/members`, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${botPat}`,
      "Content-Type": "application/json",
    },
    body: JSON.stringify({ email, role }),
  });

  const raw: unknown = await res.json().catch(() => null);

  if (!res.ok) {
    const parsedError = ApiErrorSchema.safeParse(raw);
    return {
      ok: false,
      status: res.status,
      message: parsedError.success ? parsedError.data.error : `Go API request failed: ${res.status}`,
    };
  }

  const parsed = InvitationSchema.safeParse(raw);
  if (!parsed.success) {
    console.warn("[admin] POST /api/workspaces/:id/members response failed schema validation", {
      issues: parsed.error.issues,
    });
    // Not res.status: that's still the Go API's original 2xx here, and the
    // route handler + hooks.ts's fetchJson both branch on the HTTP status to
    // decide success/failure. Passing the 2xx through with ok:false would
    // have callers treat this failure as a success (toast + dialog close)
    // while the invitation is actually in an unknown/malformed state.
    return {
      ok: false,
      status: 502,
      message: "Invitation may have been created, but the response was malformed",
    };
  }

  return { ok: true, invitation: parsed.data };
}
