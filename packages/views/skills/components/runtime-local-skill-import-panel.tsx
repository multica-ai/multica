"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { HardDrive, Download, AlertCircle } from "lucide-react";
import type { AgentRuntime, Skill } from "@multica/core/types";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  runtimeListOptions,
  runtimeLocalSkillsKeys,
  runtimeLocalSkillsOptions,
  resolveRuntimeLocalSkillImport,
} from "@multica/core/runtimes";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Badge } from "@multica/ui/components/ui/badge";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { toast } from "sonner";
import { RuntimeLocalSkillRow } from "./runtime-local-skill-row";

function runtimeLabel(runtime: AgentRuntime): string {
  return `${runtime.name} (${runtime.provider})`;
}

/**
 * Body of the "import local runtime skill into workspace" flow, extracted
 * from RuntimeLocalSkillImportDialog so it can be reused inside the unified
 * Add-Workspace-Skill dialog as a tab. Owns its own state, runtime/skill
 * picker, and Import button so the parent only needs to render it inside a
 * scroll/dialog container — no slot juggling.
 *
 * `active` lets the parent (e.g. a Tabs panel) tell the panel when it is
 * the visible tab; the panel uses that to seed defaults the first time it
 * opens, mirroring how the standalone dialog reacts to `open` going true.
 */
