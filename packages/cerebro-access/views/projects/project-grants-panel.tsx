"use client";

// Per-project access grants editor (FIR-2125). Replaces ProjectAccessTab with
// role-based grants (viewer/editor/full_access) that cascade to sub-projects.
// Mirrors FolderAccessEditor but targets the project_grant backend.
//   This project (view=direct)  — grants set directly on this project. Editable.
//   Inherited   (view=effective) — grants that cascade in from a parent project. Read-only.
import * as React from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronDown, Plus, Trash2 } from "lucide-react";
import {
  Tabs,
  TabsList,
  TabsTrigger,
  TabsContent,
} from "@multica/ui/components/ui/tabs";
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
import {
  projectGrantsOptions,
} from "@multica/cerebro-collections/queries";
import {
  useUpsertProjectGrant,
  useRemoveProjectGrant,
} from "@multica/cerebro-collections/mutations";
import {
  GRANTEE_TYPE_LABELS,
  ROLE_LABELS,
  useGranteeDirectory,
} from "@multica/cerebro-collections/views/use-grantee-directory";
import type { GranteeType, GrantRole } from "@multica/cerebro-collections/types";

const ROLE_VALUES: GrantRole[] = ["viewer", "editor", "full_access"];
const GRANTEE_TYPE_VALUES: GranteeType[] = [
  "group",
  "member",
  "workspace",
  "agent",
  "runtime",
];

export function ProjectGrantsPanel({ projectId }: { projectId: string }) {
  const wsId = useWorkspaceId();
  const directory = useGranteeDirectory(true);

  const directQuery = useQuery(
    projectGrantsOptions(wsId, projectId, "direct"),
  );
  const effectiveQuery = useQuery(
    projectGrantsOptions(wsId, projectId, "effective"),
  );

  const upsert = useUpsertProjectGrant();
  const remove = useRemoveProjectGrant();

  const direct = directQuery.data ?? [];
  const inherited = (effectiveQuery.data ?? []).filter((g) => !g.is_direct);

  const [tab, setTab] = React.useState<string | null>(null);
  const loaded = directQuery.data !== undefined;
  const resolvedTab =
    tab ??
    (loaded && direct.length === 0 && inherited.length > 0
      ? "inherited"
      : "direct");

  return (
    <Tabs value={resolvedTab} onValueChange={setTab}>
      <TabsList className="w-full">
        <TabsTrigger value="direct" className="flex-1">
          This project
        </TabsTrigger>
        <TabsTrigger value="inherited" className="flex-1">
          Inherited
        </TabsTrigger>
      </TabsList>

      <TabsContent value="direct" className="space-y-3 pt-2">
        {direct.length === 0 && (
          <p className="text-sm text-muted-foreground">
            No access set directly on this project yet.
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
                  project_id: projectId,
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
                  project_id: projectId,
                  grantee_type: g.grantee_type,
                  grantee_id: g.grantee_id,
                })
              }
            >
              <Trash2 className="size-4" />
            </Button>
          </div>
        ))}

        <AddProjectGrantRow
          projectId={projectId}
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
            Nothing inherited from a parent project.
          </p>
        )}
        {inherited.map((g) => (
          <div
            key={`${g.grantee_type}:${g.grantee_id ?? "ws"}:${g.source_project_id}`}
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
  );
}

function AddProjectGrantRow({
  projectId,
  directory,
  existing,
  onAdd,
}: {
  projectId: string;
  directory: ReturnType<typeof useGranteeDirectory>;
  existing: string[];
  onAdd: (input: {
    project_id: string;
    grantee_type: GranteeType;
    grantee_id: string | null;
    role: GrantRole;
  }) => void;
}) {
  const [granteeType, setGranteeType] = React.useState<GranteeType>("group");
  const [selectedIds, setSelectedIds] = React.useState<Set<string>>(new Set());
  const [role, setRole] = React.useState<GrantRole>("viewer");
  const [pickerOpen, setPickerOpen] = React.useState(false);
  const [search, setSearch] = React.useState("");

  const needsId = granteeType !== "workspace";
  const idOptions =
    granteeType === "workspace" ? [] : directory.options[granteeType];

  const isExisting = (id: string) => existing.includes(`${granteeType}:${id}`);

  const filtered = idOptions.filter((o) =>
    o.name.toLowerCase().includes(search.trim().toLowerCase()),
  );

  const workspaceDuplicate =
    granteeType === "workspace" && existing.includes("workspace:ws");
  const addableIds = [...selectedIds].filter((id) => !isExisting(id));
  const canAdd = needsId ? addableIds.length > 0 : !workspaceDuplicate;

  function changeType(next: GranteeType) {
    setGranteeType(next);
    setSelectedIds(new Set());
    setSearch("");
  }

  function handleAdd() {
    if (!canAdd) return;
    if (granteeType === "workspace") {
      onAdd({ project_id: projectId, grantee_type: "workspace", grantee_id: null, role });
    } else {
      for (const id of addableIds) {
        onAdd({ project_id: projectId, grantee_type: granteeType, grantee_id: id, role });
      }
    }
    setSelectedIds(new Set());
    setPickerOpen(false);
  }

  return (
    <div className="flex items-center gap-2 pt-1">
      <Select value={granteeType} onValueChange={(v) => changeType(v as GranteeType)}>
        <SelectTrigger className="w-32 shrink-0">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {GRANTEE_TYPE_VALUES.map((t) => (
            <SelectItem key={t} value={t}>
              {GRANTEE_TYPE_LABELS[t]}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      {needsId ? (
        <Popover open={pickerOpen} onOpenChange={setPickerOpen}>
          <PopoverTrigger
            render={
              <Button variant="outline" className="flex-1 justify-between" />
            }
          >
            {selectedIds.size === 0
              ? `Pick ${GRANTEE_TYPE_LABELS[granteeType].toLowerCase()}…`
              : `${selectedIds.size} selected`}
            <ChevronDown className="ml-2 size-4 shrink-0 opacity-50" />
          </PopoverTrigger>
          <PopoverContent className="w-64 p-0" align="start">
            <div className="border-b p-2">
              <Input
                placeholder="Search…"
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                className="h-7 text-xs"
              />
            </div>
            <div className="max-h-48 overflow-y-auto p-1">
              {filtered.length === 0 && (
                <p className="px-2 py-4 text-center text-xs text-muted-foreground">
                  No results
                </p>
              )}
              {filtered.map((o) => (
                <label
                  key={o.id}
                  className="flex cursor-pointer items-center gap-2 rounded px-2 py-1.5 text-sm hover:bg-muted"
                >
                  <Checkbox
                    checked={selectedIds.has(o.id)}
                    disabled={isExisting(o.id)}
                    onCheckedChange={(checked) => {
                      setSelectedIds((prev) => {
                        const next = new Set(prev);
                        if (checked) next.add(o.id);
                        else next.delete(o.id);
                        return next;
                      });
                    }}
                  />
                  <span className="flex-1 truncate">{o.name}</span>
                  {isExisting(o.id) && (
                    <span className="text-xs text-muted-foreground">added</span>
                  )}
                </label>
              ))}
            </div>
          </PopoverContent>
        </Popover>
      ) : (
        <span className="flex-1 truncate text-sm text-muted-foreground">
          Everyone in the workspace
        </span>
      )}

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
        size="icon-sm"
        variant="ghost"
        onClick={handleAdd}
        disabled={!canAdd}
        aria-label="Add grant"
      >
        <Plus className="size-4" />
      </Button>
    </div>
  );
}
