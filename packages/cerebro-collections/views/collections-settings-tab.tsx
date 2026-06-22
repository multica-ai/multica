"use client";

// Settings → Collections (FIR-1590 → Collections). One place to see every
// folder across both surfaces — Documents/Notes (artifact) and
// Autopilots/Skills (entity) — and open the per-folder access editor (the
// "Valgt her / Arvet" editor in Danish product talk). The same editor is also
// reachable inline from each folder interface via FolderAccessColumn; this page
// is the consolidated admin view.
//
// The platform layer (web + desktop) spreads useCerebroCollectionsSettingsTabs()
// into <SettingsPage extraAccountTabs={...}> — the tab only appears when the
// cerebro_collections flag is on.
import * as React from "react";
import { useQuery } from "@tanstack/react-query";
import { Layers, KeyRound } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { useWorkspaceId } from "@multica/core/hooks";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import {
  artifactCollectionFoldersOptions,
  entityCollectionFoldersOptions,
} from "../queries";
import { FolderAccessEditor } from "./folder-access-editor";
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

interface FolderRow {
  surface: GrantSurface;
  id: string;
  name: string;
  group: string;
}

function FolderList({
  title,
  rows,
  onManage,
}: {
  title: string;
  rows: FolderRow[];
  onManage: (row: FolderRow) => void;
}) {
  return (
    <div className="space-y-2">
      <h3 className="text-sm font-medium">{title}</h3>
      {rows.length === 0 ? (
        <p className="text-sm text-muted-foreground">No folders yet.</p>
      ) : (
        <div className="divide-y rounded-md border">
          {rows.map((row) => (
            <div key={`${row.surface}:${row.id}`} className="flex items-center gap-2 p-2">
              <div className="min-w-0 flex-1">
                <div className="truncate text-sm">{row.name}</div>
              </div>
              <Badge variant="outline" className="shrink-0 capitalize">
                {row.group}
              </Badge>
              <Button
                size="sm"
                variant="outline"
                className="shrink-0"
                onClick={() => onManage(row)}
              >
                <KeyRound className="size-3.5" />
                Manage access
              </Button>
            </div>
          ))}
        </div>
      )}
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

  const documentRows: FolderRow[] = artifactFolders;
  const entityRows: FolderRow[] = [...skillFolders, ...autopilotFolders];

  const [active, setActive] = React.useState<FolderRow | null>(null);

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-base font-semibold">Collections</h2>
        <p className="text-sm text-muted-foreground">
          Manage who can reach each folder. A grant gives a group, member, the
          whole workspace, an agent, or a runtime a role on a folder, and
          cascades down to its sub-folders.
        </p>
      </div>

      <FolderList
        title="Documents & Notes"
        rows={documentRows}
        onManage={setActive}
      />
      <FolderList
        title="Autopilots & Skills"
        rows={entityRows}
        onManage={setActive}
      />

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
