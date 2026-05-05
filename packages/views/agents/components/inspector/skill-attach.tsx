"use client";

import { useState } from "react";
import { Plus } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import type { Agent } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { skillListOptions } from "@multica/core/workspace/queries";
import { SkillAddDialog } from "../skill-add-dialog";
import { useT } from "@multica/i18n/react";

/**
 * Inline "+ Attach" trigger for the inspector's Skills row. The trigger is
 * the dashed-border chip; clicking it opens the shared `SkillAddDialog` —
 * same surface the SkillsTab uses for its own "Add skill" button. Single
 * source of truth for the attach flow, single visual for the picker.
 *
 * Hidden when there's nothing left to attach so we don't dangle a chip
 * that opens an empty dialog.
 */
export function SkillAttach({ agent }: { agent: Agent }) {
  const t = useT("agents");
  const wsId = useWorkspaceId();
  const { data: workspaceSkills = [] } = useQuery(skillListOptions(wsId));
  const [open, setOpen] = useState(false);

  const agentSkillIds = new Set(agent.skills.map((s) => s.id));
  const availableCount = workspaceSkills.filter(
    (s) => !agentSkillIds.has(s.id),
  ).length;

  if (availableCount === 0) return null;

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        aria-label={t("skill_attach_desc")}
        title={t("skill_attach_desc")}
        className="inline-flex cursor-pointer items-center gap-0.5 rounded-md border border-dashed border-muted-foreground/30 px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground/70 transition-colors hover:border-muted-foreground/60 hover:bg-accent/50 hover:text-muted-foreground"
      >
        <Plus className="h-2.5 w-2.5" />
        {t("skill_attach")}
      </button>
      <SkillAddDialog agent={agent} open={open} onOpenChange={setOpen} />
    </>
  );
}
