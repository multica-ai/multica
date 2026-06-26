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
import { ChevronDown, Plus, Trash2 } from "lucide-react";
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
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Input } from "@multica/ui/components/ui/input";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
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

  // Inherit-by-default: a sub-folder with no access of its own inherits its
  // parent's by default, so once we know its grants we open the Inherited tab
  // (rather than an empty This-folder tab) — making that default visible. The
  // user can still switch to This folder to set the folder's own access. A
  // null `tab` means "not yet chosen by the user", so the resolved default can
  // react to the loaded data without overriding a manual switch.
  const [tab, setTab] = React.useState<string | null>(null);
  const loaded = directQuery.data !== undefined;
  const resolvedTab =
    tab ??
    (loaded && direct.length === 0 && inherited.length > 0
      ? "inherited"
      : "direct");

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>Access — {folderName}</DialogTitle>
          <DialogDescription>
            Grant this folder to a group, member, the whole workspace, an agent,
            or a runtime. Grants cascade down to its sub-folders.
          </DialogDescription>
        </DialogHeader>

        <Tabs value={resolvedTab} onValueChange={setTab}>
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
  // Multi-select: several grantees of the chosen type can be queued and added in
  // one go, each as its own grant at the selected role.
  const [selectedIds, setSelectedIds] = React.useState<Set<string>>(new Set());
  const [role, setRole] = React.useState<GrantRole>("viewer");
  const [pickerOpen, setPickerOpen] = React.useState(false);
  const [search, setSearch] = React.useState("");

  const needsId = granteeType !== "workspace";
  const idOptions =
    granteeType === "workspace" ? [] : directory.options[granteeType];

  // An option already granted directly on this folder is shown as such and
  // can't be re-queued (change its role on the row above instead).
  const isExisting = (id: string) => existing.includes(`${granteeType}:${id}`);

  const filtered = idOptions.filter((o) =>
    o.name.toLowerCase().includes(search.trim().toLowerCase()),
  );

  // Workspace is a single grant with no id; grantee types add one grant per
  // selected, skipping any that already exist.
  const workspaceDuplicate =
    granteeType === "workspace" && existing.includes("workspace:ws");
  const addableIds = [...selectedIds].filter((id) => !isExisting(id));
  const canAdd = needsId ? addableIds.length > 0 : !workspaceDuplicate;

  function changeType(next: GranteeType) {
    setGranteeType(next);
    setSelectedIds(new Set());
    setSearch("");
    setPickerOpen(false);
  }

  function toggle(id: string) {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  function add() {
    if (needsId) {
      for (const id of addableIds) {
        onAdd({
          surface,
          folder_id: folderId,
          grantee_type: granteeType,
          grantee_id: id,
          role,
        });
      }
    } else {
      onAdd({
        surface,
        folder_id: folderId,
        grantee_type: granteeType,
        grantee_id: null,
        role,
      });
    }
    setSelectedIds(new Set());
    setSearch("");
    setPickerOpen(false);
  }

  const triggerLabel =
    selectedIds.size === 0
      ? `Choose ${GRANTEE_TYPE_LABELS[granteeType].toLowerCase()}…`
      : `${selectedIds.size} selected`;

  return (
    <div className="space-y-2 rounded-md border border-dashed p-2">
      <div className="flex items-center gap-2">
        <Select
          value={granteeType}
          onValueChange={(v) => changeType(v as GranteeType)}
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
          <Popover open={pickerOpen} onOpenChange={setPickerOpen}>
            <PopoverTrigger className="flex h-8 min-w-0 flex-1 items-center justify-between gap-1.5 rounded-lg border border-input bg-transparent px-2.5 text-sm outline-none transition-colors focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50">
              <span
                className={
                  selectedIds.size === 0
                    ? "truncate text-muted-foreground"
                    : "truncate"
                }
              >
                {triggerLabel}
              </span>
              <ChevronDown className="size-4 shrink-0 text-muted-foreground" />
            </PopoverTrigger>
            <PopoverContent align="start" className="w-72 p-0">
              <div className="border-b p-2">
                <Input
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                  placeholder={`Search ${GRANTEE_TYPE_LABELS[
                    granteeType
                  ].toLowerCase()}s…`}
                  className="h-8"
                />
              </div>
              <div className="max-h-60 overflow-y-auto p-1">
                {filtered.length === 0 && (
                  <p className="px-2 py-3 text-sm text-muted-foreground">
                    {idOptions.length === 0 ? "None available" : "No matches"}
                  </p>
                )}
                {filtered.map((o) => {
                  const already = isExisting(o.id);
                  const checked = already || selectedIds.has(o.id);
                  return (
                    <label
                      key={o.id}
                      className={
                        already
                          ? "flex cursor-default items-center gap-2 rounded-md px-2 py-1.5 text-sm opacity-60"
                          : "flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent"
                      }
                    >
                      <Checkbox
                        checked={checked}
                        disabled={already}
                        onCheckedChange={() => toggle(o.id)}
                      />
                      <span className="min-w-0 flex-1 truncate">{o.name}</span>
                      {already && (
                        <span className="shrink-0 text-xs text-muted-foreground">
                          added
                        </span>
                      )}
                    </label>
                  );
                })}
              </div>
            </PopoverContent>
          </Popover>
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
        <Button size="sm" className="ml-auto" disabled={!canAdd} onClick={add}>
          <Plus className="size-4" />
          {needsId && addableIds.length > 1
            ? `Add ${addableIds.length}`
            : "Add"}
        </Button>
      </div>
      {workspaceDuplicate && (
        <p className="text-xs text-muted-foreground">
          The whole workspace already has access here — change its role above.
        </p>
      )}
    </div>
  );
}
