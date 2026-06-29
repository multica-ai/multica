"use client";

// Settings → Collections (FIR-1590 → Collections). One place to see every
// folder across both surfaces — Documents/Notes (artifact) and
// Autopilots/Skills (entity) — as a tree of folder cards, manage per-folder
// access inline, and move folders between parents.
//
// Layout follows Jesper's mockup (FIR-1590 feedback #3 + #4): one tab per
// folder type, and within each tab a recursive tree where every folder is a
// card showing its breadcrumb path and an inline ACCESS PILL summarising who
// can reach it — "Everyone", a named group/member, or "Inherits: …" when the
// access cascades from a parent (the default for a sub-folder). The pill opens
// the full access editor.
//
// Re-parenting is DRAG-AND-DROP (FIR-1590 review): grab a folder by its handle
// and drop it onto another folder card to nest it inside, or onto the "top
// level" zone to un-nest it. Dropping onto itself or any of its own descendants
// is rejected (would orphan/cycle) — those targets light up as invalid. The
// move calls the same PUT endpoints behind the scenes, with the server cycle
// guard as the final backstop.
//
// The platform layer (web + desktop) spreads useCerebroCollectionsSettingsTabs()
// into <SettingsPage extraAccountTabs={...}> — the tab only appears when the
// cerebro_collections flag is on.
import * as React from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Layers,
  Folder as FolderIcon,
  FolderPlus,
  FilePlus,
  ChevronDown,
  GripVertical,
  ArrowUpToLine,
} from "lucide-react";
import {
  DndContext,
  DragOverlay,
  PointerSensor,
  useSensor,
  useSensors,
  useDraggable,
  useDroppable,
  pointerWithin,
  type DragStartEvent,
  type DragEndEvent,
} from "@dnd-kit/core";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  Tabs,
  TabsList,
  TabsTrigger,
  TabsContent,
} from "@multica/ui/components/ui/tabs";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { cn } from "@multica/ui/lib/utils";
import { useWorkspaceId } from "@multica/core/hooks";
import { projectTreeOptions } from "@multica/core/projects/nesting";
import type { ProjectTreeItem } from "@multica/core/types";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import {
  artifactCollectionFoldersOptions,
  entityCollectionFoldersOptions,
  folderGrantsOptions,
} from "../queries";
import { useCreateCollectionFolder, useCreateCollectionItem, useMoveCollectionFolder } from "../mutations";
import {
  buildFolderTree,
  collectSubtreeIds,
  flattenFolderTree,
  folderPath,
  type FolderNode,
} from "../tree";
import { summarizeAccess, type AccessTone } from "../access-summary";
import { useGranteeDirectory } from "./use-grantee-directory";
import { FolderAccessEditor } from "./folder-access-editor";
import { ProjectGrantsPanel } from "./project-grants-panel";
import type { CollectionFolder } from "../api";
import type { GranteeType, GrantSurface } from "../types";

// Mirrors @multica/views ExtraSettingsTab structurally. Defined locally so this
// entrypoint stays free of a views dependency (and the topo-sort coupling it
// brings), exactly like cerebro-tool-policy/views/workspace-settings-tab.
export interface ExtraSettingsTab {
  value: string;
  label: string;
  icon: React.ComponentType<{ className?: string }>;
  content: React.ReactNode;
  wide?: boolean;
}

type LabelFor = (type: GranteeType, id: string | null) => string;

// The four folder groups, one per tab. entityKind/artifactKind is the kind the
// respective backend keys folders by.
const GROUPS: {
  group: string;
  surface: GrantSurface;
  artifactKind?: "document" | "note";
  entityKind?: "skill" | "autopilot";
}[] = [
  { group: "Documents", surface: "artifact", artifactKind: "document" },
  { group: "Notes", surface: "artifact", artifactKind: "note" },
  { group: "Autopilots", surface: "entity", entityKind: "autopilot" },
  { group: "Skills", surface: "entity", entityKind: "skill" },
];

const PROJECTS_TAB = "Projects";

const TONE_DOT: Record<AccessTone, string> = {
  everyone: "bg-emerald-500",
  restricted: "bg-blue-500",
  inherited: "bg-amber-500",
  none: "bg-muted-foreground/40",
};

