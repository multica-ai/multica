// The per-folder access editor (the "Valgt her / Arvet" editor in Danish
// product talk — rendered in English as the "This folder" / "Inherited" tabs).
//
//   This folder (view=direct)    — grants set directly on this folder; add,
//                                  change a role, or remove. Editable.
//   Inherited (view=effective)   — grants that cascade in from a parent folder;
//                                  read-only here (edit them on the parent).
//
// One editor serves both folder backends via the `surface` prop ('artifact' for
// Documents/Notes, 'entity' for Autopilots/Skills). Gated by the
// cerebro_collections flag at the call sites; this component assumes the flag
// is on.
import * as React from "react";
import { useQuery } from "@tanstack/react-query";
import { Plus, Trash2 } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@multica/ui/components/ui/tabs";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { useWorkspaceId } from "@multica/core/hooks";
import { folderGrantsOptions } from "../queries";
import { useRemoveFolderGrant, useUpsertFolderGrant } from "../mutations";
import type {
  GranteeType,
  GrantRole,
  GrantSurface,
} from "../types";
import {
  GRANTEE_TYPE_LABELS,
  ROLE_LABELS,
  useGranteeDirectory,
} from "./use-grantee-directory";

const ROLE_VALUES: GrantRole[] = ["viewer", "editor", "full_access"];
const GRANTEE_TYPE_VALUES: GranteeType[] = [
  "group",
  "member",
  "workspace",
  "agent",
  "runtime",
];

