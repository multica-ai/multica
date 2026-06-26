"use client";

// Settings → Collections (FIR-1590 → Collections). One place to see every
// folder across both surfaces — Documents/Notes (artifact) and
// Autopilots/Skills (entity) — as a tree of folder cards, open the per-folder
// access editor (the "Valgt her / Arvet" editor in Danish product talk), and
// move folders between parents. The same access editor is also reachable inline
// from each folder interface via FolderAccessColumn; this page is the
// consolidated admin view.
//
// Layout follows Jesper's mockup (FIR-1590 feedback #4): one tab per folder
// type, and within each tab a recursive tree where every folder is a card
// showing its access affordance and, for sub-folders, a hint that access is
// inherited from the parent by default (feedback #3).
//
// The platform layer (web + desktop) spreads useCerebroCollectionsSettingsTabs()
// into <SettingsPage extraAccountTabs={...}> — the tab only appears when the
// cerebro_collections flag is on.
import * as React from "react";
import { useQuery } from "@tanstack/react-query";
import { Layers, KeyRound, FolderTree as FolderTreeIcon, MoveRight } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
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
import { useWorkspaceId } from "@multica/core/hooks";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import {
  artifactCollectionFoldersOptions,
  entityCollectionFoldersOptions,
} from "../queries";
import { useMoveCollectionFolder } from "../mutations";
import {
  buildFolderTree,
  collectSubtreeIds,
  flattenFolderTree,
  type FolderNode,
} from "../tree";
import { FolderAccessEditor } from "./folder-access-editor";
import type { CollectionFolder } from "../api";
import type { GrantSurface } from "../types";

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

interface FolderTreeProps {
  group: string;
  folders: CollectionFolder[];
  entityKind?: "skill" | "autopilot";
  onManage: (folder: CollectionFolder) => void;
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
          >
            <MoveRight className="size-3.5" />
            Move
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
  entityKind,
  onManage,
}: {
  node: FolderNode;
  allFolders: CollectionFolder[];
  entityKind?: "skill" | "autopilot";
  onManage: (folder: CollectionFolder) => void;
}) {
  const isSubfolder = node.parent_id !== null;
  return (
    <div
      className="flex items-center gap-2 rounded-md border bg-card p-2"
      style={{ marginLeft: node.depth * 20 }}
    >
      <FolderTreeIcon className="size-4 shrink-0 text-muted-foreground" />
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm">{node.name}</div>
      </div>
      {isSubfolder && (
        <Badge variant="secondary" className="shrink-0 font-normal">
          Inherits access
        </Badge>
      )}
      <MoveFolderMenu
        node={node}
        allFolders={allFolders}
        entityKind={entityKind}
      />
      <Button
        size="sm"
        variant="outline"
        className="shrink-0"
        onClick={() => onManage(node)}
      >
        <KeyRound className="size-3.5" />
        Manage access
      </Button>
    </div>
  );
}

function FolderTree({ group, folders, entityKind, onManage }: FolderTreeProps) {
  const tree = React.useMemo(() => buildFolderTree(folders), [folders]);
  const ordered = React.useMemo(() => flattenFolderTree(tree), [tree]);

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
          entityKind={entityKind}
          onManage={onManage}
        />
      ))}
    </div>
  );
}

export function CollectionsTab() {
  const wsId = useWorkspaceId();

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

      <Tabs defaultValue="Documents">
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
