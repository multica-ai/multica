"use client";

// FIR-2629 — Skill ownership panels injected into the skill detail sidebar.
// Rendered via CEREBRO-PATCH(skill-ownership-ui) in packages/views/skills/components/skill-detail-page.tsx.

import { useAuthStore } from "@multica/core/auth";
import type { MemberWithUser, Skill } from "@multica/core/types";
import { SkillOwnershipPanel } from "./components/skill-ownership-panel";
import { SkillVersionsPanel } from "./components/skill-versions-panel";
import { SkillChangeRequestQueue } from "./components/skill-change-request-queue";
import { SkillForkPanel } from "./components/skill-fork-panel";

interface Props {
  skill: Skill;
  wsId: string;
  members: MemberWithUser[];
  /** Whether the current user can directly edit/save this skill. */
  canEdit: boolean;
}

export function CerebroSkillDetailExtension({
  skill,
  wsId,
  members,
  canEdit,
}: Props) {
  const userId = useAuthStore((s) => s.user?.id ?? null);

  // Owner can edit ownership — same bar as canEdit (admin/owner/creator) + explicit owner_id check
  const isOwner = !!(userId && skill.owner_id && skill.owner_id === userId);
  const canManageOwnership = canEdit || isOwner;

  // Show change-request queue to those who can manage (owner + approvers + admins)
  const isApprover = !!(
    userId && (skill.approver_ids ?? []).includes(userId)
  );
  const canReview = canEdit || isOwner || isApprover;

  return (
    <>
      <div className="my-3 h-px bg-border" />

      <div className="space-y-4">
        <SkillOwnershipPanel
          skill={skill}
          wsId={wsId}
          members={members}
          canManage={canManageOwnership}
        />

        <SkillVersionsPanel skill={skill} members={members} />

        {canReview && (
          <SkillChangeRequestQueue
            skill={skill}
            wsId={wsId}
            members={members}
          />
        )}

        <SkillForkPanel skill={skill} wsId={wsId} />
      </div>
    </>
  );
}
