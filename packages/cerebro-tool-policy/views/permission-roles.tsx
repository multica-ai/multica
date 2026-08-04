"use client";

import { useEffect, useMemo, useState } from "react";
import { Archive, Loader2, Plus, ShieldCheck, Trash2 } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { useCurrentMember } from "@multica/core/permissions";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  permissionRoleAssignmentsOptions,
  permissionRolesOptions,
  toolPolicyTableOptions,
  useArchivePermissionRole,
  useAssignPermissionRole,
  useCreatePermissionRole,
  useUnassignPermissionRole,
  useUpdatePermissionRole,
  withRoleDecision,
  type DraftRoleDecision,
  type PermissionRole,
  type RoleDecision,
  type RolePermissions,
} from "../core";
import {
  isFileWriteToolKey,
  presentCapabilityRows,
} from "./capability-presentation";

export function PermissionRoles({ workspaceId }: { workspaceId: string }) {
  const roles = useQuery(permissionRolesOptions(workspaceId));
  const { role: memberRole } = useCurrentMember(workspaceId);
  const canEdit = memberRole === "owner" || memberRole === "admin";
  const [editorRole, setEditorRole] = useState<PermissionRole | "new" | null>(null);
  const [selectedRoleId, setSelectedRoleId] = useState("");
  const selectedRole = roles.data?.find((role) => role.id === selectedRoleId) ?? null;

  useEffect(() => {
    if (!selectedRoleId && roles.data?.[0]) setSelectedRoleId(roles.data[0].id);
  }, [roles.data, selectedRoleId]);

  return (
    <section className="overflow-hidden rounded-lg border bg-card" aria-labelledby="permission-roles-heading">
      <div className="flex flex-wrap items-start justify-between gap-3 border-b p-4">
        <div>
          <div className="flex items-center gap-2">
            <ShieldCheck className="h-4 w-4 text-muted-foreground" />
            <h3 id="permission-roles-heading" className="text-sm font-semibold">Permission profiles</h3>
          </div>
          <p className="mt-1 max-w-2xl text-xs text-muted-foreground">
            Reusable, versioned sets of Allow, Ask and Deny decisions. Assign a
            profile to several agents or members when they should share the same
            access. For one agent, use that agent&apos;s Tools page instead.
          </p>
        </div>
        {canEdit ? (
          <Button size="sm" onClick={() => setEditorRole("new")}>
            <Plus className="mr-1.5 h-3.5 w-3.5" /> Create profile
          </Button>
        ) : null}
      </div>

      {roles.isLoading ? (
        <div className="flex items-center gap-2 p-4 text-sm text-muted-foreground">
          <Loader2 className="h-4 w-4 animate-spin" /> Loading profiles…
        </div>
      ) : roles.isError ? (
        <p className="p-4 text-sm text-destructive">Permission profiles could not be loaded.</p>
      ) : (roles.data ?? []).length === 0 ? (
        <p className="p-4 text-sm text-muted-foreground">
          No Permission profiles yet. Create one when several agents or members
          should share the same access rules.
        </p>
      ) : (
        <div className="grid min-h-64 md:grid-cols-[16rem_minmax(0,1fr)]">
          <div className="flex gap-2 overflow-x-auto border-b p-2 md:block md:border-b-0 md:border-r">
            {(roles.data ?? []).map((role) => (
              <button
                key={role.id}
                type="button"
                onClick={() => setSelectedRoleId(role.id)}
                className={`w-48 shrink-0 rounded-md px-3 py-2 text-left text-sm transition-colors md:mb-1 md:w-full ${
                  selectedRoleId === role.id ? "bg-accent" : "hover:bg-muted"
                }`}
              >
                <span className="block truncate font-medium">{role.name}</span>
                <span className="text-xs text-muted-foreground">
                  Version {role.version} · {Object.keys(role.permissions).length} permissions
                </span>
              </button>
            ))}
          </div>
          <div className="min-w-0 p-4">
            {selectedRole ? (
              <RoleDetails
                workspaceId={workspaceId}
                role={selectedRole}
                canEdit={canEdit}
                onEdit={() => setEditorRole(selectedRole)}
              />
            ) : null}
          </div>
        </div>
      )}

      {editorRole ? (
        <RoleEditor
          workspaceId={workspaceId}
          role={editorRole === "new" ? null : editorRole}
          open
          onOpenChange={(open) => {
            if (!open) setEditorRole(null);
          }}
          onSaved={(role) => {
            setSelectedRoleId(role.id);
            setEditorRole(null);
          }}
        />
      ) : null}
    </section>
  );
}

