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
// the full access editor; a Move menu re-parents the folder.
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
  MoveRight,
} from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  Tabs,
  TabsList,
  TabsTrigger,
  TabsContent,
} from "@multica/ui/components/ui/tabs";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
} from "@multica/ui/components/ui/dropdown-menu";
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

function MoveFolderMenu({
  node,
  allFolders,
  entityKind,
}: {
  node: FolderNode;
  allFolders: CollectionFolder[];
  entityKind?: "skill" | "autopilot";
}) {
  const move = useMoveCollectionFolder();
  // A folder cannot move into itself or any descendant (would orphan/cycle).
  const blocked = collectSubtreeIds(node);
  const targets = allFolders.filter((f) => !blocked.has(f.id));

  const onMove = (parentId: string | null) => {
    if (parentId === node.parent_id) return; // no-op
    move.mutate({
      surface: node.surface,
      folderId: node.id,
      parentId,
      entityKind,
    });
  };

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            size="sm"
            variant="ghost"
            className="shrink-0 text-muted-foreground"
            disabled={move.isPending}
            aria-label="Move folder"
            title="Move folder"
          >
            <MoveRight className="size-3.5" />
          </Button>
        }
      />
      <DropdownMenuContent align="end" className="max-h-72 overflow-auto">
        <DropdownMenuLabel>Move to</DropdownMenuLabel>
        <DropdownMenuItem
          disabled={node.parent_id === null}
          onClick={() => onMove(null)}
        >
          Top level (root)
        </DropdownMenuItem>
        {targets.length > 0 && <DropdownMenuSeparator />}
        {targets.map((t) => (
          <DropdownMenuItem
            key={t.id}
            disabled={t.id === node.parent_id}
            onClick={() => onMove(t.id)}
          >
            {t.name}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function FolderCard({
  node,
  allFolders,
  byId,
  active,
  labelFor,
  entityKind,
  onManage,
}: {
  node: FolderNode;
  allFolders: CollectionFolder[];
  byId: Map<string, CollectionFolder>;
  active: boolean;
  labelFor: LabelFor;
  entityKind?: "skill" | "autopilot";
  onManage: (folder: CollectionFolder) => void;
}) {
  const path = folderPath(node, byId);
  return (
    <div style={{ marginLeft: node.depth * 24 }}>
      <div className="flex items-center gap-3 rounded-lg border bg-card px-3 py-2.5">
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
        <MoveFolderMenu
          node={node}
          allFolders={allFolders}
          entityKind={entityKind}
        />
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

  if (folders.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">No {group} folders yet.</p>
    );
  }

  return (
    <div className="space-y-1.5">
      {ordered.map((node) => (
        <FolderCard
          key={`${node.surface}:${node.id}`}
          node={node}
          allFolders={folders}
          byId={byId}
          active={active}
          labelFor={labelFor}
          entityKind={entityKind}
          onManage={onManage}
        />
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

      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList>
          {GROUPS.map((g) => (
            <TabsTrigger key={g.group} value={g.group}>
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
