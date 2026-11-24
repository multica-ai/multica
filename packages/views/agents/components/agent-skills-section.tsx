"use client";

import { useState } from "react";
import { Loader2, Plus, Settings2, X } from "@multica/ui/icons";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import type { Agent } from "@multica/core/types";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  skillListOptions,
  workspaceKeys,
} from "@multica/core/workspace/queries";
import { cn } from "@multica/ui/lib/utils";
import { SkillIcon } from "../../skills/lib/skill-icon";
import {
  ObjectSection,
  ObjectSectionAction,
} from "../../layout/object-page";
import { useT } from "../../i18n";
import { SkillAddDialog } from "./skill-add-dialog";

/**
 * The skills assigned to the agent, as chips. Adding and removing happen
 * here; pausing a skill and the runtime's own skills live in Advanced
 * settings, which `onManage` opens.
 */
export function AgentSkillsSection({
  agent,
  canEdit,
  onManage,
}: {
  agent: Agent;
  canEdit: boolean;
  onManage: () => void;
}) {
  const { t } = useT("agents");
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const { data: workspaceSkills = [] } = useQuery(skillListOptions(wsId));
  const [showAdd, setShowAdd] = useState(false);
  const [removingId, setRemovingId] = useState<string | null>(null);

  const handleRemove = async (skillId: string) => {
    setRemovingId(skillId);
    try {
      await api.removeAgentSkill(agent.id, skillId);
      await qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t(($) => $.tab_body.skills.remove_failed_toast),
      );
    } finally {
      setRemovingId(null);
    }
  };

  return (
    <ObjectSection
      title={t(($) => $.tabs.skills)}
      count={agent.skills.length}
      action={
        <>
          <ObjectSectionAction icon={Settings2} onClick={onManage}>
            {t(($) => $.sections.manage)}
          </ObjectSectionAction>
          {canEdit ? (
            <ObjectSectionAction
              icon={Plus}
              onClick={() => setShowAdd(true)}
              disabled={workspaceSkills.length === 0}
            >
              {t(($) => $.sections.add)}
            </ObjectSectionAction>
          ) : null}
        </>
      }
    >
      {agent.skills.length === 0 ? (
        <p className="text-body text-muted-foreground">
          {t(($) => $.tab_body.skills.empty_title)}
        </p>
      ) : (
        <ul className="flex flex-wrap gap-1.5">
          {agent.skills.map((skill) => {
            const paused = skill.enabled === false;
            return (
              <li
                key={skill.id}
                className={cn(
                  "group flex h-7 max-w-full items-center gap-1.5 rounded-md border bg-card pl-2 text-caption",
                  canEdit ? "pr-1" : "pr-2",
                  paused && "text-muted-foreground",
                )}
              >
                <SkillIcon className="size-3.5 shrink-0 text-muted-foreground" />
                <span className="min-w-0 truncate">{skill.name}</span>
                {canEdit ? (
                  <button
                    type="button"
                    onClick={() => handleRemove(skill.id)}
                    disabled={removingId !== null}
                    aria-label={t(($) => $.tab_body.skills.remove_aria, {
                      name: skill.name,
                    })}
                    className="flex size-5 shrink-0 items-center justify-center rounded-sm text-muted-foreground opacity-0 transition-opacity hover:bg-accent hover:text-foreground focus-visible:opacity-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed group-hover:opacity-100 [@media(hover:none)]:opacity-100"
                  >
                    {removingId === skill.id ? (
                      <Loader2 className="size-3 animate-spin motion-reduce:animate-none" />
                    ) : (
                      <X className="size-3" aria-hidden="true" />
                    )}
                  </button>
                ) : null}
              </li>
            );
          })}
        </ul>
      )}
      <SkillAddDialog agent={agent} open={showAdd} onOpenChange={setShowAdd} />
    </ObjectSection>
  );
}
