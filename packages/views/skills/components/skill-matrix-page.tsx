"use client";

import { useState, useMemo } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowLeft,
  RefreshCw,
  ArrowRightLeft,
  Layers,
  Search,
  Check,
  X,
  Trash2,
  AlertTriangle,
  Info,
} from "lucide-react";
import type {
  SkillMatrixResponse,
  SkillMatrixSkill,
  SkillMatrixWorkspace,
} from "@multica/core/types";
import { api } from "@multica/core/api";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@multica/ui/components/ui/dialog";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@multica/ui/components/ui/tooltip";
import { Badge } from "@multica/ui/components/ui/badge";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { toast } from "sonner";

import { PageHeader } from "../../layout/page-header";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

interface SkillMatrixPageProps {
  onBack: () => void;
}

interface CellSelection {
  skillName: string;  // For display/identification
  skillId: string;     // Actual skill ID for API calls
  workspaceId: string;
  exists: boolean;
}

// ---------------------------------------------------------------------------
// Queries
// ---------------------------------------------------------------------------

const skillMatrixKeys = {
  all: ["skill-matrix"] as const,
  matrix: () => [...skillMatrixKeys.all, "matrix"] as const,
};

// ---------------------------------------------------------------------------
// Components
// ---------------------------------------------------------------------------

