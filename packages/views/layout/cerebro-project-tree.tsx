"use client";

// CEREBRO-PATCH(project-tree): FIR-1614 the nested Projects tree in the sidebar.
// Extracted from app-sidebar into a cerebro- file so the substantial design +
// drag-and-drop logic lives in the cerebro zone (app-sidebar just mounts it).
//
// Design: built on the shadcn nested-sidebar primitives (SidebarMenuSub) so the
// vertical guide line is the sub-list's own `border-l` — it sits to the LEFT of
// each indented level and never crosses a fold-out chevron. Every row reads as a
// folder: [chevron][folder icon][name]. Up to 4 nesting levels, and each sibling
// group is drag-sortable (cross-parent moves stay in the row's "Move" menu).

import * as React from "react";
import { useEffect, useRef, useState } from "react";
import { ChevronRight, Folder, FolderOpen } from "lucide-react";
import {
  DndContext,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import { SortableContext, verticalListSortingStrategy, useSortable, arrayMove } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";

import type { ProjectTreeItem } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useReorderProjects } from "@multica/core/projects/nesting";
import {
  SidebarMenuItem,
  SidebarMenuButton,
  SidebarMenuSub,
  SidebarMenuSubItem,
  SidebarMenuSubButton,
} from "@multica/ui/components/ui/sidebar";
import { cn } from "@multica/ui/lib/utils";

import { AppLink, useNavigation } from "../navigation";
import { ProjectSprintsNav } from "./cerebro-project-sprints-nav";
import { ProjectRowMenu } from "./cerebro-project-row-menu";

interface Props {
  projects: ProjectTreeItem[];
  expandedMap: Record<string, boolean>;
  onToggleExpand: (projectId: string, expanded: boolean) => void;
  onNavigate: () => void;
}

function projectTreeContainsPath(
  project: ProjectTreeItem,
  pathname: string,
  projectHref: (id: string) => string,
): boolean {
  if (pathname === projectHref(project.id)) return true;
  return (project.children ?? []).some((child) => projectTreeContainsPath(child, pathname, projectHref));
}

// A project's own emoji icon if it has one, else a clean lucide folder (open
// when the node is expanded). Keeps every row looking like a folder.
function ProjectRowIcon({ item, isOpen }: { item: ProjectTreeItem; isOpen: boolean }) {
  if (item.icon) {
    return (
      <span aria-hidden="true" className="flex size-4 shrink-0 items-center justify-center text-xs leading-none">
        {item.icon}
      </span>
    );
  }
  return isOpen ? (
    <FolderOpen className="size-4 shrink-0 text-muted-foreground" />
  ) : (
    <Folder className="size-4 shrink-0 text-muted-foreground" />
  );
}

// Reorder one sibling group inside the tree. parentId null targets the roots.
function reorderSiblings(
  tree: ProjectTreeItem[],
  parentId: string | null,
  activeId: string,
  overId: string,
): { tree: ProjectTreeItem[]; orderedIds: string[] } | null {
  const apply = (siblings: ProjectTreeItem[]): ProjectTreeItem[] | null => {
    const oldIndex = siblings.findIndex((s) => s.id === activeId);
    const newIndex = siblings.findIndex((s) => s.id === overId);
    if (oldIndex === -1 || newIndex === -1) return null;
    return arrayMove(siblings, oldIndex, newIndex);
  };

  if (parentId === null) {
    const next = apply(tree);
    if (!next) return null;
    return { tree: next, orderedIds: next.map((s) => s.id) };
  }

  let ordered: string[] | null = null;
  const walk = (nodes: ProjectTreeItem[]): ProjectTreeItem[] =>
    nodes.map((node) => {
      if (node.id === parentId && node.children) {
        const next = apply(node.children);
        if (next) {
          ordered = next.map((s) => s.id);
          return { ...node, children: next };
        }
      }
      return node.children ? { ...node, children: walk(node.children) } : node;
    });

  const nextTree = walk(tree);
  if (!ordered) return null;
  return { tree: nextTree, orderedIds: ordered };
}