export function FolderAccessEditor({
  surface,
  folderId,
  folderName,
  open,
  onOpenChange,
}: {
  surface: GrantSurface;
  folderId: string;
  folderName: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const wsId = useWorkspaceId();
  const directory = useGranteeDirectory(open);

  const directQuery = useQuery(
    folderGrantsOptions(wsId, surface, folderId, "direct", { enabled: open }),
  );
  const effectiveQuery = useQuery(
    folderGrantsOptions(wsId, surface, folderId, "effective", { enabled: open }),
  );

  const upsert = useUpsertFolderGrant();
  const remove = useRemoveFolderGrant();

  const direct = directQuery.data ?? [];
  const inherited = (effectiveQuery.data ?? []).filter((g) => !g.is_direct);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>Access — {folderName}</DialogTitle>
          <DialogDescription>
            Grant this folder to a group, member, the whole workspace, an agent,
            or a runtime. Grants cascade down to its sub-folders.
          </DialogDescription>
        </DialogHeader>

        <Tabs defaultValue="direct">
          <TabsList className="w-full">
            <TabsTrigger value="direct" className="flex-1">
              This folder
            </TabsTrigger>
            <TabsTrigger value="inherited" className="flex-1">
              Inherited
            </TabsTrigger>
          </TabsList>

          <TabsContent value="direct" className="space-y-3 pt-2">
            {direct.length === 0 && (
              <p className="text-sm text-muted-foreground">
                No access set directly on this folder yet.
              </p>
            )}
            {direct.map((g) => (
              <div
                key={`${g.grantee_type}:${g.grantee_id ?? "ws"}`}
                className="flex items-center gap-2"
              >
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm">
                    {directory.labelFor(g.grantee_type, g.grantee_id)}
                  </div>
                  <div className="text-xs text-muted-foreground">
                    {GRANTEE_TYPE_LABELS[g.grantee_type]}
                  </div>
                </div>
                <Select
                  value={g.role}
                  onValueChange={(role) =>
                    upsert.mutate({
                      surface,
                      folder_id: folderId,
                      grantee_type: g.grantee_type,
                      grantee_id: g.grantee_id,
                      role: role as GrantRole,
                    })
                  }
                >
                  <SelectTrigger className="w-36 shrink-0">
                    <SelectValue>{() => ROLE_LABELS[g.role]}</SelectValue>
                  </SelectTrigger>
                  <SelectContent>
                    {ROLE_VALUES.map((r) => (
                      <SelectItem key={r} value={r}>
                        {ROLE_LABELS[r]}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <Button
                  size="icon-sm"
                  variant="ghost"
                  aria-label="Remove access"
                  className="shrink-0 text-muted-foreground"
                  onClick={() =>
                    remove.mutate({
                      surface,
                      folder_id: folderId,
                      grantee_type: g.grantee_type,
                      grantee_id: g.grantee_id,
                    })
                  }
                >
                  <Trash2 className="size-4" />
                </Button>
              </div>
            ))}

            <AddGrantRow
              surface={surface}
              folderId={folderId}
              directory={directory}
              existing={direct.map(
                (g) => `${g.grantee_type}:${g.grantee_id ?? "ws"}`,
              )}
              onAdd={(input) => upsert.mutate(input)}
            />
          </TabsContent>

          <TabsContent value="inherited" className="space-y-3 pt-2">
            {inherited.length === 0 && (
              <p className="text-sm text-muted-foreground">
                Nothing inherited from a parent folder.
              </p>
            )}
            {inherited.map((g) => (
              <div
                key={`${g.grantee_type}:${g.grantee_id ?? "ws"}:${g.source_folder_id}`}
                className="flex items-center gap-2"
              >
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm">
                    {directory.labelFor(g.grantee_type, g.grantee_id)}
                  </div>
                  <div className="text-xs text-muted-foreground">
                    {GRANTEE_TYPE_LABELS[g.grantee_type]} · inherited
                  </div>
                </div>
                <Badge variant="outline" className="shrink-0">
                  {ROLE_LABELS[g.role]}
                </Badge>
              </div>
            ))}
          </TabsContent>
        </Tabs>
      </DialogContent>
    </Dialog>
  );
}

function AddGrantRow({
  surface,
  folderId,
  directory,
  existing,
  onAdd,
}: {
  surface: GrantSurface;
  folderId: string;
  directory: ReturnType<typeof useGranteeDirectory>;
  existing: string[];
  onAdd: (input: {
    surface: GrantSurface;
    folder_id: string;
    grantee_type: GranteeType;
    grantee_id: string | null;
    role: GrantRole;
  }) => void;
}) {
  const [granteeType, setGranteeType] = React.useState<GranteeType>("group");
  const [granteeId, setGranteeId] = React.useState<string | null>(null);
  const [role, setRole] = React.useState<GrantRole>("viewer");

  const needsId = granteeType !== "workspace";
  const idOptions =
    granteeType === "workspace" ? [] : directory.options[granteeType];

  const key = `${granteeType}:${granteeId ?? "ws"}`;
  const duplicate = existing.includes(key);
  const canAdd = (!needsId || Boolean(granteeId)) && !duplicate;

  function reset() {
    setGranteeId(null);
  }

  return (
    <div className="space-y-2 rounded-md border border-dashed p-2">
      <div className="flex items-center gap-2">
        <Select
          value={granteeType}
          onValueChange={(v) => {
            setGranteeType(v as GranteeType);
            setGranteeId(null);
          }}
        >
          <SelectTrigger className="w-36 shrink-0">
            <SelectValue>{() => GRANTEE_TYPE_LABELS[granteeType]}</SelectValue>
          </SelectTrigger>
          <SelectContent>
            {GRANTEE_TYPE_VALUES.map((t) => (
              <SelectItem key={t} value={t}>
                {GRANTEE_TYPE_LABELS[t]}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        {needsId && (
          <Select
            value={granteeId ?? ""}
            onValueChange={(v) => setGranteeId(v)}
          >
            <SelectTrigger className="min-w-0 flex-1">
              <SelectValue placeholder="Choose…">
                {() => idOptions.find((o) => o.id === granteeId)?.name ?? null}
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              {idOptions.length === 0 && (
                <SelectItem value="__none" disabled>
                  None available
                </SelectItem>
              )}
              {idOptions.map((o) => (
                <SelectItem key={o.id} value={o.id}>
                  {o.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
      </div>

      <div className="flex items-center gap-2">
        <Select value={role} onValueChange={(v) => setRole(v as GrantRole)}>
          <SelectTrigger className="w-36 shrink-0">
            <SelectValue>{() => ROLE_LABELS[role]}</SelectValue>
          </SelectTrigger>
          <SelectContent>
            {ROLE_VALUES.map((r) => (
              <SelectItem key={r} value={r}>
                {ROLE_LABELS[r]}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button
          size="sm"
          className="ml-auto"
          disabled={!canAdd}
          onClick={() => {
            onAdd({
              surface,
              folder_id: folderId,
              grantee_type: granteeType,
              grantee_id: needsId ? granteeId : null,
              role,
            });
            reset();
          }}
        >
          <Plus className="size-4" />
          Add
        </Button>
      </div>
      {duplicate && (
        <p className="text-xs text-muted-foreground">
          That grantee already has access here — change its role above.
        </p>
      )}
    </div>
  );
}