// The inline access pill on a folder card. Reads the folder's EFFECTIVE grants
// (direct + inherited) and renders a one-line summary with a colored dot;
// clicking opens the full access editor. Only fetches while its tab is active.
function AccessPill({
  folder,
  active,
  labelFor,
  onManage,
}: {
  folder: CollectionFolder;
  active: boolean;
  labelFor: LabelFor;
  onManage: (folder: CollectionFolder) => void;
}) {
  const wsId = useWorkspaceId();
  const { data: grants, isLoading } = useQuery(
    folderGrantsOptions(wsId, folder.surface, folder.id, "effective", {
      enabled: active,
    }),
  );
  const summary = summarizeAccess(grants ?? [], labelFor);

  return (
    <Button
      variant="outline"
      size="sm"
      className="shrink-0 gap-1.5 font-normal"
      onClick={() => onManage(folder)}
      title="Manage access"
    >
      <span
        className={cn("size-2 rounded-full", TONE_DOT[summary.tone])}
        aria-hidden
      />
      <span
        className={cn(
          "max-w-44 truncate",
          summary.kind !== "direct" && "text-muted-foreground",
        )}
      >
        {isLoading && !grants ? "…" : summary.label}
      </span>
      <ChevronDown className="size-3.5 text-muted-foreground" />
    </Button>
  );
}

// The drop target that re-parents a folder to the top level (root). Only shown
// while a drag is in progress, so it doesn't clutter the resting tree.
const ROOT_DROP_ID = "__collections_root__";

function RootDropZone({ dragging }: { dragging: boolean }) {
  const { setNodeRef, isOver } = useDroppable({ id: ROOT_DROP_ID });
  if (!dragging) return null;
  return (
    <div
      ref={setNodeRef}
      className={cn(
        "mb-1.5 flex items-center justify-center gap-2 rounded-lg border border-dashed py-2 text-xs transition-colors",
        isOver
          ? "border-primary bg-primary/5 text-foreground"
          : "text-muted-foreground",
      )}
    >
      <ArrowUpToLine className="size-3.5" />
      Drop here to move to the top level
    </div>
  );
}

function FolderCard({
  node,
  byId,
  active,
  labelFor,
  dragging,
  isDragSource,
  isBlockedTarget,
  onManage,
}: {
  node: FolderNode;
  byId: Map<string, CollectionFolder>;
  active: boolean;
  labelFor: LabelFor;
  // True while ANY folder in this tree is being dragged.
  dragging: boolean;
  // True if THIS card is the folder currently being dragged.
  isDragSource: boolean;
  // True if THIS card is an invalid drop target for the active drag (it is the
  // dragged folder itself or one of its descendants).
  isBlockedTarget: boolean;
  onManage: (folder: CollectionFolder) => void;
}) {
  const path = folderPath(node, byId);
  // The card is the drop target (drop ONTO a folder to nest inside it); the
  // grip handle is the drag source. Self/descendant targets are disabled so the
  // collision detector never reports them as a drop.
  const drop = useDroppable({
    id: node.id,
    disabled: isDragSource || isBlockedTarget,
  });
  const drag = useDraggable({ id: node.id });
  const showDropHint = dragging && !isDragSource && !isBlockedTarget;

  return (
    <div style={{ marginLeft: node.depth * 24 }}>
      <div
        ref={drop.setNodeRef}
        className={cn(
          "flex items-center gap-2 rounded-lg border bg-card px-3 py-2.5 transition-colors",
          isDragSource && "opacity-50",
          dragging && isBlockedTarget && !isDragSource && "opacity-40",
          showDropHint && drop.isOver && "border-primary bg-primary/5 ring-1 ring-primary",
        )}
      >
        <button
          ref={drag.setNodeRef}
          {...drag.listeners}
          {...drag.attributes}
          className="shrink-0 cursor-grab touch-none rounded text-muted-foreground/50 outline-none hover:text-muted-foreground focus-visible:ring-2 focus-visible:ring-ring active:cursor-grabbing"
          aria-label={`Drag ${node.name} to move it into another folder`}
          title="Drag to move this folder"
        >
          <GripVertical className="size-4" />
        </button>
        <FolderIcon className="size-4 shrink-0 text-muted-foreground" />
        <div className="min-w-0 flex-1">
          <div className="flex items-baseline gap-2">
            <span className="truncate text-sm font-medium">{node.name}</span>
            <span className="shrink-0 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
              Folder
            </span>
          </div>
          {path.length > 1 && (
            <div className="truncate text-xs text-muted-foreground">
              {path.join(" / ")}
            </div>
          )}
        </div>
        <AccessPill
          folder={node}
          active={active}
          labelFor={labelFor}
          onManage={onManage}
        />
      </div>
    </div>
  );
}