function ProjectRow({
  item,
  parentId,
  isSub,
  isActive,
  hasChildren,
  hasActiveChild,
  isExpanded,
  href,
  onToggleExpand,
  onNavigate,
}: {
  item: ProjectTreeItem;
  parentId: string | null;
  isSub: boolean;
  isActive: boolean;
  hasChildren: boolean;
  hasActiveChild: boolean;
  isExpanded: boolean;
  href: string;
  onToggleExpand: (projectId: string, expanded: boolean) => void;
  onNavigate: () => void;
}) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: item.id,
    data: { parentId },
  });
  const wasDragged = useRef(false);
  useEffect(() => {
    if (isDragging) wasDragged.current = true;
  }, [isDragging]);

  const style = { transform: CSS.Transform.toString(transform), transition };

  const chevron = hasChildren ? (
    <span
      role="button"
      tabIndex={0}
      aria-label={isExpanded ? "Collapse" : "Expand"}
      className="flex size-4 shrink-0 items-center justify-center rounded-sm text-muted-foreground hover:text-foreground"
      onClick={(event) => {
        event.preventDefault();
        event.stopPropagation();
        onToggleExpand(item.id, !isExpanded);
      }}
      onKeyDown={(event) => {
        if (event.key !== "Enter" && event.key !== " ") return;
        event.preventDefault();
        event.stopPropagation();
        onToggleExpand(item.id, !isExpanded);
      }}
    >
      <ChevronRight className={cn("!size-3.5 transition-transform", isExpanded && "rotate-90")} />
    </span>
  ) : (
    <span className="size-4 shrink-0" />
  );

  const inner = (
    <>
      {chevron}
      <ProjectRowIcon item={item} isOpen={hasChildren && isExpanded} />
      <span className="truncate">{item.title}</span>
    </>
  );

  const buttonClass = cn(
    "h-7 gap-1.5 pr-7 text-muted-foreground hover:not-data-active:bg-sidebar-accent/70 data-active:bg-sidebar-accent data-active:text-sidebar-accent-foreground",
    hasActiveChild && "bg-sidebar-accent/45 text-sidebar-accent-foreground",
    isDragging && "pointer-events-none",
  );

  const onClick = (event: React.MouseEvent) => {
    if (wasDragged.current) {
      wasDragged.current = false;
      event.preventDefault();
      return;
    }
    onNavigate();
  };

  return (
    <div ref={setNodeRef} className={cn("relative", isDragging && "z-10 opacity-60")} style={style} {...attributes} {...listeners}>
      {isSub ? (
        <SidebarMenuSubButton isActive={isActive} render={<AppLink href={href} draggable={false} />} onClick={onClick} className={buttonClass}>
          {inner}
        </SidebarMenuSubButton>
      ) : (
        <SidebarMenuButton isActive={isActive} render={<AppLink href={href} draggable={false} />} onClick={onClick} className={buttonClass}>
          {inner}
        </SidebarMenuButton>
      )}
      {/* CEREBRO-PATCH(project-row-menu): FIR-1614 3-dot actions menu (replaces issue-count). */}
      <ProjectRowMenu project={item} />
    </div>
  );
}

export function CerebroProjectTree({ projects, expandedMap, onToggleExpand, onNavigate }: Props) {
  const p = useWorkspacePaths();
  const { pathname } = useNavigation();
  const wsId = useWorkspaceId();
  const reorder = useReorderProjects(wsId);
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 5 } }));

  const [localProjects, setLocalProjects] = useState(projects);
  const draggingRef = useRef(false);
  useEffect(() => {
    if (!draggingRef.current) setLocalProjects(projects);
  }, [projects]);

  const handleDragEnd = (event: DragEndEvent) => {
    draggingRef.current = false;
    const { active, over } = event;
    if (!over || active.id === over.id) return;
    const parentId = (active.data.current?.parentId ?? null) as string | null;
    const overParent = (over.data.current?.parentId ?? null) as string | null;
    if (parentId !== overParent) return; // reorder within a sibling group only
    const result = reorderSiblings(localProjects, parentId, String(active.id), String(over.id));
    if (!result) return;
    setLocalProjects(result.tree);
    reorder.mutate({ parentProjectId: parentId, orderedIds: result.orderedIds });
  };

  const renderNode = (item: ProjectTreeItem, parentId: string | null, level: number): React.ReactNode => {
    const href = p.projectDetail(item.id);
    const isActive = pathname === href;
    const children = item.children ?? [];
    const hasChildren = children.length > 0;
    const isExpanded = expandedMap[item.id] ?? true;
    const hasActiveChild = children.some((child) => projectTreeContainsPath(child, pathname, p.projectDetail));
    const isSub = level > 0;

    const row = (
      <ProjectRow
        item={item}
        parentId={parentId}
        isSub={isSub}
        isActive={isActive}
        hasChildren={hasChildren}
        hasActiveChild={hasActiveChild}
        isExpanded={isExpanded}
        href={href}
        onToggleExpand={onToggleExpand}
        onNavigate={onNavigate}
      />
    );

    // The children of this node — a sub-list whose own border-l draws the guide.
    const childrenBlock =
      hasChildren && isExpanded ? (
        <SidebarMenuSub className="mr-0">
          <SortableContext items={children.map((c) => c.id)} strategy={verticalListSortingStrategy}>
            {children.map((child) => renderNode(child, item.id, level + 1))}
          </SortableContext>
        </SidebarMenuSub>
      ) : null;

    if (isSub) {
      return (
        <SidebarMenuSubItem key={item.id}>
          {row}
          {childrenBlock}
          {/* CEREBRO-PATCH(sidebar-sprints-nav): TECH-3684 sprints under the project. */}
          <ProjectSprintsNav projectId={item.id} level={level} />
        </SidebarMenuSubItem>
      );
    }
    return (
      <SidebarMenuItem key={item.id}>
        {row}
        {childrenBlock}
        {/* CEREBRO-PATCH(sidebar-sprints-nav): TECH-3684 sprints under the project. */}
        <ProjectSprintsNav projectId={item.id} level={level} />
      </SidebarMenuItem>
    );
  };

  return (
    <DndContext
      sensors={sensors}
      collisionDetection={closestCenter}
      onDragStart={() => {
        draggingRef.current = true;
      }}
      onDragEnd={handleDragEnd}
    >
      <SortableContext items={localProjects.map((s) => s.id)} strategy={verticalListSortingStrategy}>
        {localProjects.map((project) => renderNode(project, null, 0))}
      </SortableContext>
    </DndContext>
  );
}
