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
import {
  Tabs,
  TabsList,
  TabsTrigger,
  TabsContent,
} from "@multica/ui/components/ui/tabs";
import { cn } from "@multica/ui/lib/utils";
import { useWorkspaceId } from "@multica/core/hooks";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import {
  artifactCollectionFoldersOptions,
  entityCollectionFoldersOptions,
  folderGrantsOptions,
} from "../queries";
import { useMoveCollectionFolder } from "../mutations";
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

// The four folder groups, one per tab. entityKind is the kind the entity
// backend keys folders by; artifact folders don't need it.
const GROUPS: {
  group: string;
  surface: GrantSurface;
  entityKind?: "skill" | "autopilot";
}[] = [
  { group: "Documents", surface: "artifact" },
  { group: "Notes", surface: "artifact" },
  { group: "Autopilots", surface: "entity", entityKind: "autopilot" },
  { group: "Skills", surface: "entity", entityKind: "skill" },
];

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
  group,
  folders,
  entityKind,
  active,
  onManage,
}: {
  group: string;
  folders: CollectionFolder[];
  entityKind?: "skill" | "autopilot";
  active: boolean;
  onManage: (folder: CollectionFolder) => void;
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

  if (folders.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">No {group} folders yet.</p>
    );
  }

  return (
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

  const foldersByGroup: Record<string, CollectionFolder[]> = {
    Documents: artifactFolders.filter((f) => f.group === "Documents"),
    Notes: artifactFolders.filter((f) => f.group === "Notes"),
    Autopilots: autopilotFolders,
    Skills: skillFolders,
  };

  const [active, setActive] = React.useState<CollectionFolder | null>(null);

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-base font-semibold">Collections</h2>
        <p className="text-sm text-muted-foreground">
          Manage who can reach each folder. A grant gives a group, member, the
          whole workspace, an agent, or a runtime a role on a folder, and
          cascades down to its sub-folders — so a sub-folder inherits its
          parent&apos;s access by default until you set its own.
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
        </TabsList>
        {GROUPS.map((g) => (
          <TabsContent key={g.group} value={g.group} className="pt-2">
            <FolderTree
              group={g.group}
              folders={foldersByGroup[g.group] ?? []}
              entityKind={g.entityKind}
              active={activeTab === g.group}
              onManage={setActive}
            />
          </TabsContent>
        ))}
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
