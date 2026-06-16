"use client";

import { useState } from "react";
import { FileText, Plus, Trash2 } from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import type { Agent } from "@multica/core/types";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  skillListOptions,
  workspaceKeys,
} from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import { AppLink } from "../../../navigation";
import { SkillAddDialog } from "../skill-add-dialog";
import { useT } from "../../../i18n";

export function SkillsTab({
  agent,
}: {
  agent: Agent;
}) {
  const { t } = useT("agents");
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  // Same query the SkillAddDialog uses (TanStack Query dedupes by key, so
  // this isn't an extra request) — used here only to grey out the "Add
  // skill" button when the workspace has zero skills total. When skills
  // exist but are all already attached, we still open the dialog: it
  // filters out attached skills and renders a localised "no more skills
  // to add" empty state, which is more useful than a mysterious
  // greyed-out button.
  const { data: workspaceSkills = [] } = useQuery(skillListOptions(wsId));
  const [removing, setRemoving] = useState(false);
  const [showAdd, setShowAdd] = useState(false);

  const handleRemove = async (skillId: string) => {
    setRemoving(true);
    try {
      const newIds = agent.skills
        .filter((s) => s.id !== skillId)
        .map((s) => s.id);
      await api.setAgentSkills(agent.id, { skill_ids: newIds });
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.tab_body.skills.remove_failed_toast));
    } finally {
      setRemoving(false);
    }
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between gap-3">
        <p className="text-xs text-muted-foreground">
          {t(($) => $.tab_body.skills.intro)}
        </p>
        <Button
          variant="outline"
          size="sm"
          onClick={() => setShowAdd(true)}
          disabled={workspaceSkills.length === 0}
          className="shrink-0"
        >
          <Plus className="h-3 w-3" />
          {t(($) => $.tab_body.skills.add_action)}
        </Button>
      </div>

      {agent.skills.length === 0 ? (
        <div className="flex flex-col items-center justify-center rounded-lg border border-dashed py-12">
          <FileText className="h-8 w-8 text-muted-foreground/40" />
          <p className="mt-3 text-sm text-muted-foreground">
            {t(($) => $.tab_body.skills.empty_title)}
          </p>
          <p className="mt-1 max-w-xs text-center text-xs text-muted-foreground">
            {t(($) => $.tab_body.skills.empty_hint)}
          </p>
          {workspaceSkills.length > 0 && (
            <Button
              onClick={() => setShowAdd(true)}
              size="sm"
              className="mt-3"
            >
              <Plus className="h-3 w-3" />
              {t(($) => $.tab_body.skills.add_action)}
            </Button>
          )}
        </div>
      ) : (
        <ul className="space-y-1.5">
          {agent.skills.map((skill) => (
            <li
              key={skill.id}
              className="group/skill-row relative rounded-md transition-colors hover:bg-accent/40 focus-within:bg-accent/40"
            >
              <AppLink
                href={paths.skillDetail(skill.id)}
                className="flex min-w-0 items-center gap-2.5 rounded-md px-2 py-2 pr-8 outline-none focus-visible:ring-2 focus-visible:ring-ring"
              >
                <span
                  className="flex size-6 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground"
                  aria-hidden="true"
                >
                  <FileText className="h-3.5 w-3.5" />
                </span>
                <div className="min-w-0 flex-1">
                  <div className="text-sm font-medium">{skill.name}</div>
                  {skill.description && (
                    <div className="truncate text-xs text-muted-foreground">
                      {skill.description}
                    </div>
                  )}
                </div>
              </AppLink>
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label={t(($) => $.tab_body.skills.remove_aria)}
                onClick={() => handleRemove(skill.id)}
                disabled={removing}
                className="absolute right-1.5 top-1/2 z-10 -translate-y-1/2 bg-transparent text-muted-foreground/0 opacity-0 transition-opacity hover:bg-transparent hover:text-muted-foreground focus-visible:text-muted-foreground focus-visible:opacity-100 group-hover/skill-row:text-muted-foreground group-hover/skill-row:opacity-100 group-focus-within/skill-row:text-muted-foreground group-focus-within/skill-row:opacity-100"
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