function FolderTree({
  folders,
  entityKind,
  active,
  onManage,
  onCreate,
  creating,
}: {
  folders: CollectionFolder[];
  entityKind?: "skill" | "autopilot";
  active: boolean;
  onManage: (folder: CollectionFolder) => void;
  onCreate: (name: string, parentId: string | null) => void;
  creating?: boolean;
}) {
  // labelFor only needs the directories while this tab is open.
  const { labelFor } = useGranteeDirectory(active);
  const tree = React.useMemo(() => buildFolderTree(folders), [folders]);
  const ordered = React.useMemo(() => flattenFolderTree(tree), [tree]);
  const byId = React.useMemo(
    () => new Map(folders.map((f) => [f.id, f])),
    [folders],
  );
  const nodeById = React.useMemo(
    () => new Map(ordered.map((n) => [n.id, n])),
    [ordered],
  );

  const move = useMoveCollectionFolder();
  const [activeId, setActiveId] = React.useState<string | null>(null);
  const [adding, setAdding] = React.useState(false);
  const [newName, setNewName] = React.useState("");
  const activeNode = activeId ? (nodeById.get(activeId) ?? null) : null;
  // A folder can't drop into itself or any descendant; pre-compute the blocked
  // set once per drag so every card can flag itself as a valid/invalid target.
  const blocked = React.useMemo(
    () => (activeNode ? collectSubtreeIds(activeNode) : new Set<string>()),
    [activeNode],
  );

  // A small activation distance lets a click on the access pill or the grip
  // through without starting a drag, matching the inbox drag pattern.
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
  );

  function handleDragStart(event: DragStartEvent) {
    setActiveId(String(event.active.id));
  }

  function handleDragEnd(event: DragEndEvent) {
    const id = String(event.active.id);
    setActiveId(null);
    const node = nodeById.get(id);
    if (!node) return;
    const overId = event.over ? String(event.over.id) : null;
    if (!overId) return;

    if (overId === ROOT_DROP_ID) {
      if (node.parent_id !== null) {
        move.mutate({
          surface: node.surface,
          folderId: node.id,
          parentId: null,
          entityKind,
        });
      }
      return;
    }
    // Guard client-side too (the disabled droppables already prevent these,
    // and the server enforces the cycle guard as the final backstop).
    if (overId === id) return;
    if (collectSubtreeIds(node).has(overId)) return;
    if (overId === node.parent_id) return;
    move.mutate({
      surface: node.surface,
      folderId: node.id,
      parentId: overId,
      entityKind,
    });
  }

  function submitFolder() {
    const trimmed = newName.trim();
    if (!trimmed) return;
    onCreate(trimmed, null);
    setNewName("");
    setAdding(false);
  }

  const folderList = folders.length > 0 ? (
    <DndContext
      sensors={sensors}
      collisionDetection={pointerWithin}
      onDragStart={handleDragStart}
      onDragEnd={handleDragEnd}
      onDragCancel={() => setActiveId(null)}
    >
      <RootDropZone dragging={activeId !== null} />
      <div className="space-y-1.5">
        {ordered.map((node) => (
          <FolderCard
            key={`${node.surface}:${node.id}`}
            node={node}
            byId={byId}
            active={active}
            labelFor={labelFor}
            dragging={activeId !== null}
            isDragSource={activeId === node.id}
            isBlockedTarget={blocked.has(node.id)}
            onManage={onManage}
          />
        ))}
      </div>
      <DragOverlay>
        {activeNode ? (
          <div className="flex items-center gap-2 rounded-lg border bg-card px-3 py-2.5 shadow-lg">
            <FolderIcon className="size-4 shrink-0 text-muted-foreground" />
            <span className="truncate text-sm font-medium">
              {activeNode.name}
            </span>
          </div>
        ) : null}
      </DragOverlay>
    </DndContext>
  ) : null;

  return (
    <div className="space-y-1.5">
      {folderList}
      {adding ? (
        <div className="flex items-center gap-1 px-1">
          <Input
            autoFocus
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") submitFolder();
              if (e.key === "Escape") { setAdding(false); setNewName(""); }
            }}
            placeholder="Folder name"
            className="h-7 text-xs"
          />
          <Button
            size="sm"
            className="h-7 shrink-0"
            onClick={submitFolder}
            disabled={creating || !newName.trim()}
          >
            Add
          </Button>
        </div>
      ) : (
        <button
          onClick={() => setAdding(true)}
          className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-[13px] text-muted-foreground hover:bg-muted/50"
        >
          <FolderPlus className="size-3.5" />
          New folder
        </button>
      )}
    </div>
  );
}