function RoleDetails({
  workspaceId,
  role,
  canEdit,
  onEdit,
}: {
  workspaceId: string;
  role: PermissionRole;
  canEdit: boolean;
  onEdit: () => void;
}) {
  const assignments = useQuery(permissionRoleAssignmentsOptions(workspaceId, role.id));
  const agents = useQuery(agentListOptions(workspaceId));
  const members = useQuery(memberListOptions(workspaceId));
  const assign = useAssignPermissionRole(workspaceId, role.id);
  const unassign = useUnassignPermissionRole(workspaceId, role.id);
  const archive = useArchivePermissionRole(workspaceId);
  const [actor, setActor] = useState("");
  const [expiresAt, setExpiresAt] = useState("");

  const actorOptions = useMemo(() => [
    ...(agents.data ?? []).filter((agent) => !agent.archived_at).map((agent) => ({
      value: `agent:${agent.id}`,
      label: agent.name,
      group: "Agents",
    })),
    ...(members.data ?? []).map((member) => ({
      value: `member:${member.user_id}`,
      label: member.name || member.email,
      group: "Members",
    })),
  ], [agents.data, members.data]);

  const previewContext = useMemo(() => {
    const [type, id] = actor.split(":");
    if (!id) return {};
    if (type === "agent") {
      const runtimeId = agents.data?.find((agent) => agent.id === id)?.runtime_id;
      return { agentId: id, runtimeId };
    }
    return { userId: id };
  }, [actor, agents.data]);
  const preview = useQuery({
    ...toolPolicyTableOptions(workspaceId, previewContext),
    enabled: !!actor,
  });
  const roleKeys = Object.keys(role.permissions);
  const previewRows = (preview.data ?? []).filter((row) => roleKeys.includes(row.tool_key));

  async function submitAssignment() {
    const [subject_type, subject_id] = actor.split(":") as ["agent" | "member", string];
    if (!subject_id) return;
    await assign.mutateAsync({
      subject_type,
      subject_id,
      expires_at: expiresAt ? new Date(expiresAt).toISOString() : null,
    });
    setExpiresAt("");
  }

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <div className="flex items-center gap-2">
            <h4 className="font-medium">{role.name}</h4>
            <Badge variant="secondary">Version {role.version}</Badge>
          </div>
          {role.description ? (
            <p className="mt-1 text-sm text-muted-foreground">{role.description}</p>
          ) : null}
        </div>
        {canEdit ? (
          <div className="flex gap-2">
            <Button size="sm" variant="outline" onClick={onEdit}>Edit</Button>
            <Button
              size="sm"
              variant="outline"
              disabled={archive.isPending}
              onClick={() => {
                if (window.confirm(`Archive ${role.name}? Assigned agents and members will stop inheriting this profile on new tasks.`)) {
                  archive.mutate(role.id);
                }
              }}
            >
              <Archive className="mr-1.5 h-3.5 w-3.5" /> Archive
            </Button>
          </div>
        ) : null}
      </div>

      <div>
        <h5 className="mb-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
          Permission profile
        </h5>
        {roleKeys.length === 0 ? (
          <p className="text-sm text-muted-foreground">This profile currently inherits every permission.</p>
        ) : (
          <div className="flex flex-wrap gap-2">
            {presentRolePermissionKeys(roleKeys).map(({ key, label, keys }) => {
              const decisions = keys.flatMap((permissionKey) =>
                (role.permissions[permissionKey] ?? []).map((rule) => rule.setting),
              );
              return (
                <span key={key} className="inline-flex items-center gap-1.5 rounded-md border px-2 py-1 text-xs">
                  <span className={keys.length > 1 ? "" : "font-mono"}>{label}</span>
                  <DecisionBadge decision={mostRestrictive(decisions)} />
                </span>
              );
            })}
          </div>
        )}
      </div>

      <div>
        <h5 className="mb-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
          Assignments
        </h5>
        <div className="space-y-2">
          {(assignments.data ?? []).map((assignment) => (
            <div key={`${assignment.subject_type}:${assignment.subject_id}`} className="flex flex-wrap items-center gap-2 rounded-md border px-3 py-2 text-sm">
              <Badge variant="outline">{assignment.subject_type}</Badge>
              <span className="min-w-0 flex-1 truncate">
                {assignment.subject_display_name || assignment.subject_id}
              </span>
              {assignment.expires_at ? (
                <span className="text-xs text-muted-foreground">
                  Expires {new Date(assignment.expires_at).toLocaleString()}
                </span>
              ) : (
                <span className="text-xs text-muted-foreground">No expiry</span>
              )}
              <Button
                size="sm"
                variant="ghost"
                onClick={() => setActor(`${assignment.subject_type}:${assignment.subject_id}`)}
              >
                View access
              </Button>
              {canEdit ? (
                <Button
                  size="icon-sm"
                  variant="ghost"
                  aria-label={`Remove ${assignment.subject_display_name || "assignment"}`}
                  onClick={() => unassign.mutate({
                    subject_type: assignment.subject_type,
                    subject_id: assignment.subject_id,
                  })}
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </Button>
              ) : null}
            </div>
          ))}
          {!assignments.isLoading && (assignments.data ?? []).length === 0 ? (
            <p className="text-sm text-muted-foreground">This profile is not assigned yet.</p>
          ) : null}
        </div>

        {canEdit ? (
          <div className="mt-3 grid gap-2 md:grid-cols-[minmax(12rem,1fr)_12rem_auto]">
            <Select value={actor} onValueChange={(value) => setActor(value ?? "")}>
              <SelectTrigger aria-label="Profile assignee">
                <SelectValue placeholder="Select an agent or member" />
              </SelectTrigger>
              <SelectContent>
                {["Agents", "Members"].map((group) => (
                  <div key={group}>
                    <p className="px-2 py-1 text-xs font-semibold text-muted-foreground">{group}</p>
                    {actorOptions.filter((option) => option.group === group).map((option) => (
                      <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>
                    ))}
                  </div>
                ))}
              </SelectContent>
            </Select>
            <Input
              type="datetime-local"
              aria-label="Assignment expiry"
              value={expiresAt}
              onChange={(event) => setExpiresAt(event.target.value)}
            />
            <Button disabled={!actor || assign.isPending} onClick={submitAssignment}>
              {assign.isPending ? <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" /> : null}
              Assign
            </Button>
          </div>
        ) : null}
      </div>

      {actor ? (
        <div>
          <h5 className="mb-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
            Effective access
          </h5>
          {preview.isLoading ? (
            <p className="text-sm text-muted-foreground">Resolving the live permission chain…</p>
          ) : (
            <div className="space-y-1">
              {previewRows.map((row) => (
                <div key={`${row.tool_key}:${row.resource_pattern}`} className="grid gap-2 rounded-md border px-3 py-2 text-xs md:grid-cols-[minmax(10rem,1fr)_auto_minmax(12rem,2fr)]">
                  <span>{row.title || row.tool_key}</span>
                  <DecisionBadge decision={row.effective.setting} />
                  <span className="text-muted-foreground">{row.effective.reason || "Resolved by the permission engine"}</span>
                </div>
              ))}
            </div>
          )}
        </div>
      ) : null}
    </div>
  );
}

