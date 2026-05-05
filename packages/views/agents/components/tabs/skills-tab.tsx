"use client";

import { useState } from "react";
import { FileText, Info, Plus, Trash2 } from "lucide-react";
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
import { SkillAddDialog } from "../skill-add-dialog";

export function SkillsTab({
  agent,
}: {
  agent: Agent;
}) {
  const qc = useQueryClient();
  const t = useT("agents");
  const tc = useT("common");
  const wsId = useWorkspaceId();
  // Same query the SkillAddDialog uses (TanStack Query dedupes by key, so
  // this isn't an extra request) — used here only to grey out the "Add skill"
  // button when there's nothing left to attach.
  const { data: workspaceSkills = [] } = useQuery(skillListOptions(wsId));
  const [removing, setRemoving] = useState(false);
  const [showAdd, setShowAdd] = useState(false);

  const agentSkillIds = new Set(agent.skills.map((s) => s.id));
  const availableCount = workspaceSkills.filter(
    (s) => !agentSkillIds.has(s.id),
  ).length;

  const handleRemove = async (skillId: string) => {
    setRemoving(true);
    try {
      const newIds = agent.skills
        .filter((s) => s.id !== skillId)
        .map((s) => s.id);
      await api.setAgentSkills(agent.id, { skill_ids: newIds });
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("toast_failed_remove_skill"));
    } finally {
      setRemoving(false);
    }
  };

  return (
    <div className="space-y-4">
      <div className="flex items-start justify-between gap-3">
        <p className="text-xs text-muted-foreground">
          {t("skills_assigned_desc")}
        </p>
        <Button
          variant="outline"
          size="sm"
          onClick={() => setShowAdd(true)}
          disabled={availableCount === 0}
          className="shrink-0"
        >
          <Plus className="h-3 w-3" />
          {tc("add")} {t("inspector_skills").toLowerCase()}
        </Button>
      </div>

      <div className="flex items-start gap-2 rounded-md border border-info/20 bg-info/5 px-3 py-2.5">
        <Info className="mt-0.5 h-3.5 w-3.5 shrink-0 text-info" />
        <p className="text-xs text-muted-foreground">
          {t("skills_importing_note")}
        </p>
      </div>

      {agent.skills.length === 0 ? (
        <div className="flex flex-col items-center justify-center rounded-lg border border-dashed py-12">
          <FileText className="h-8 w-8 text-muted-foreground/40" />
          <p className="mt-3 text-sm text-muted-foreground">
            {t("skills_no_skills")}
          </p>
          <p className="mt-1 max-w-xs text-center text-xs text-muted-foreground">
            {t("skills_add_hint")}
          </p>
          {availableCount > 0 && (
            <Button
              onClick={() => setShowAdd(true)}
              size="sm"
              className="mt-3"
            >
              <Plus className="h-3 w-3" />
              {tc("add")} {t("inspector_skills").toLowerCase()}
            </Button>
          )}
        </div>
      ) : (
        <ul className="space-y-1.5">
          {agent.skills.map((skill) => (
            <li
              key={skill.id}
              className="flex items-center gap-2.5 rounded-md border px-3 py-2"
            >
              <FileText className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
              <div className="min-w-0 flex-1">
                <div className="text-sm font-medium">{skill.name}</div>
                {skill.description && (
                  <div className="truncate text-xs text-muted-foreground">
                    {skill.description}
                  </div>
                )}
              </div>
              <Button
                variant="ghost"
                size="icon-sm"
                onClick={() => handleRemove(skill.id)}
                disabled={removing}
                className="text-muted-foreground hover:text-destructive"
              >
                <Trash2 className="h-3.5 w-3.5" />
              </Button>
            </li>
          ))}
        </ul>
      )}

      <SkillAddDialog agent={agent} open={showAdd} onOpenChange={setShowAdd} />
    </div>
  );
}
