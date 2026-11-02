"use client";

// Labrastro fork (OL-1030, OL-1170): in-place folder tree for the agents page.
// Custom folders come from the OL-1028 tree; squad folders are derived with
// squad id = folder id. Expansion is local UI state and never changes the URL.

import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react";
import {
  AlertTriangle,
  ExternalLink,
  Folder,
  FolderInput,
  FolderPlus,
  ListChevronsDownUp,
  ListChevronsUpDown,
  MoreHorizontal,
  Pencil,
  Trash2,
} from "@multica/ui/icons";
import { useQuery } from "@tanstack/react-query";
import type { AgentSquadFolder } from "@multica/core/api/schemas";
import type { Agent } from "@multica/core/types";
import type { AgentColumnKey, AgentListFilters } from "@multica/core/agents/stores";
import { agentFolderTreeOptions } from "@multica/core/agents/folder-queries";
import { useWorkspacePaths } from "@multica/core/paths";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import { Button } from "@multica/ui/components/ui/button";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { ListGridCell } from "@multica/ui/components/ui/list-grid";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  FolderExplorerRow,
  NewFolderButton,
  entryPathHints,
  flattenFolderExplorerRows,
  useFolderExplorerExpansion,
  type FolderExplorerFolder,
} from "../../common/folder-explorer";
import { useT } from "../../i18n";
import { useNavigation } from "../../navigation";
import {
  agentEntryFolderIds,
  agentMoveDestinations,
  explorerFolders,
  folderAgents,
  folderMoveDestinations,
  placementFolderByAgentId,
  squadById,
  squadIdsByAgentId,
  squadMoveDestinations,
} from "../lib/agent-folder-tree";
import {
  AgentFolderDeleteDialog,
  AgentFolderFormDialog,
  AgentFolderMoveDialog,
  MoveAgentsDialog,
  type AgentFolderFormState,
} from "./agent-folder-dialogs";
import { AgentFolderAvatars } from "./agent-folder-avatars";
import { AgentFolderStatusCell } from "./agent-folder-status";
import type { AgentTreeActions } from "./agent-tree-actions";
import type { AgentListRow } from "./agents-page";
import type { AgentsScope } from "@multica/core/agents/stores";

interface AgentExplorerRowBase {
  key: string;
  parentFolderId: string | null;
  depth: number;
}

export type AgentExplorerListRow =
  | (AgentExplorerRowBase & {
      kind: "agent";
      row: AgentListRow;
      pathHints?: string[];
      leaderLabel?: string;
    })
  | (AgentExplorerRowBase & {
      kind: "folder";
      folder: FolderExplorerFolder;
      entryCount: number;
      memberRows: AgentListRow[];
    });

export type AgentExplorerAuxRow = Exclude<AgentExplorerListRow, { kind: "agent" }>;

export interface AgentsPageListModel {
  rows: AgentExplorerListRow[];
  agentRows: AgentListRow[];
  emptyMessage: string | null;
}

export interface AgentsPageFolderExplorer {
  treeActions: AgentTreeActions | undefined;
  toolbarActions: ReactNode;
  notice: ReactNode;
  dialogs: ReactNode;
  organizeRows: (rows: AgentListRow[]) => AgentsPageListModel;
  renderRow: (row: AgentExplorerAuxRow) => ReactNode;
  treeLoading: boolean;
  treeReady: boolean;
  hasFolders: boolean;
}

