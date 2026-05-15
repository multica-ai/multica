"use client";

import { useState } from "react";
import { FileText, Search } from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import type { Agent } from "@multica/core/types";
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
import { Input } from "@multica/ui/components/ui/input";
import { useT } from "../../i18n";

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
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const { data: workspaceSkills = [] } = useQuery(skillListOptions(wsId));
  const [saving, setSaving] = useState(false);
  const [query, setQuery] = useState("");

  const agentSkillIds = new Set(agent.skills.map((s) => s.id));
  const availableSkills = workspaceSkills.filter(
    (s) => !agentSkillIds.has(s.id),
  );
  const trimmedQuery = query.trim().toLowerCase();
  const filteredSkills = trimmedQuery
    ? availableSkills.filter((s) => {
        const name = s.name.toLowerCase();
        const description = s.description?.toLowerCase() ?? "";
        return (
          name.includes(trimmedQuery) || description.includes(trimmedQuery)
        );
      })
    : availableSkills;

  const handleOpenChange = (v: boolean) => {
    if (!v) setQuery("");
    onOpenChange(v);
  };

  const handleAdd = async (skillId: string) => {
    setSaving(true);
    try {
      const newIds = [...agent.skills.map((s) => s.id), skillId];
      await api.setAgentSkills(agent.id, { skill_ids: newIds });
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
      handleOpenChange(false);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.tab_body.skills.add_failed_toast));
    } finally {
      setSaving(false);
    }
  };

  const showSearch = availableSkills.length > 0;
  const noMatch = showSearch && filteredSkills.length === 0;

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle className="text-sm">{t(($) => $.tab_body.skills.add_dialog_title)}</DialogTitle>
          <DialogDescription className="text-xs">
            {t(($) => $.tab_body.skills.add_dialog_description)}
          </DialogDescription>
        </DialogHeader>
        {showSearch && (
          <div className="relative">
            <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              autoFocus
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={t(($) => $.tab_body.skills.add_dialog_search_placeholder)}
              aria-label={t(($) => $.tab_body.skills.add_dialog_search_placeholder)}
              className="pl-7"
            />
          </div>
        )}
        <div className="max-h-64 space-y-1 overflow-y-auto">
          {filteredSkills.map((skill) => (
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
              {t(($) => $.tab_body.skills.add_dialog_empty)}
            </p>
          )}
          {noMatch && (
            <p className="py-6 text-center text-xs text-muted-foreground">
              {t(($) => $.tab_body.skills.add_dialog_no_match)}
            </p>
          )}
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={() => handleOpenChange(false)}>
            {t(($) => $.tab_body.skills.add_dialog_cancel)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
