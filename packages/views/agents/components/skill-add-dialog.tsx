"use client";

import { useState } from "react";
import { FileText } from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import type { Agent } from "@multica/core/types";
import { useT } from "@multica/i18n/react";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  skillListOptions,
  workspaceKeys,
} from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";

/**
 * Single source of truth for "attach a workspace skill to this agent".
 * Used by both:
 *   - SkillsTab — full surface, "Add skill" button
 *   - Inspector → SkillAttach — inline dashed `+ Attach` chip
 *
 * Owns the workspace-skill list query, the "what's still attachable" filter,
 * the API call, and the optimistic invalidation. Callers only manage the
 * open/close state — they don't repeat the attach logic.
 */
export function SkillAddDialog({
  agent,
  open,
  onOpenChange,
}: {
  agent: Agent;
  open: boolean;
  onOpenChange: (v: boolean) => void;
}) {
  const wsId = useWorkspaceId();
  const t = useT("agents");
  const tc = useT("common");
  const qc = useQueryClient();
  const { data: workspaceSkills = [] } = useQuery(skillListOptions(wsId));
  const [saving, setSaving] = useState(false);

  const agentSkillIds = new Set(agent.skills.map((s) => s.id));
  const availableSkills = workspaceSkills.filter(
    (s) => !agentSkillIds.has(s.id),
  );

  const handleAdd = async (skillId: string) => {
    setSaving(true);
    try {
      const newIds = [...agent.skills.map((s) => s.id), skillId];
      await api.setAgentSkills(agent.id, { skill_ids: newIds });
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
      onOpenChange(false);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("toast_failed_add_skill"));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle className="text-sm">{t("skill_add_title")}</DialogTitle>
          <DialogDescription className="text-xs">
            {t("skill_add_desc")}
          </DialogDescription>
        </DialogHeader>
        <div className="max-h-64 space-y-1 overflow-y-auto">
          {availableSkills.map((skill) => (
            <button
              key={skill.id}
              onClick={() => handleAdd(skill.id)}
              disabled={saving}
              className="flex w-full items-center gap-2.5 rounded-md px-3 py-2 text-left text-sm transition-colors hover:bg-accent/50"
            >
              <FileText className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
              <div className="min-w-0 flex-1">
                <div className="truncate font-medium">{skill.name}</div>
                {skill.description && (
                  <div className="truncate text-xs text-muted-foreground">
                    {skill.description}
                  </div>
                )}
              </div>
            </button>
          ))}
          {availableSkills.length === 0 && (
            <p className="py-6 text-center text-xs text-muted-foreground">
              {t("skill_all_assigned")}
            </p>
          )}
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            {tc("cancel")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
