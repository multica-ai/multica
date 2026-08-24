import type { InviteEligibility, WorkspaceMember } from "./types";

// Pure logic, deliberately not `server-only` — same reasoning as
// lib/litellm-match.ts: this is called from server-only route handlers
// either way, but keeping it free of the marker (and of any DB/fetch import)
// makes it directly unit-testable. See invite-eligibility.test.ts.
//
/**
 * LBYL pre-check, not a post-hoc 403 interpretation: computed from the same
 * member roster the detail panel already fetches (lib/queries.ts's
 * getWorkspaceMembers), so it costs nothing extra and is known before the
 * operator ever opens the invite dialog. Re-run against a fresh member read
 * in the route handler right before the write, to close the gap between
 * page load and submit.
 *
 * AGENTFARM_BOT_EMAIL identifies the account whose PAT (BOT_PAT, delivered
 * via gitops/base/secret-store.yaml's /agentfarm/tools/* glob) admin uses to
 * call the Go API — the same "agentfarm-bot@g2.com" account gandalf's
 * workspace-create flow hardcodes. Every Go API credential resolves to a
 * real user (see server/internal/middleware/auth.go — there is no
 * service-role concept), so that account must itself be an owner/admin
 * member of a workspace before admin can invite into it. Both env reads
 * happen inside the function (not as module-level constants) so eligibility
 * reflects the environment at call time, not at first import.
 */
export function computeInviteEligibility(members: WorkspaceMember[]): InviteEligibility {
  const botEmail = process.env.AGENTFARM_BOT_EMAIL || "agentfarm-bot@g2.com";
  // BOT_PAT missing is a deployment misconfiguration, not a per-workspace
  // condition — but it's still a feasibility fact worth catching before the
  // operator fills out a dialog that can never submit successfully. Checked
  // first so the reported reason is the actual root cause rather than the
  // (also-true) "bot isn't a member" symptom.
  const botPatConfigured = Boolean(process.env.BOT_PAT);
  if (!botPatConfigured) {
    return { eligible: false, botEmail, reason: "pat-missing" };
  }
  const botIsWorkspaceAdmin = members.some(
    (m) => m.email.toLowerCase() === botEmail.toLowerCase() && (m.role === "owner" || m.role === "admin"),
  );
  if (!botIsWorkspaceAdmin) {
    return { eligible: false, botEmail, reason: "not-workspace-admin" };
  }
  return { eligible: true, botEmail, reason: null };
}