export function RuntimeLocalSkillImportPanel({
  active,
  onImported,
  initialRuntimeId,
  initialSkillKey,
  fixedRuntimeId,
}: {
  active: boolean;
  onImported?: (skill: Skill) => void;
  initialRuntimeId?: string | null;
  initialSkillKey?: string | null;
  fixedRuntimeId?: string | null;
}) {
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const userId = useAuthStore((s) => s.user?.id ?? null);
  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));
  // Only the runtime owner can browse + import its local skills (server-side
  // ACL enforces this), so listing other people's runtimes here just sets
  // the user up for a permission error after the fact. Match the Runtimes
  // page's "Mine" tab default and only show the caller's own local runtimes.
  const localRuntimes = useMemo(
    () =>
      runtimes.filter(
        (runtime) =>
          runtime.runtime_mode === "local" &&
          (userId == null || runtime.owner_id === userId),
      ),
    [runtimes, userId],
  );

  const [selectedRuntimeId, setSelectedRuntimeId] = useState<string>("");
  const [selectedSkillKey, setSelectedSkillKey] = useState<string>("");
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [importing, setImporting] = useState(false);

  useEffect(() => {
    if (!active) return;
    const preferredRuntimeId =
      fixedRuntimeId ?? initialRuntimeId ?? localRuntimes[0]?.id ?? "";
    setSelectedRuntimeId(preferredRuntimeId);
  }, [fixedRuntimeId, initialRuntimeId, localRuntimes, active]);

  const selectedRuntime = localRuntimes.find(
    (runtime) => runtime.id === selectedRuntimeId,
  );
  const canBrowseSkills = active && !!selectedRuntimeId && selectedRuntime?.status === "online";
  const skillsQuery = useQuery({
    ...runtimeLocalSkillsOptions(selectedRuntimeId || null),
    enabled: canBrowseSkills,
  });

  const runtimeSkills = skillsQuery.data?.skills ?? [];
  const selectedSkill = runtimeSkills.find((skill) => skill.key === selectedSkillKey);

  useEffect(() => {
    if (!active) return;
    const preferredSkill =
      (initialSkillKey
        ? runtimeSkills.find((skill) => skill.key === initialSkillKey)
        : null) ?? runtimeSkills[0];
    if (!preferredSkill) {
      setSelectedSkillKey("");
      setName("");
      setDescription("");
      return;
    }
    if (!runtimeSkills.some((skill) => skill.key === selectedSkillKey)) {
      setSelectedSkillKey(preferredSkill.key);
      setName(preferredSkill.name);
      setDescription(preferredSkill.description ?? "");
    }
  }, [initialSkillKey, active, runtimeSkills, selectedSkillKey]);

  useEffect(() => {
    if (!selectedSkill) return;
    setName(selectedSkill.name);
    setDescription(selectedSkill.description ?? "");
  }, [selectedSkillKey, selectedSkill]);

  const handleImport = async () => {
    if (!selectedRuntimeId || !selectedSkill) return;
    setImporting(true);
    try {
      const result = await resolveRuntimeLocalSkillImport(selectedRuntimeId, {
        skill_key: selectedSkill.key,
        name: name.trim() || undefined,
        description: description.trim() || undefined,
      });
      await Promise.all([
        qc.invalidateQueries({ queryKey: runtimeLocalSkillsKeys.forRuntime(selectedRuntimeId) }),
        qc.invalidateQueries({ queryKey: workspaceKeys.skills(wsId) }),
        qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) }),
      ]);
      toast.success("Skill imported");
      onImported?.(result.skill);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Failed to import skill");
    } finally {
      setImporting(false);
    }
  };

  const renderSkillContent = () => {
    if (localRuntimes.length === 0) {
      return (
        <div className="rounded-lg border border-dashed px-4 py-8 text-center">
          <p className="text-sm text-muted-foreground">No local runtimes available</p>
          <p className="mt-1 text-xs text-muted-foreground">
            Connect a local runtime to browse and import its local skills.
          </p>
        </div>
      );
    }
    if (!selectedRuntime) {
      return (
        <div className="rounded-lg border border-dashed px-4 py-8 text-center">
          <p className="text-sm text-muted-foreground">Choose a runtime to continue</p>
        </div>
      );
    }
    if (selectedRuntime.status !== "online") {
      return (
        <div className="flex items-start gap-2 rounded-md bg-warning/10 px-3 py-2 text-xs text-muted-foreground">
          <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0 text-warning" />
          Runtime must be online to browse local skills.
        </div>
      );
    }
    if (skillsQuery.isLoading) {
      return (
        <div className="space-y-2">
          {Array.from({ length: 3 }).map((_, index) => (
            <div key={index} className="rounded-lg border px-4 py-3">
              <Skeleton className="h-4 w-32" />
              <Skeleton className="mt-2 h-3 w-48" />
            </div>
          ))}
        </div>
      );
    }
    if (skillsQuery.error) {
      return (
        <div className="flex items-start gap-2 rounded-md bg-destructive/10 px-3 py-2 text-xs text-destructive">
          <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
          {skillsQuery.error instanceof Error
            ? skillsQuery.error.message
            : "Failed to load runtime local skills"}
        </div>
      );
    }
    if (!skillsQuery.data?.supported) {
      return (
        <div className="flex items-start gap-2 rounded-md bg-muted/50 px-3 py-2 text-xs text-muted-foreground">
          <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
          This runtime provider does not expose local skill inventory yet.
        </div>
      );
    }
    if (runtimeSkills.length === 0) {
      return (
        <div className="rounded-lg border border-dashed px-4 py-8 text-center">
          <p className="text-sm text-muted-foreground">No local skills found</p>
          <p className="mt-1 text-xs text-muted-foreground">
            This runtime does not have any discoverable local skills yet.
          </p>
        </div>
      );
    }
    return (
      <div className="space-y-4">
        <div className="space-y-2">
          {runtimeSkills.map((skill) => (
            <RuntimeLocalSkillRow
              key={skill.key}
              skill={skill}
              selected={selectedSkillKey === skill.key}
              onSelect={() => setSelectedSkillKey(skill.key)}
            />
          ))}
        </div>

        {selectedSkill && (
          <div className="space-y-3 rounded-lg border bg-muted/20 p-4">
            <div>
              <Label className="text-xs text-muted-foreground">Workspace skill name</Label>
              <Input
                value={name}
                onChange={(event) => setName(event.target.value)}
                className="mt-1"
              />
            </div>
            <div>
              <Label className="text-xs text-muted-foreground">Description</Label>
              <Input
                value={description}
                onChange={(event) => setDescription(event.target.value)}
                className="mt-1"
                placeholder="Optional description override"
              />
            </div>
          </div>
        )}
      </div>
    );
  };

  const canImport = !!selectedRuntime
    && selectedRuntime.status === "online"
    && !!selectedSkill
    && !!name.trim()
    && !importing;

  return (
    <div className="space-y-4">
      {!fixedRuntimeId && (
        <div className="space-y-1.5">
          <Label className="text-xs text-muted-foreground">Runtime</Label>
          <Select value={selectedRuntimeId} onValueChange={(value) => value && setSelectedRuntimeId(value)}>
            <SelectTrigger className="w-full">
              <SelectValue placeholder="Select a local runtime">
                {selectedRuntime ? runtimeLabel(selectedRuntime) : null}
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              {localRuntimes.map((runtime) => (
                <SelectItem key={runtime.id} value={runtime.id}>
                  {runtimeLabel(runtime)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      )}

      {selectedRuntime && (
        <div className="flex items-center gap-2 rounded-lg border bg-muted/20 px-3 py-2 text-xs text-muted-foreground">
          <HardDrive className="h-3.5 w-3.5 shrink-0" />
          <span className="min-w-0 flex-1 truncate">{runtimeLabel(selectedRuntime)}</span>
          <Badge variant={selectedRuntime.status === "online" ? "secondary" : "outline"}>
            {selectedRuntime.status}
          </Badge>
        </div>
      )}

      {renderSkillContent()}

      <p className="text-xs text-muted-foreground">
        Symlinks, unreadable files, oversized files, and very large bundles are ignored during import.
      </p>

      <div className="flex justify-end">
        <Button onClick={handleImport} disabled={!canImport}>
          {importing ? (
            "Importing..."
          ) : (
            <>
              <Download className="h-3 w-3" />
              Import to Workspace
            </>
          )}
        </Button>
      </div>
    </div>
  );
}
