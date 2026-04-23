"use client";

import { useState, useMemo } from "react";
import { Plus, FileText, Trash2, Info, HardDrive, Sparkles } from "lucide-react";
import type { Agent, RuntimeLocalSkillSummary } from "@multica/core/types";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { useWorkspaceId, useAuthStore } from "@multica/core/hooks";
import { skillListOptions, workspaceKeys } from "@multica/core/workspace/queries";
import { runtimeListOptions, runtimeLocalSkillsOptions } from "@multica/core/runtimes";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";

export function SkillsTab({
  agent,
}: {
  agent: Agent;
}) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const userId = useAuthStore((s) => s.user?.id ?? null);
  const { data: workspaceSkills = [] } = useQuery(skillListOptions(wsId));
  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));
  const [saving, setSaving] = useState(false);
  const [showPicker, setShowPicker] = useState(false);

  // Get the agent's runtime and check if it's a local runtime owned by the user
  const agentRuntime = useMemo(() => 
    runtimes.find((r) => r.id === agent.runtime_id),
    [runtimes, agent.runtime_id]
  );
  
  const isLocalRuntime = agentRuntime?.runtime_mode === "local";
  const isRuntimeOwner = userId == null || agentRuntime?.owner_id === userId;
  const canFetchLocalSkills = isLocalRuntime && isRuntimeOwner && agentRuntime?.status === "online";
  
  // Fetch local runtime skills if applicable
  const { data: localSkillsResult } = useQuery({
    ...runtimeLocalSkillsOptions(agent.runtime_id),
    enabled: canFetchLocalSkills,
    staleTime: 60_000,
  });
  
  const localSkills = localSkillsResult?.skills ?? [];
  const localSkillsSupported = localSkillsResult?.supported ?? false;

  const agentSkillIds = new Set(agent.skills.map((s) => s.id));
  const availableSkills = workspaceSkills.filter((s) => !agentSkillIds.has(s.id));

  const handleAdd = async (skillId: string) => {
    setSaving(true);
    try {
      const newIds = [...agent.skills.map((s) => s.id), skillId];
      await api.setAgentSkills(agent.id, { skill_ids: newIds });
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to add skill");
    } finally {
      setSaving(false);
      setShowPicker(false);
    }
  };

  const handleRemove = async (skillId: string) => {
    setSaving(true);
    try {
      const newIds = agent.skills.filter((s) => s.id !== skillId).map((s) => s.id);
      await api.setAgentSkills(agent.id, { skill_ids: newIds });
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to remove skill");
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-sm font-semibold">Skills</h3>
          <p className="text-xs text-muted-foreground mt-0.5">
            {localSkills.length > 0 
              ? `${agent.skills.length} workspace + ${localSkills.length} local skills available`
              : "Workspace skills assigned to this agent."}
          </p>
        </div>
        <Button
          variant="outline"
          size="xs"
          onClick={() => setShowPicker(true)}
          disabled={saving || availableSkills.length === 0}
        >
          <Plus className="h-3 w-3" />
          Add Skill
        </Button>
      </div>

      <div className="flex items-start gap-2 rounded-md border border-info/20 bg-info/5 px-3 py-2.5">
        <Info className="h-3.5 w-3.5 shrink-0 text-info mt-0.5" />
        <p className="text-xs text-muted-foreground">
          Local runtime skills are always available automatically. Importing creates a workspace copy that your team can edit and reuse.
        </p>
      </div>

      {/* Unified skills list - workspace skills + local runtime skills */}
      <div className="space-y-4">
        {/* Workspace Skills Section */}
        {agent.skills.length > 0 && (
          <div className="space-y-2">
            <div className="flex items-center gap-2">
              <Sparkles className="h-3.5 w-3.5 text-muted-foreground" />
              <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider">
                Workspace Skills
              </span>
              <Badge variant="secondary" className="text-[10px]">{agent.skills.length}</Badge>
            </div>
            <div className="space-y-2">
              {agent.skills.map((skill) => (
                <div
                  key={skill.id}
                  className="flex items-center gap-3 rounded-lg border px-4 py-3"
                >
                  <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-muted">
                    <FileText className="h-4 w-4 text-muted-foreground" />
                  </div>
                  <div className="min-w-0 flex-1">
                    <div className="text-sm font-medium">{skill.name}</div>
                    {skill.description && (
                      <div className="text-xs text-muted-foreground truncate">
                        {skill.description}
                      </div>
                    )}
                  </div>
                  <Tooltip>
                    <TooltipTrigger
                      render={
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          onClick={() => handleRemove(skill.id)}
                          disabled={saving}
                          className="text-muted-foreground hover:text-destructive"
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </Button>
                      }
                    />
                    <TooltipContent>Remove from agent</TooltipContent>
                  </Tooltip>
                </div>
              ))}
            </div>
          </div>
        )}

        {/* Local Runtime Skills Section */}
        {localSkills.length > 0 && (
          <div className="space-y-2">
            <div className="flex items-center gap-2">
              <HardDrive className="h-3.5 w-3.5 text-muted-foreground" />
              <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider">
                Local Runtime Skills
              </span>
              <Badge variant="outline" className="text-[10px]">{localSkills.length}</Badge>
            </div>
            <div className="space-y-2">
              {localSkills.map((skill) => (
                <LocalRuntimeSkillItem key={skill.key} skill={skill} />
              ))}
            </div>
          </div>
        )}

        {/* Empty state when no skills at all */}
        {agent.skills.length === 0 && localSkills.length === 0 && (
          <div className="flex flex-col items-center justify-center rounded-lg border border-dashed py-12">
            <FileText className="h-8 w-8 text-muted-foreground/40" />
            <p className="mt-3 text-sm text-muted-foreground">No skills assigned</p>
            <p className="mt-1 text-xs text-muted-foreground text-center max-w-[300px]">
              {localSkillsSupported === false 
                ? "This runtime does not expose local skills. Add workspace skills to enhance this agent's capabilities."
                : "Add workspace skills to share team knowledge with this agent. Local runtime skills will appear here automatically when available."}
            </p>
            {availableSkills.length > 0 && (
              <Button
                onClick={() => setShowPicker(true)}
                size="xs"
                className="mt-3"
                disabled={saving}
              >
                <Plus className="h-3 w-3" />
                Add Skill
              </Button>
            )}
          </div>
        )}
      </div>

      {/* Skill Picker Dialog */}
      {showPicker && (
        <Dialog open onOpenChange={(v) => { if (!v) setShowPicker(false); }}>
          <DialogContent className="max-w-md">
            <DialogHeader>
              <DialogTitle className="text-sm">Add Skill</DialogTitle>
              <DialogDescription className="text-xs">
                Select a skill to assign to this agent.
              </DialogDescription>
            </DialogHeader>
            <div className="max-h-64 overflow-y-auto space-y-1">
              {availableSkills.map((skill) => (
                <button
                  key={skill.id}
                  onClick={() => handleAdd(skill.id)}
                  disabled={saving}
                  className="flex w-full items-center gap-3 rounded-md px-3 py-2.5 text-left text-sm transition-colors hover:bg-accent/50"
                >
                  <FileText className="h-4 w-4 shrink-0 text-muted-foreground" />
                  <div className="min-w-0 flex-1">
                    <div className="font-medium">{skill.name}</div>
                    {skill.description && (
                      <div className="text-xs text-muted-foreground truncate">
                        {skill.description}
                      </div>
                    )}
                  </div>
                </button>
              ))}
              {availableSkills.length === 0 && (
                <p className="py-6 text-center text-xs text-muted-foreground">
                  All workspace skills are already assigned.
                </p>
              )}
            </div>
            <DialogFooter>
              <Button variant="ghost" onClick={() => setShowPicker(false)}>
                Cancel
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}

    </div>
  );
}

// Local runtime skill item - read-only display since local skills are automatic
function LocalRuntimeSkillItem({
  skill,
}: {
  skill: RuntimeLocalSkillSummary;
}) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <div className="flex items-center gap-3 rounded-lg border border-dashed px-4 py-3 bg-muted/20">
            <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-muted">
              <HardDrive className="h-4 w-4 text-muted-foreground" />
            </div>
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-2">
                <span className="text-sm font-medium">{skill.name}</span>
                <Badge variant="outline" className="text-[10px]">Auto</Badge>
              </div>
              {skill.description && (
                <div className="text-xs text-muted-foreground truncate">
                  {skill.description}
                </div>
              )}
              <div className="mt-0.5 truncate font-mono text-[10px] text-muted-foreground">
                {skill.source_path}
              </div>
            </div>
            <Badge variant="secondary" className="text-[10px]">
              {skill.file_count} file{skill.file_count === 1 ? "" : "s"}
            </Badge>
          </div>
        }
      />
      <TooltipContent>
        <p className="max-w-xs">Local runtime skill from {skill.provider}</p>
        <p className="text-xs text-muted-foreground mt-1">Automatically available to this agent</p>
      </TooltipContent>
    </Tooltip>
  );
}