export function SkillMatrixPage({ onBack }: SkillMatrixPageProps) {
  const queryClient = useQueryClient();
  const [searchQuery, setSearchQuery] = useState("");
  const [selectedCells, setSelectedCells] = useState<CellSelection[]>([]);
  const [syncDialogOpen, setSyncDialogOpen] = useState(false);
  const [isProcessing, setIsProcessing] = useState(false);

  // Fetch matrix data
  const { data: matrixData, isLoading } = useQuery({
    queryKey: skillMatrixKeys.matrix(),
    queryFn: () => api.getSkillMatrix(),
    staleTime: 0,
  });

  // Filter skills based on search
  const filteredData = useMemo(() => {
    if (!matrixData) return null;
    if (!searchQuery.trim()) return matrixData;

    const query = searchQuery.toLowerCase();
    const skillIndices = matrixData.skills
      .map((s, i) => ({ skill: s, index: i }))
      .filter(({ skill }) =>
        skill.name.toLowerCase().includes(query) ||
        skill.description.toLowerCase().includes(query)
      );

    return {
      skills: skillIndices.map(({ skill }) => skill),
      workspaces: matrixData.workspaces,
      matrix: skillIndices.map(({ index }) => matrixData.matrix[index]),
    };
  }, [matrixData, searchQuery]);

  // Check if skill exists in workspace
  const hasSkillInWorkspace = (skillIdx: number, wsIdx: number) => {
    if (!filteredData) return false;
    return filteredData.matrix[skillIdx]?.[wsIdx] ?? false;
  };

  // Find actual skill ID for a skill name in a specific workspace
  // Use the skill_lookup map from the API response
  const findSkillIdForWorkspace = (skillName: string, wsId: string): string | null => {
    if (!matrixData) return null;
    return matrixData.skill_lookup?.[skillName]?.[wsId] ?? null;
  };

  // Check if cell is selected
  const isCellSelected = (skillName: string, wsId: string) => {
    return selectedCells.some((c) => c.skillName === skillName && c.workspaceId === wsId);
  };

  // Toggle cell selection
  const toggleCell = (skillName: string, wsId: string, exists: boolean) => {
    // Find the actual skill ID for this workspace
    const skillId = exists 
      ? findSkillIdForWorkspace(skillName, wsId)  // For delete: get the skill ID in this workspace
      : matrixData?.skills.find((s) => s.name === skillName)?.id ?? null;  // For sync: get any source skill
    
    if (!skillId) return;
    
    setSelectedCells((prev) => {
      const existing = prev.find((c) => c.skillName === skillName && c.workspaceId === wsId);
      if (existing) {
        return prev.filter((c) => !(c.skillName === skillName && c.workspaceId === wsId));
      }
      return [...prev, { skillName, skillId, workspaceId: wsId, exists }];
    });
  };

  // Get selections by operation type
  const syncSelections = selectedCells.filter((c) => !c.exists);
  const deleteSelections = selectedCells.filter((c) => c.exists);

  // Get unique skills for each operation
  const skillsToSync = useMemo(() => {
    if (!matrixData) return [];
    const skillIds = [...new Set(syncSelections.map((c) => c.skillId))];
    return skillIds
      .map((id) => matrixData.skills.find((s) => s.id === id))
      .filter(Boolean) as SkillMatrixSkill[];
  }, [syncSelections, matrixData]);

  const skillsToDelete = useMemo(() => {
    if (!matrixData) return [];
    const skillIds = [...new Set(deleteSelections.map((c) => c.skillId))];
    return skillIds
      .map((id) => matrixData.skills.find((s) => s.id === id))
      .filter(Boolean) as SkillMatrixSkill[];
  }, [deleteSelections, matrixData]);

  // Group sync selections by skill
  const syncBySkill = useMemo(() => {
    const grouped: Record<string, string[]> = {};
    syncSelections.forEach((cell) => {
      if (!grouped[cell.skillId]) grouped[cell.skillId] = [];
      grouped[cell.skillId].push(cell.workspaceId);
    });
    return grouped;
  }, [syncSelections]);

  // Group delete selections by skill name (for dialog display)
  const deleteBySkill = useMemo(() => {
    const grouped: Record<string, string[]> = {};
    deleteSelections.forEach((cell) => {
      if (!grouped[cell.skillName]) grouped[cell.skillName] = [];
      grouped[cell.skillName].push(cell.workspaceId);
    });
    return grouped;
  }, [deleteSelections]);

  // Check for conflicting operations: skill selected for both sync and delete
  const conflictingSkills = useMemo(() => {
    const conflicts: { skillName: string; syncWorkspaces: string[]; deleteWorkspaces: string[] }[] = [];
    
    // Get skill names for sync selections
    const syncSkillNames = new Map<string, string[]>(); // skillName -> workspaces
    syncSelections.forEach((cell) => {
      const existing = syncSkillNames.get(cell.skillName) || [];
      existing.push(cell.workspaceId);
      syncSkillNames.set(cell.skillName, existing);
    });
    
    // Check if any delete selection has the same skill name
    deleteSelections.forEach((cell) => {
      const syncWorkspaces = syncSkillNames.get(cell.skillName);
      if (syncWorkspaces) {
        // This skill is being both synced and deleted
        const existingConflict = conflicts.find((c) => c.skillName === cell.skillName);
        if (existingConflict) {
          if (!existingConflict.deleteWorkspaces.includes(cell.workspaceId)) {
            existingConflict.deleteWorkspaces.push(cell.workspaceId);
          }
        } else {
          conflicts.push({
            skillName: cell.skillName,
            syncWorkspaces: [...syncWorkspaces],
            deleteWorkspaces: [cell.workspaceId],
          });
        }
      }
    });
    
    return conflicts;
  }, [syncSelections, deleteSelections]);

  // Helper to get workspace name by ID
  const getWorkspaceName = (wsId: string) => {
    return matrixData?.workspaces.find((w) => w.id === wsId)?.name ?? wsId;
  };

  // Handle apply changes - both sync and delete in one operation
  const handleApplyChanges = async () => {
    if (selectedCells.length === 0 || !matrixData) return;

    setIsProcessing(true);
    let syncSuccess = 0;
    let syncFailed = 0;
    let deleteSuccess = 0;
    let deleteFailed = 0;

    // First handle sync (add skills)
    for (const [skillId, workspaceIds] of Object.entries(syncBySkill)) {
      try {
        const result = await api.syncSkillToWorkspaces(skillId, {
          target_workspace_ids: workspaceIds,
          overwrite_existing: false,
        });
        syncSuccess += result.success_count;
        syncFailed += result.failed_count;
      } catch {
        syncFailed += workspaceIds.length;
      }
    }

    // Then handle delete (remove skills)
    for (const [skillName, workspaceIds] of Object.entries(deleteBySkill)) {
      // Find source skill ID (any instance of this skill to get the name)
      const sourceSkillId = deleteSelections.find((s) => s.skillName === skillName)?.skillId;
      if (!sourceSkillId) continue;
      
      try {
        const result = await api.deleteSkillFromWorkspaces(sourceSkillId, {
          target_workspace_ids: workspaceIds,
        });
        deleteSuccess += result.deleted_count;
        deleteFailed += result.failed_count;
      } catch {
        deleteFailed += workspaceIds.length;
      }
    }

    // Show combined toast messages
    if (syncSuccess > 0) {
      toast.success(`Synced ${syncSuccess} skills to workspaces`);
    }
    if (deleteSuccess > 0) {
      toast.success(`Deleted ${deleteSuccess} skills from workspaces`);
    }
    if (syncFailed > 0 || deleteFailed > 0) {
      toast.error(`Failed: ${syncFailed} sync, ${deleteFailed} delete`);
    }

    queryClient.invalidateQueries({ queryKey: skillMatrixKeys.all });
    setSelectedCells([]);
    setSyncDialogOpen(false);
    setIsProcessing(false);
  };

  // Clear all selections
  const clearSelection = () => {
    setSelectedCells([]);
  };

  return (
    <div className="flex flex-col h-full">
      <PageHeader
        title="Skill Matrix"
        description="Click cells to sync (add) or delete skills across workspaces"
        actions={
          <div className="flex items-center gap-2">
            <Button variant="outline" size="sm" onClick={onBack}>
              <ArrowLeft className="h-4 w-4 mr-2" />
              Back
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={() => queryClient.invalidateQueries({ queryKey: skillMatrixKeys.all })}
            >
              <RefreshCw className="h-4 w-4 mr-2" />
              Refresh
            </Button>
          </div>
        }
      />

      {/* Toolbar */}
      <div className="flex items-center justify-between px-6 py-3 border-b gap-4">
        <div className="flex items-center gap-3 flex-1 min-w-0">
          <div className="relative flex-shrink-0">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground pointer-events-none" />
            <Input
              placeholder="Search skills..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="pl-9 w-[280px] h-9"
            />
          </div>
          {selectedCells.length > 0 && (
            <div className="flex items-center gap-2 flex-shrink-0">
              <Badge variant="secondary" className="h-6 px-2">
                {selectedCells.length} selected
              </Badge>
              {syncSelections.length > 0 && (
                <Badge variant="outline" className="text-primary border-primary h-6 px-2">
                  {syncSelections.length} to sync
                </Badge>
              )}
              {deleteSelections.length > 0 && (
                <Badge variant="outline" className="text-destructive border-destructive h-6 px-2">
                  {deleteSelections.length} to delete
                </Badge>
              )}
              <Button variant="ghost" size="icon" className="h-7 w-7" onClick={clearSelection}>
                <X className="h-4 w-4" />
              </Button>
            </div>
          )}
        </div>
        <div className="flex items-center gap-2 flex-shrink-0">
          {selectedCells.length > 0 && (
            <Button
              variant="default"
              size="sm"
              onClick={() => setSyncDialogOpen(true)}
              className="h-9"
            >
              <ArrowRightLeft className="h-4 w-4 mr-2" />
              Apply Changes ({selectedCells.length})
            </Button>
          )}
        </div>
      </div>

      {/* Legend */}
      <div className="flex items-center justify-center gap-8 px-6 py-3 border-b bg-muted/50 text-xs text-muted-foreground">
        <div className="flex items-center gap-2">
          <div className="w-4 h-4 rounded bg-green-500/10 flex items-center justify-center">
            <Check className="w-3 h-3 text-green-600" />
          </div>
          <span>Exists (click to delete)</span>
        </div>
        <div className="flex items-center gap-2">
          <div className="w-4 h-4 rounded bg-primary/10 ring-1 ring-primary flex items-center justify-center">
            <ArrowRightLeft className="w-3 h-3 text-primary" />
          </div>
          <span>Selected for sync</span>
        </div>
        <div className="flex items-center gap-2">
          <div className="w-4 h-4 rounded bg-destructive/10 ring-1 ring-destructive flex items-center justify-center">
            <Trash2 className="w-3 h-3 text-destructive" />
          </div>
          <span>Selected for delete</span>
        </div>
        <div className="flex items-center gap-2">
          <div className="w-4 h-4 border border-dashed border-muted-foreground/40 rounded" />
          <span>Not present (click to sync)</span>
        </div>
      </div>

      {/* Matrix */}
      <div className="flex-1 overflow-auto p-6">
        {isLoading ? (
          <div className="space-y-4">
            <Skeleton className="h-8 w-full" />
            <Skeleton className="h-96 w-full" />
          </div>
        ) : filteredData && filteredData.skills.length > 0 ? (
          <div className="border rounded-lg overflow-hidden">
            <table className="w-full text-sm border-collapse">
              <thead className="bg-muted/50 sticky top-0 z-20">
                <tr>
                  <th className="px-4 py-3 text-left font-medium text-muted-foreground sticky left-0 bg-muted/50 z-30 border-r w-[280px] min-w-[280px]">
                    Skill
                  </th>
                  {filteredData.workspaces.map((ws) => (
                    <th
                      key={ws.id}
                      className="px-2 py-3 text-center font-medium text-muted-foreground w-[72px] min-w-[72px]"
                    >
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <div className="flex flex-col items-center justify-center gap-1 cursor-pointer">
                            <span className="truncate max-w-[60px] text-xs leading-none">{ws.name}</span>
                            <Badge variant="outline" className="text-[10px] leading-none px-1.5 py-0 h-4">
                              {ws.skill_count}
                            </Badge>
                          </div>
                        </TooltipTrigger>
                        <TooltipContent side="top">
                          <p className="font-medium">{ws.name}</p>
                          <p className="text-muted-foreground text-xs">{ws.skill_count} skills</p>
                        </TooltipContent>
                      </Tooltip>
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {filteredData.skills.map((skill, skillIdx) => (
                  <tr key={skill.id} className="hover:bg-muted/30 transition-colors">
                    <td className="px-4 py-3 sticky left-0 bg-background z-20 border-r w-[280px]">
                      <div className="flex flex-col justify-center">
                        <p className="font-medium text-sm leading-5">{skill.name}</p>
                        {skill.description && (
                          <p className="text-xs text-muted-foreground line-clamp-1 mt-0.5 leading-4">
                            {skill.description}
                          </p>
                        )}
                      </div>
                    </td>
                    {filteredData.workspaces.map((ws, wsIdx) => {
                      const hasSkill = hasSkillInWorkspace(skillIdx, wsIdx);
                      const isSelected = isCellSelected(skill.name, ws.id);
                      const selection = selectedCells.find((c) => c.skillName === skill.name && c.workspaceId === ws.id);
                      const isDelete = selection?.exists ?? false;
                      
                      return (
                        <td key={ws.id} className="px-2 py-2 text-center w-[72px]">
                          <button
                            onClick={() => toggleCell(skill.name, ws.id, hasSkill)}
                            className={`
                              w-8 h-8 rounded transition-all flex items-center justify-center mx-auto
                              ${hasSkill 
                                ? isSelected
                                  ? "bg-destructive/10 ring-2 ring-destructive"
                                  : "bg-green-500/10 hover:bg-destructive/10"
                                : isSelected
                                  ? "bg-primary/10 ring-2 ring-primary"
                                  : "hover:bg-muted border border-dashed border-muted-foreground/30"
                              }
                            `}
                            title={hasSkill 
                              ? isSelected
                                ? `Cancel deletion of ${skill.name} from ${ws.name}`
                                : `Delete ${skill.name} from ${ws.name}`
                              : isSelected 
                                ? `Cancel sync of ${skill.name} to ${ws.name}`
                                : `Sync ${skill.name} to ${ws.name}`
                            }
                          >
                            {hasSkill && !isSelected && <Check className="w-4 h-4 text-green-600" />}
                            {hasSkill && isSelected && <Trash2 className="w-4 h-4 text-destructive" />}
                            {!hasSkill && isSelected && <ArrowRightLeft className="w-4 h-4 text-primary" />}
                          </button>
                        </td>
                      );
                    })}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <div className="flex flex-col items-center justify-center h-64 text-muted-foreground">
            <Layers className="h-12 w-12 mb-4 opacity-50" />
            <p className="text-lg font-medium">No skills found</p>
            <p className="text-sm mt-1">
              {searchQuery ? "Try a different search term" : "Create your first skill to get started"}
            </p>
          </div>
        )}
      </div>

      {/* Apply Changes Dialog */}
      <Dialog open={syncDialogOpen} onOpenChange={setSyncDialogOpen}>
        <DialogContent className="max-w-md">
          <DialogHeader className="gap-2">
            <DialogTitle className="text-base font-semibold">
              Apply Changes ({selectedCells.length})
            </DialogTitle>
            <DialogDescription className="text-sm">
              Review the operations below before confirming.
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4 py-4">
            {/* Info about execution order */}
            {conflictingSkills.length > 0 && (
              <div className="rounded-md bg-muted px-3 py-2 text-sm">
                <div className="flex items-center gap-2 text-muted-foreground">
                  <Info className="h-4 w-4" />
                  <span>Execution order: copy first, then delete</span>
                </div>
              </div>
            )}

            {/* Sync section */}
            {syncSelections.length > 0 && (
              <div className="space-y-2">
                <div className="flex items-center gap-2 text-sm font-medium">
                  <ArrowRightLeft className="h-4 w-4 text-primary" />
                  <span>Copy to workspaces</span>
                  <span className="text-muted-foreground">({syncSelections.length})</span>
                </div>
                <div className="rounded-md border bg-muted/50 p-3 space-y-1.5 max-h-32 overflow-auto">
                  {Object.entries(syncBySkill).map(([skillId, wsIds]) => {
                    const skill = matrixData?.skills.find((s) => s.id === skillId);
                    if (!skill) return null;
                    const sourceWsName = getWorkspaceName(skill.workspace_id);
                    return (
                      <div key={skillId} className="flex items-center justify-between text-sm">
                        <span className="font-medium">{skill.name}</span>
                        <span className="text-xs text-muted-foreground">
                          {sourceWsName} → {wsIds.map(getWorkspaceName).join(", ")}
                        </span>
                      </div>
                    );
                  })}
                </div>
              </div>
            )}

            {/* Delete section */}
            {deleteSelections.length > 0 && (
              <div className="space-y-2">
                <div className="flex items-center gap-2 text-sm font-medium">
                  <Trash2 className="h-4 w-4 text-destructive" />
                  <span>Delete from workspaces</span>
                  <span className="text-muted-foreground">({deleteSelections.length})</span>
                </div>
                <div className="rounded-md border border-destructive/20 bg-muted/50 p-3 space-y-1.5 max-h-32 overflow-auto">
                  {Object.entries(deleteBySkill).map(([skillName, wsIds]) => (
                    <div key={skillName} className="flex items-center justify-between text-sm">
                      <span className="font-medium">{skillName}</span>
                      <span className="text-xs text-muted-foreground">
                        → {wsIds.map(getWorkspaceName).join(", ")}
                      </span>
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>

          <DialogFooter className="gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={() => setSyncDialogOpen(false)}
              disabled={isProcessing}
            >
              Cancel
            </Button>
            <Button
              size="sm"
              onClick={handleApplyChanges}
              disabled={isProcessing}
              variant={deleteSelections.length > 0 ? "destructive" : "default"}
            >
              {isProcessing ? (
                <RefreshCw className="h-4 w-4 mr-2 animate-spin" />
              ) : (
                <>
                  {deleteSelections.length > 0 ? (
                    <Trash2 className="h-4 w-4 mr-2" />
                  ) : (
                    <ArrowRightLeft className="h-4 w-4 mr-2" />
                  )}
                  Apply
                </>
              )}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

export default SkillMatrixPage;