function AgentFolderRowMenu({
  folder,
  squad,
  canManageSquadFolder,
  onOpenSquad,
  onCreateSubfolder,
  onRename,
  onMove,
  onDelete,
}: {
  folder: FolderExplorerFolder;
  squad: AgentSquadFolder | null;
  canManageSquadFolder: boolean;
  onOpenSquad: () => void;
  onCreateSubfolder: () => void;
  onRename: () => void;
  onMove: () => void;
  onDelete: () => void;
}) {
  const { t } = useT("agent-folders");
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <button
            type="button"
            aria-label={t(($) => $.tree.folder_actions, { name: folder.name })}
            onPointerDown={(event) => event.stopPropagation()}
            onClick={(event) => event.stopPropagation()}
            className="inline-flex size-7 shrink-0 items-center justify-center rounded-xs text-muted-foreground opacity-0 transition-opacity hover:bg-accent hover:text-foreground focus-visible:opacity-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring group-hover/row:opacity-100 aria-expanded:opacity-100 [@media(hover:none)]:opacity-100"
          >
            <MoreHorizontal className="size-4" />
          </button>
        }
      />
      <DropdownMenuContent align="end" className="w-56">
        {squad ? (
          <>
            <DropdownMenuItem onClick={onOpenSquad}>
              <ExternalLink />
              {t(($) => $.tree.open_squad)}
            </DropdownMenuItem>
            {canManageSquadFolder ? (
              <DropdownMenuItem onClick={onMove}>
                <FolderInput />
                {t(($) => $.tree.move_to_folder)}
              </DropdownMenuItem>
            ) : (
              <DropdownMenuItem disabled>
                <FolderInput />
                <span className="flex min-w-0 flex-col">
                  <span>{t(($) => $.tree.move_to_folder)}</span>
                  <span className="whitespace-normal text-caption text-muted-foreground">
                    {t(($) => $.tree.squad_move_forbidden)}
                  </span>
                </span>
              </DropdownMenuItem>
            )}
          </>
        ) : (
          <>
            <DropdownMenuItem onClick={onCreateSubfolder}>
              <FolderPlus />
              {t(($) => $.tree.new_subfolder)}
            </DropdownMenuItem>
            <DropdownMenuItem onClick={onRename}>
              <Pencil />
              {t(($) => $.tree.rename_folder)}
            </DropdownMenuItem>
            <DropdownMenuItem onClick={onMove}>
              <Folder />
              {t(($) => $.tree.move_folder)}
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem variant="destructive" onClick={onDelete}>
              <Trash2 />
              {t(($) => $.tree.delete_folder)}
            </DropdownMenuItem>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function AgentFolderTrailingCells({
  rows,
  isColVisible,
}: {
  rows: AgentListRow[];
  isColVisible: (key: AgentColumnKey) => boolean;
}) {
  return (
    <>
      {isColVisible("status") ? (
        <AgentFolderStatusCell rows={rows} />
      ) : (
        <ListGridCell className="px-0" />
      )}
      {(["owner", "access", "runtime", "lastActive", "runs", "model", "created"] as const).map((key) => (
        <ListGridCell key={key} className="hidden px-0 @2xl:flex" />
      ))}
    </>
  );
}

export function useAgentsPageFolderExplorer({
  wsId,
  currentUserId,
  isAdmin,
  scope,
  search,
  filters,
  isColVisible,
  onMoved,
}: {
  wsId: string;
  currentUserId: string | null;
  isAdmin: boolean;
  scope: AgentsScope;
  search: string;
  filters: AgentListFilters;
  isColVisible: (key: AgentColumnKey) => boolean;
  onMoved: () => void;
}): AgentsPageFolderExplorer {
  const { t: tFolder } = useT("agent-folders");
  const navigation = useNavigation();
  const paths = useWorkspacePaths();
  const {
    data: folderTree,
    error: folderTreeError,
    failureCount: folderTreeFailures,
    refetch: refetchFolderTree,
  } = useQuery(agentFolderTreeOptions(wsId));
  const [moveAgents, setMoveAgents] = useState<{
    agents: Agent[];
    variant: "move" | "place";
  } | null>(null);
  const [folderForm, setFolderForm] = useState<AgentFolderFormState | null>(null);
  const [folderMove, setFolderMove] = useState<FolderExplorerFolder | null>(null);
  const [folderDelete, setFolderDelete] = useState<FolderExplorerFolder | null>(null);
  const {
    expandedFolderIds,
    toggleFolder,
    expandFolder,
    expandAll,
    collapseAll,
  } = useFolderExplorerExpansion("agents", wsId);

  // Never hide entries behind an unreachable tree: after the first failed
  // attempt with no data yet (even with the retry still pending) or 5s
  // without any data, fall back to the flat list with a notice. Late data
  // restores the hierarchy. A failed background refetch that still has the
  // cached tree keeps the hierarchy — the retry usually succeeds.
  const [treeWaitExpired, setTreeWaitExpired] = useState(false);
  useEffect(() => {
    if (folderTree || folderTreeError) {
      setTreeWaitExpired(false);
      return;
    }
    const timer = setTimeout(() => setTreeWaitExpired(true), 5000);
    return () => clearTimeout(timer);
  }, [folderTree, folderTreeError]);

  const treeReady = !!folderTree && !folderTreeError;
  const treeUnavailable =
    folderTree === null ||
    !!folderTreeError ||
    (folderTree === undefined && folderTreeFailures > 0) ||
    treeWaitExpired;
  const treeLoading = !treeReady && !treeUnavailable;
  const flatRequested =
    search.trim().length > 0 ||
    Object.values(filters).some((values) => values.length > 0);
  const isFlatView = treeUnavailable || flatRequested || scope === "archived";

  const folders = useMemo<FolderExplorerFolder[]>(
    () => (folderTree ? explorerFolders(folderTree) : []),
    [folderTree],
  );
  const squads = useMemo(
    () => (folderTree ? squadById(folderTree) : new Map<string, AgentSquadFolder>()),
    [folderTree],
  );
  const placements = useMemo(
    () => (treeReady && folderTree ? placementFolderByAgentId(folderTree) : null),
    [treeReady, folderTree],
  );
  const squadMemberships = useMemo(
    () => (treeReady && folderTree ? squadIdsByAgentId(folderTree) : null),
    [treeReady, folderTree],
  );

  const entryFolderIdsFor = useCallback(
    (agentId: string): string[] =>
      agentEntryFolderIds(agentId, placements ?? new Map(), squadMemberships ?? new Map()),
    [placements, squadMemberships],
  );

  const organizeRows = useCallback(
    (rows: AgentListRow[]): AgentsPageListModel => {
      if (treeLoading) return { rows: [], agentRows: [], emptyMessage: null };

      const entries = rows.map((row) => ({
        id: row.agent.id,
        folderIds: entryFolderIdsFor(row.agent.id),
        row,
      }));

      if (isFlatView) {
        return {
          rows: rows.map((row, index) => {
            const hints = entryPathHints(folders, entries[index]!);
            return {
              kind: "agent" as const,
              key: `agent-root-${row.agent.id}`,
              parentFolderId: null,
              depth: 0,
              row,
              pathHints: hints.length > 0 ? hints : undefined,
            };
          }),
          agentRows: rows,
          emptyMessage: null,
        };
      }

      const members = folderAgents(folders, entries, rows.map((row) => row.agent), squads);
      const rowsByAgentId = new Map(rows.map((row) => [row.agent.id, row]));
      const visibleAgentRows = new Map<string, AgentListRow>();
      const leaderLabel = tFolder(($) => $.tree.leader_label);
      const flattened = flattenFolderExplorerRows({
        folders,
        entries,
        expandedFolderIds,
        entryKeyPrefix: "agent",
      });
      const organized: AgentExplorerListRow[] = flattened.map((flatRow) => {
        if (flatRow.kind === "folder") {
          const memberRows = (members.get(flatRow.folder.id) ?? [])
            .map((agent) => rowsByAgentId.get(agent.id))
            .filter((row): row is AgentListRow => row !== undefined);
          return {
            kind: "folder",
            key: flatRow.key,
            folder: flatRow.folder,
            parentFolderId: flatRow.parentFolderId,
            depth: flatRow.depth,
            entryCount: memberRows.length,
            memberRows,
          };
        }
        const { row } = flatRow.entry;
        visibleAgentRows.set(row.agent.id, row);
        const squad = flatRow.parentFolderId ? squads.get(flatRow.parentFolderId) : undefined;
        return {
          kind: "agent",
          key: flatRow.key,
          parentFolderId: flatRow.parentFolderId,
          depth: flatRow.depth,
          row,
          leaderLabel: squad?.leaderId === row.agent.id ? leaderLabel : undefined,
        };
      });
      return {
        rows: organized,
        agentRows: [...visibleAgentRows.values()],
        emptyMessage: null,
      };
    },
    [treeLoading, isFlatView, entryFolderIdsFor, folders, squads, expandedFolderIds, tFolder],
  );

  const renderRow = useCallback(
    (row: AgentExplorerAuxRow): ReactNode => {
      const { folder } = row;
      const squad = squads.get(folder.id) ?? null;
      const expanded = expandedFolderIds.has(folder.id);
      const canManageSquadFolder =
        squad !== null &&
        (isAdmin || (currentUserId !== null && squad.creatorId === currentUserId));
      return (
        <FolderExplorerRow
          key={row.key}
          className="h-14 border-b bg-muted/40 [--folder-row-background:color-mix(in_oklab,var(--muted)_40%,var(--background))] hover:bg-muted/70 hover:[--folder-row-background:color-mix(in_oklab,var(--muted)_70%,var(--background))]"
          name={folder.name}
          typeLabel={squad ? tFolder(($) => $.tree.squad_label) : undefined}
          countLabel={tFolder(($) => $.tree.folder_agent_count, { count: row.entryCount })}
          openLabel={tFolder(
            expanded ? ($) => $.tree.collapse_folder : ($) => $.tree.expand_folder,
            { name: folder.name },
          )}
          onOpen={() => toggleFolder(folder.id)}
          expanded={expanded}
          depth={row.depth}
          leading={<ListGridCell className="hidden justify-center px-0 @2xl:flex" />}
          trailing={<AgentFolderTrailingCells rows={row.memberRows} isColVisible={isColVisible} />}
          icon={
            squad ? (
              <span className="relative size-8 shrink-0">
                <ActorAvatar
                  name={squad.name}
                  initials={squad.name.slice(0, 2).toUpperCase()}
                  avatarUrl={resolvePublicFileUrl(squad.avatarUrl)}
                  isSquad
                  size="lg"
                  className="shrink-0"
                />
                <span className="absolute -right-1 -bottom-1 flex size-4 items-center justify-center rounded-xs bg-warning/15 text-warning ring-2 ring-[var(--folder-row-background)]">
                  <Folder aria-hidden="true" className="size-3 fill-current" />
                </span>
              </span>
            ) : (
              <span className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-warning/15 text-warning">
                <Folder aria-hidden="true" className="size-5 fill-current" />
              </span>
            )
          }
          preview={
            <AgentFolderAvatars
              agents={row.memberRows.map((member) => member.agent)}
              count={row.entryCount}
              emptyLabel={tFolder(($) => $.tree.empty_label)}
              presenceByAgentId={new Map(row.memberRows.map((member) => [member.agent.id, member.presence]))}
              leaderId={squad?.leaderId}
            />
          }
          actions={
            <AgentFolderRowMenu
              folder={folder}
              squad={squad}
              canManageSquadFolder={canManageSquadFolder}
              onOpenSquad={() => {
                if (squad) navigation.push(paths.squadDetail(squad.squadId));
              }}
              onCreateSubfolder={() => setFolderForm({ mode: "create", parentId: folder.id })}
              onRename={() => setFolderForm({ mode: "rename", folder })}
              onMove={() => setFolderMove(folder)}
              onDelete={() => setFolderDelete(folder)}
            />
          }
        />
      );
    },
    [squads, expandedFolderIds, isAdmin, currentUserId, tFolder, toggleFolder, isColVisible, navigation, paths],
  );

  const treeActions: AgentTreeActions | undefined =
    treeReady && placements && squadMemberships
      ? {
          defaultVariant: "move",
          squadFolderIds: new Set(squads.keys()),
          onMove: (agents, variant) => setMoveAgents({ agents, variant }),
        }
      : undefined;

  const toolbarActions = treeReady && !isFlatView ? (
    <>
      {folders.length > 0 && (
        <>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            aria-label={tFolder(($) => $.tree.expand_all)}
            onClick={() => expandAll(folders.map((folder) => folder.id))}
          >
            <ListChevronsUpDown aria-hidden="true" className="size-3.5" />
            <span className="hidden md:inline">{tFolder(($) => $.tree.expand_all)}</span>
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            aria-label={tFolder(($) => $.tree.collapse_all)}
            onClick={collapseAll}
          >
            <ListChevronsDownUp aria-hidden="true" className="size-3.5" />
            <span className="hidden md:inline">{tFolder(($) => $.tree.collapse_all)}</span>
          </Button>
        </>
      )}
      <NewFolderButton
        label={tFolder(($) => $.tree.new_folder)}
        onClick={() => setFolderForm({ mode: "create", parentId: null })}
      />
    </>
  ) : null;

  const notice = treeUnavailable ? (
    <div
      role="alert"
      className="flex shrink-0 flex-wrap items-center gap-2 border-b bg-warning/10 px-6 py-2 text-caption text-muted-foreground"
    >
      <AlertTriangle aria-hidden="true" className="size-3.5 shrink-0 text-warning" />
      <span className="min-w-0 flex-1">{tFolder(($) => $.tree.unavailable)}</span>
      <Button type="button" variant="outline" size="sm" onClick={() => refetchFolderTree()}>
        {tFolder(($) => $.tree.retry)}
      </Button>
    </div>
  ) : null;

  const dialogs = (
    <>
      {moveAgents && treeReady && (
        <MoveAgentsDialog
          wsId={wsId}
          folders={folders}
          destinations={agentMoveDestinations(folders)}
          agents={moveAgents.agents}
          hasSquadMembers={moveAgents.agents.some((agent) => squadMemberships?.has(agent.id))}
          variant={moveAgents.variant}
          open
          onOpenChange={(open) => {
            if (!open) setMoveAgents(null);
          }}
          onMoved={onMoved}
        />
      )}
      {folderForm && (
        <AgentFolderFormDialog
          wsId={wsId}
          state={folderForm}
          onCreated={(parentId) => {
            if (parentId) expandFolder(parentId);
          }}
          onClose={() => setFolderForm(null)}
        />
      )}
      {folderMove && treeReady && (
        <AgentFolderMoveDialog
          wsId={wsId}
          folders={folders}
          destinations={
            folderMove.managed
              ? squadMoveDestinations(folders)
              : folderMoveDestinations(folders, folderMove.id)
          }
          folder={folderMove}
          onClose={() => setFolderMove(null)}
        />
      )}
      {folderDelete && (
        <AgentFolderDeleteDialog
          wsId={wsId}
          folder={folderDelete}
          onDeleted={() => {}}
          onClose={() => setFolderDelete(null)}
        />
      )}
    </>
  );

  return {
    treeActions,
    toolbarActions,
    notice,
    dialogs,
    organizeRows,
    renderRow,
    treeLoading,
    treeReady,
    hasFolders: (folderTree?.folders.length ?? 0) + (folderTree?.squads.length ?? 0) > 0,
  };
}
