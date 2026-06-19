"use client";

/* eslint-disable i18next/no-literal-string -- cerebro-only sidebar UI: cerebro
   features stay English and self-contained rather than adding keys to the
   upstream (packages/views/locales) translation glossary. */

// CEREBRO-PATCH(project-row-menu): FIR-1614 a 3-dot actions menu on every project
// row in the sidebar, replacing the old issue-count badge. Actions: create a
// sub-project, rename, and move (re-parent). Lives in views so it can reuse the
// nested-projects mutations and the shared dropdown/dialog primitives; kept in a
// dedicated cerebro- file so upstream merges never conflict on app-sidebar.

import { useMemo, useState } from "react";
import { FolderPlus, MoreHorizontal, Pencil, FolderInput } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";

import { useWorkspaceId } from "@multica/core/hooks";
import { useCreateProject, useUpdateProject } from "@multica/core/projects/mutations";
import { projectTreeOptions, useSetProjectParent } from "@multica/core/projects/nesting";
import type { ProjectTreeItem } from "@multica/core/types";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";

interface Props {
  project: ProjectTreeItem;
}

/** Collect this project's id plus every descendant id — invalid move targets. */
function collectSelfAndDescendants(node: ProjectTreeItem, acc: Set<string>) {
  acc.add(node.id);
  for (const child of node.children ?? []) collectSelfAndDescendants(child, acc);
}

/** Flatten a project tree into a depth-tagged list for the move picker. */
function flattenWithDepth(
  nodes: ProjectTreeItem[],
  depth = 0,
  acc: Array<{ project: ProjectTreeItem; depth: number }> = [],
) {
  for (const node of [...nodes].sort((a, b) => a.title.localeCompare(b.title))) {
    acc.push({ project: node, depth });
    flattenWithDepth(node.children ?? [], depth + 1, acc);
  }
  return acc;
}

export function ProjectRowMenu({ project }: Props) {
  const wsId = useWorkspaceId();
  const createProject = useCreateProject();
  const updateProject = useUpdateProject();
  const setParent = useSetProjectParent(wsId);

  const [nameDialog, setNameDialog] = useState<"create-sub" | "rename" | null>(null);
  const [nameDraft, setNameDraft] = useState("");
  const [moveOpen, setMoveOpen] = useState(false);
  const [submitting, setSubmitting] = useState(false);

  const treeQuery = useQuery({ ...projectTreeOptions(wsId), enabled: moveOpen && !!wsId });

  const blockedIds = useMemo(() => {
    const set = new Set<string>();
    collectSelfAndDescendants(project, set);
    return set;
  }, [project]);

  const moveCandidates = useMemo(
    () =>
      flattenWithDepth(treeQuery.data ?? []).filter(({ project: p }) => !blockedIds.has(p.id)),
    [treeQuery.data, blockedIds],
  );

  const openCreateSub = () => {
    setNameDraft("");
    setNameDialog("create-sub");
  };
  const openRename = () => {
    setNameDraft(project.title);
    setNameDialog("rename");
  };

  const submitName = async () => {
    const name = nameDraft.trim();
    if (!name || submitting) return;
    setSubmitting(true);
    try {
      if (nameDialog === "rename") {
        await updateProject.mutateAsync({ id: project.id, title: name });
        toast.success("Project renamed");
      } else {
        const created = await createProject.mutateAsync({ title: name });
        await setParent.mutateAsync({ id: created.id, parentProjectId: project.id });
        toast.success(`Sub-project created under ${project.title}`);
      }
      setNameDialog(null);
    } catch {
      toast.error("Something went wrong — try again");
    } finally {
      setSubmitting(false);
    }
  };

  const move = async (parentProjectId: string | null) => {
    if (submitting) return;
    setSubmitting(true);
    try {
      await setParent.mutateAsync({ id: project.id, parentProjectId });
      toast.success(parentProjectId ? "Project moved" : "Moved to top level");
      setMoveOpen(false);
    } catch {
      toast.error("Could not move project — try again");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <button
              type="button"
              aria-label="Project actions"
              onClick={(e) => e.stopPropagation()}
              className={cn(
                "absolute top-1/2 right-1 flex size-5 -translate-y-1/2 items-center justify-center rounded-sm text-muted-foreground opacity-0 transition-opacity",
                "hover:bg-sidebar-accent hover:text-foreground focus-visible:opacity-100",
                "group-hover/menu-item:opacity-100 aria-expanded:opacity-100 data-[popup-open]:opacity-100",
              )}
            >
              <MoreHorizontal className="size-3.5" />
            </button>
          }
        />
        <DropdownMenuContent align="end" onClick={(e) => e.stopPropagation()}>
          <DropdownMenuItem onClick={openCreateSub}>
            <FolderPlus className="size-3.5" />
            New sub-project
          </DropdownMenuItem>
          <DropdownMenuItem onClick={openRename}>
            <Pencil className="size-3.5" />
            Rename
          </DropdownMenuItem>
          <DropdownMenuItem onClick={() => setMoveOpen(true)}>
            <FolderInput className="size-3.5" />
            Move
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      {/* Create sub-project / rename */}
      <Dialog open={nameDialog !== null} onOpenChange={(o) => !o && setNameDialog(null)}>
        <DialogContent className="sm:max-w-sm">
          <DialogHeader>
            <DialogTitle>
              {nameDialog === "rename" ? "Rename project" : `New sub-project in ${project.title}`}
            </DialogTitle>
          </DialogHeader>
          <Input
            autoFocus
            value={nameDraft}
            onChange={(e) => setNameDraft(e.target.value)}
            placeholder="Project name"
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                void submitName();
              }
            }}
          />
          <DialogFooter>
            <Button variant="outline" size="sm" onClick={() => setNameDialog(null)}>
              Cancel
            </Button>
            <Button size="sm" onClick={() => void submitName()} disabled={!nameDraft.trim() || submitting}>
              {nameDialog === "rename" ? "Save" : "Create"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Move (re-parent) */}
      <Dialog open={moveOpen} onOpenChange={(o) => !o && setMoveOpen(false)}>
        <DialogContent className="sm:max-w-sm">
          <DialogHeader>
            <DialogTitle>Move {project.title}</DialogTitle>
          </DialogHeader>
          <div className="max-h-72 overflow-y-auto rounded-md border">
            {project.parent_project_id && (
              <button
                type="button"
                onClick={() => void move(null)}
                disabled={submitting}
                className="flex w-full items-center px-3 py-2 text-left text-sm hover:bg-accent disabled:opacity-50"
              >
                Top level (no parent)
              </button>
            )}
            {moveCandidates.length === 0 ? (
              <p className="px-3 py-2 text-sm text-muted-foreground">No other projects available.</p>
            ) : (
              moveCandidates.map(({ project: candidate, depth }) => (
                <button
                  key={candidate.id}
                  type="button"
                  onClick={() => void move(candidate.id)}
                  disabled={submitting || candidate.id === project.parent_project_id}
                  style={{ paddingLeft: depth * 14 + 12 }}
                  className="flex w-full items-center truncate py-2 pr-3 text-left text-sm hover:bg-accent disabled:opacity-50"
                >
                  {candidate.title}
                  {candidate.id === project.parent_project_id && (
                    <span className="ml-2 text-xs text-muted-foreground">(current)</span>
                  )}
                </button>
              ))
            )}
          </div>
          <DialogFooter>
            <Button variant="outline" size="sm" onClick={() => setMoveOpen(false)}>
              Cancel
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