function ProjectTree({
  projects,
  onManage,
}: {
  projects: ProjectTreeItem[];
  onManage: (project: ProjectTreeItem) => void;
}) {
  if (projects.length === 0) {
    return (
      <p className="rounded-lg border border-dashed px-3 py-6 text-center text-sm text-muted-foreground">
        No projects yet.
      </p>
    );
  }

  return (
    <div className="space-y-1.5">
      {projects.map((project) => (
        <React.Fragment key={project.id}>
          <div style={{ marginLeft: project.depth * 24 }}>
            <div className="flex items-center gap-2 rounded-lg border bg-card px-3 py-2.5">
              <Layers className="size-4 shrink-0 text-muted-foreground" />
              <div className="min-w-0 flex-1">
                <div className="flex items-baseline gap-2">
                  <span className="truncate text-sm font-medium">
                    {project.title}
                  </span>
                  <span className="shrink-0 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
                    Project
                  </span>
                </div>
                <div className="truncate text-xs text-muted-foreground">
                  {project.issue_count} issues
                </div>
              </div>
              <Button
                variant="outline"
                size="sm"
                className="shrink-0 font-normal"
                onClick={() => onManage(project)}
              >
                Manage access
              </Button>
            </div>
          </div>
          {project.children && project.children.length > 0 && (
            <ProjectTree projects={project.children} onManage={onManage} />
          )}
        </React.Fragment>
      ))}
    </div>
  );
}