function RoleEditor({
  workspaceId,
  role,
  open,
  onOpenChange,
  onSaved,
}: {
  workspaceId: string;
  role: PermissionRole | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSaved: (role: PermissionRole) => void;
}) {
  const catalog = useQuery(toolPolicyTableOptions(workspaceId, {}));
  const create = useCreatePermissionRole(workspaceId);
  const update = useUpdatePermissionRole(workspaceId);
  const [name, setName] = useState(role?.name ?? "");
  const [description, setDescription] = useState(role?.description ?? "");
  const [search, setSearch] = useState("");
  const [permissions, setPermissions] = useState<RolePermissions>(role?.permissions ?? {});
  const saving = create.isPending || update.isPending;

  const presentedRows = useMemo(() => {
    const needle = search.trim().toLowerCase();
    return presentCapabilityRows(catalog.data ?? []).filter((presented) =>
      !needle ||
      `${presented.title} ${presented.rows
        .map((row) => `${row.title} ${row.tool_key} ${row.category}`)
        .join(" ")}`
        .toLowerCase()
        .includes(needle),
    );
  }, [catalog.data, search]);

  async function save() {
    if (!name.trim()) return;
    const saved = role
      ? await update.mutateAsync({ id: role.id, name: name.trim(), description, permissions })
      : await create.mutateAsync({ name: name.trim(), description, permissions });
    onSaved(saved);
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[calc(100dvh-1rem)] w-[calc(100vw-1rem)] max-w-3xl flex-col overflow-hidden sm:max-h-[85vh]">
        <DialogHeader>
          <DialogTitle>{role ? `Edit ${role.name}` : "Create profile"}</DialogTitle>
          <DialogDescription>
            Save one shared set of access rules. Every assigned agent or member
            receives the same version through the permission engine.
          </DialogDescription>
        </DialogHeader>
        <div className="grid gap-3 sm:grid-cols-2">
          <div className="space-y-1.5">
            <Label htmlFor="role-name">Name</Label>
            <Input id="role-name" value={name} onChange={(event) => setName(event.target.value)} />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="role-description">Description</Label>
            <Textarea id="role-description" rows={1} value={description} onChange={(event) => setDescription(event.target.value)} />
          </div>
        </div>
        <div className="min-h-0 flex-1 space-y-2 overflow-hidden">
          <Input
            aria-label="Search permissions"
            placeholder="Search permissions"
            value={search}
            onChange={(event) => setSearch(event.target.value)}
          />
          <div className="max-h-[48vh] overflow-y-auto rounded-md border">
            {presentedRows.map((presented) => {
              const decisions = presented.rows.map((row) =>
                capabilityWideDecision(permissions[row.tool_key]),
              );
              const decision = decisions.every((value) => value === decisions[0])
                ? decisions[0]!
                : "mixed";
              const hasScoped = presented.rows.some((row) =>
                (permissions[row.tool_key] ?? []).some((rule) => rule.resource_pattern),
              );
              return (
                <div key={presented.key} className="grid items-center gap-2 border-b px-3 py-2 last:border-b-0 sm:grid-cols-[minmax(0,1fr)_9rem]">
                  <div className="min-w-0">
                    <p className="truncate text-sm font-medium">{presented.title}</p>
                    <p className="truncate text-xs text-muted-foreground">
                      {presented.rows.length > 1
                        ? presented.rows
                            .map((row) => row.tool_key.replace(/^tools:/i, ""))
                            .join(" · ")
                        : `${presented.rows[0]!.category} · ${presented.rows[0]!.tool_key}`}
                      {hasScoped ? " · scoped rules retained" : ""}
                    </p>
                  </div>
                  <Select
                    value={decision}
                    onValueChange={(value) =>
                      setPermissions((current) =>
                        presented.rows.reduce(
                          (next, row) =>
                            withRoleDecision(
                              next,
                              row.tool_key,
                              (value ?? "inherit") as DraftRoleDecision,
                            ),
                          current,
                        ),
                      )
                    }
                  >
                    <SelectTrigger aria-label={`${presented.title} decision`}>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {decision === "mixed" ? (
                        <SelectItem value="mixed" disabled>Mixed</SelectItem>
                      ) : null}
                      <SelectItem value="inherit">Inherit</SelectItem>
                      <SelectItem value="allow">Allow</SelectItem>
                      <SelectItem value="ask">Ask</SelectItem>
                      <SelectItem value="deny">Deny</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
              );
            })}
          </div>
        </div>
        <div className="flex justify-end gap-2">
          <Button variant="outline" onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button disabled={!name.trim() || saving} onClick={save}>
            {saving ? <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" /> : null}
            Save version
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function capabilityWideDecision(rules: RolePermissions[string] | undefined): DraftRoleDecision {
  return rules?.find((rule) => rule.resource_pattern === "")?.setting ?? "inherit";
}

function presentRolePermissionKeys(keys: string[]) {
  const fileWriteKeys = keys.filter(isFileWriteToolKey);
  const combineFileWrite = fileWriteKeys.length >= 2;
  const presented: Array<{ key: string; label: string; keys: string[] }> = [];
  let insertedFileWrite = false;
  for (const key of keys) {
    if (combineFileWrite && isFileWriteToolKey(key)) {
      if (!insertedFileWrite) {
        presented.push({
          key: "file-write",
          label: "Create and edit files",
          keys: fileWriteKeys,
        });
        insertedFileWrite = true;
      }
      continue;
    }
    presented.push({ key, label: key, keys: [key] });
  }
  return presented;
}

function mostRestrictive(decisions: RoleDecision[]): RoleDecision {
  if (decisions.includes("deny")) return "deny";
  if (decisions.includes("ask")) return "ask";
  return "allow";
}

function DecisionBadge({ decision }: { decision: RoleDecision }) {
  const classes = decision === "allow"
    ? "bg-emerald-500/15 text-emerald-600"
    : decision === "ask"
      ? "bg-amber-500/15 text-amber-600"
      : "bg-destructive/15 text-destructive";
  return <span className={`rounded px-1.5 py-0.5 text-[10px] font-semibold uppercase ${classes}`}>{decision}</span>;
}
