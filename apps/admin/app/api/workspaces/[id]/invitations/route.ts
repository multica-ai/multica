import { NextRequest, NextResponse } from "next/server";
import { z } from "zod";
import { getPendingInvitations, getWorkspaceMembers, getWorkspaceMetadata } from "@/lib/queries";
import { computeInviteEligibility } from "@/lib/invite-eligibility";
import { createWorkspaceInvitation } from "@/lib/agentfarm-api";

const InviteBodySchema = z.object({
  email: z.string().trim().toLowerCase().email(),
  role: z.enum(["admin", "member"]),
});

// POST /api/workspaces/[id]/invitations — invite a user into a workspace.
//
// LBYL, not EAFP: every condition that would make the write fail is checked
// against a fresh DB read *before* calling the Go API, so the normal error
// path is "we already knew this wouldn't work" rather than "the Go API told
// us after the fact". See lib/invite-eligibility.ts for why these checks are
// re-run here even though the client already ran the same checks against the
// data it loaded on page open — a page can go stale between load and submit.
// The Go API call at the end can still itself return 403/409 (a same-instant
// race), which is mapped through as a last-resort fallback, not the primary
// mechanism.
export async function POST(request: NextRequest, { params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;

  let rawBody: unknown;
  try {
    rawBody = await request.json();
  } catch {
    return NextResponse.json({ error: "Invalid JSON body" }, { status: 400 });
  }
  const parsedBody = InviteBodySchema.safeParse(rawBody);
  if (!parsedBody.success) {
    return NextResponse.json({ error: "A valid email and role ('admin' or 'member') are required" }, { status: 400 });
  }
  const { email, role } = parsedBody.data;

  try {
    const metadata = await getWorkspaceMetadata(id);
    if (!metadata) {
      return NextResponse.json({ error: "Workspace not found" }, { status: 404 });
    }

    const [members, pendingInvitations] = await Promise.all([
      getWorkspaceMembers(metadata.id),
      getPendingInvitations(metadata.id),
    ]);

    const eligibility = computeInviteEligibility(members);
    if (!eligibility.eligible) {
      const error =
        eligibility.reason === "pat-missing"
          ? "The admin dashboard's BOT_PAT isn't configured — invites are unavailable until this deployment is fixed."
          : `${eligibility.botEmail} isn't an owner/admin of this workspace — ask an existing owner to add it before inviting through this dashboard.`;
      return NextResponse.json({ error }, { status: 409 });
    }

    if (members.some((m) => m.email.toLowerCase() === email)) {
      return NextResponse.json({ error: "This email is already a member of the workspace." }, { status: 409 });
    }

    if (pendingInvitations.some((inv) => inv.email.toLowerCase() === email)) {
      return NextResponse.json({ error: "This email already has a pending invitation." }, { status: 409 });
    }

    const result = await createWorkspaceInvitation(metadata.id, email, role);
    if (!result.ok) {
      return NextResponse.json({ error: result.message }, { status: result.status });
    }

    return NextResponse.json(result.invitation, { status: 201 });
  } catch (error) {
    console.error("[admin] POST /api/workspaces/[id]/invitations failed", error);
    return NextResponse.json({ error: "Failed to create invitation" }, { status: 500 });
  }
}