export function CollectionsTab() {
  const wsId = useWorkspaceId();
  const [activeTab, setActiveTab] = React.useState("Documents");

  const { data: artifactFolders = [] } = useQuery(
    artifactCollectionFoldersOptions(wsId),
  );
  const { data: skillFolders = [] } = useQuery(
    entityCollectionFoldersOptions(wsId, "skill"),
  );
  const { data: autopilotFolders = [] } = useQuery(
    entityCollectionFoldersOptions(wsId, "autopilot"),
  );
  const { data: projects = [] } = useQuery(projectTreeOptions(wsId));

  const foldersByGroup: Record<string, CollectionFolder[]> = {
    Documents: artifactFolders.filter((f) => f.group === "Documents"),
    Notes: artifactFolders.filter((f) => f.group === "Notes"),
    Autopilots: autopilotFolders,
    Skills: skillFolders,
  };

  const [active, setActive] = React.useState<CollectionFolder | null>(null);
  const [activeProject, setActiveProject] =
    React.useState<ProjectTreeItem | null>(null);
  const createFolder = useCreateCollectionFolder();
  const createItem = useCreateCollectionItem();

  // Per-tab inline create-item state (name input + submitting flag).
  const [creatingItem, setCreatingItem] = React.useState<Record<string, boolean>>({});
  const [newItemName, setNewItemName] = React.useState<Record<string, string>>({});

  function itemLabel(g: typeof GROUPS[number]): string {
    if (g.artifactKind === "document") return "New document";
    if (g.artifactKind === "note") return "New note";
    if (g.entityKind === "skill") return "New skill";
    return "New autopilot";
  }

  function submitItem(g: typeof GROUPS[number]) {
    const name = (newItemName[g.group] ?? "").trim();
    if (!name) return;
    if (g.artifactKind === "document") {
      createItem.mutate({ kind: "document", title: name });
    } else if (g.artifactKind === "note") {
      createItem.mutate({ kind: "note", title: name });
    } else if (g.entityKind === "skill") {
      createItem.mutate({ kind: "skill", name });
    }
    // Autopilots require an assignee; show a button that navigates in the consumer.
    setNewItemName((prev) => ({ ...prev, [g.group]: "" }));
    setCreatingItem((prev) => ({ ...prev, [g.group]: false }));
  }

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-base font-semibold">Collections</h2>
        <p className="text-sm text-muted-foreground">
          Manage who can reach each folder. A grant gives a group, member, the
          whole workspace, an agent, or a runtime a role on a folder, and
          cascades down to its sub-folders and sub-projects — so a child item
          inherits its parent&apos;s access by default until you set its own.
        </p>
      </div>

      <Tabs
        value={activeTab}
        onValueChange={setActiveTab}
        orientation="horizontal"
      >
        {/* Force a horizontal tab row in the settings context, where the
            shared Tabs otherwise stack vertically — same override the
            cost-optimization settings tabs use. */}
        <TabsList className="!h-auto w-full !flex-row flex-wrap justify-start gap-1">
          {GROUPS.map((g) => (
            <TabsTrigger
              key={g.group}
              value={g.group}
              className="!w-auto !flex-none !justify-center"
            >
              {g.group}
            </TabsTrigger>
          ))}
          <TabsTrigger
            value={PROJECTS_TAB}
            className="!w-auto !flex-none !justify-center"
          >
            Projects
          </TabsTrigger>
        </TabsList>
        {GROUPS.map((g) => (
          <TabsContent key={g.group} value={g.group} className="pt-2">
            {/* New-item inline form — document / note / skill (not autopilot, which
                requires an assignee and belongs in the Autopilots settings page). */}
            {g.entityKind !== "autopilot" && (
              <div className="mb-3">
                {creatingItem[g.group] ? (
                  <div className="flex items-center gap-1 px-1">
                    <Input
                      autoFocus
                      value={newItemName[g.group] ?? ""}
                      onChange={(e) =>
                        setNewItemName((prev) => ({ ...prev, [g.group]: e.target.value }))
                      }
                      onKeyDown={(e) => {
                        if (e.key === "Enter") submitItem(g);
                        if (e.key === "Escape")
                          setCreatingItem((prev) => ({ ...prev, [g.group]: false }));
                      }}
                      placeholder={g.artifactKind ? "Title" : "Name"}
                      className="h-7 text-xs"
                    />
                    <Button
                      size="sm"
                      className="h-7 shrink-0"
                      onClick={() => submitItem(g)}
                      disabled={createItem.isPending || !(newItemName[g.group] ?? "").trim()}
                    >
                      Add
                    </Button>
                  </div>
                ) : (
                  <button
                    onClick={() => setCreatingItem((prev) => ({ ...prev, [g.group]: true }))}
                    className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-[13px] text-muted-foreground hover:bg-muted/50"
                  >
                    <FilePlus className="size-3.5" />
                    {itemLabel(g)}
                  </button>
                )}
              </div>
            )}
            <FolderTree
              folders={foldersByGroup[g.group] ?? []}
              entityKind={g.entityKind}
              active={activeTab === g.group}
              onManage={setActive}
              onCreate={(name, parentId) =>
                createFolder.mutate({
                  surface: g.surface,
                  name,
                  kind: g.artifactKind ?? g.entityKind ?? "document",
                  parentId,
                })
              }
              creating={createFolder.isPending}
            />
          </TabsContent>
        ))}
        <TabsContent value={PROJECTS_TAB} className="pt-2">
          <ProjectTree projects={projects} onManage={setActiveProject} />
        </TabsContent>
      </Tabs>

      {active && (
        <FolderAccessEditor
          surface={active.surface}
          folderId={active.id}
          folderName={active.name}
          open={Boolean(active)}
          onOpenChange={(open) => {
            if (!open) setActive(null);
          }}
        />
      )}
      {activeProject && (
        <Dialog open={Boolean(activeProject)} onOpenChange={(open) => {
          if (!open) setActiveProject(null);
        }}>
          <DialogContent className="sm:max-w-2xl">
            <DialogHeader>
              <DialogTitle>Access — {activeProject.title}</DialogTitle>
              <DialogDescription>
                Set direct access on this project and review inherited access
                from parent projects.
              </DialogDescription>
            </DialogHeader>
            <ProjectGrantsPanel projectId={activeProject.id} />
          </DialogContent>
        </Dialog>
      )}
    </div>
  );
}

export function useCerebroCollectionsSettingsTabs(): ExtraSettingsTab[] {
  const enabled = useFeatureFlag("cerebro_collections");
  if (!enabled) return [];
  return [
    {
      value: "collections",
      label: "Collections",
      icon: Layers,
      content: <CollectionsTab />,
    },
  ];
}
