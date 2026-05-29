"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronDown, ChevronRight, Plus, Trash2, UserPlus } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import {
  rolesListOptions,
  roleAssignmentsOptions,
  useCreateRole,
  useDeleteRole,
  useAssignRole,
  useUnassignRole,
  type CerebroRole,
} from "../../core";

// Roles tab — FIR-2130. A role is a named permission subject; members and
// agents are assigned to it, and role-scoped grants (authored in the Matrix
// tab) apply to everyone holding the role.
export function RolesTab({ wsId }: { wsId: string }) {
  const { data: roles = [], isLoading } = useQuery(rolesListOptions(wsId));
  const createRole = useCreateRole();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [expanded, setExpanded] = useState<string | null>(null);

  const canCreate = name.trim().length > 0 && !createRole.isPending;

  return (
    <div className="flex flex-1 min-h-0 flex-col overflow-y-auto p-4">
      <form
        className="flex max-w-3xl flex-wrap items-end gap-2"
        onSubmit={(event) => {
          event.preventDefault();
          if (!canCreate) return;
          createRole.mutate(
            { name: name.trim(), description: description.trim() || null },
            {
              onSuccess: () => {
                setName("");
                setDescription("");
              },
            },
          );
        }}
      >
        <label className="flex-1">
          <span className="mb-1 block text-xs font-medium text-muted-foreground">Role name</span>
          <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. Release manager" />
        </label>
        <label className="flex-1">
          <span className="mb-1 block text-xs font-medium text-muted-foreground">Description</span>
          <Input value={description} onChange={(e) => setDescription(e.target.value)} placeholder="Optional" />
        </label>
        <Button type="submit" disabled={!canCreate}>
          <Plus className="size-4" />
          {createRole.isPending ? "Creating…" : "New role"}
        </Button>
      </form>

      {createRole.isError && (
        <div className="mt-3 max-w-3xl rounded-md border border-destructive/40 bg-destructive/5 p-2 text-sm text-destructive">
          {createRole.error instanceof Error ? createRole.error.message : "Could not create role"}
        </div>
      )}

      <div className="mt-5 flex flex-col gap-2">
        {isLoading && <div className="text-sm text-muted-foreground">Loading roles…</div>}
        {!isLoading && roles.length === 0 && (
          <div className="rounded-md border border-dashed p-6 text-center text-sm text-muted-foreground">
            No roles yet. Create one above, then grant it capabilities in the Matrix tab.
          </div>
        )}
        {roles.map((role) => (
          <RoleRow
            key={role.id}
            wsId={wsId}
            role={role}
            expanded={expanded === role.id}
            onToggle={() => setExpanded(expanded === role.id ? null : role.id)}
          />
        ))}
      </div>
    </div>
  );
}

function RoleRow({
  wsId,
  role,
  expanded,
  onToggle,
}: {
  wsId: string;
  role: CerebroRole;
  expanded: boolean;
  onToggle: () => void;
}) {
  const deleteRole = useDeleteRole();
  return (
    <div className="rounded-md border">
      <div className="flex items-center gap-2 px-3 py-2">
        <button
          type="button"
          onClick={onToggle}
          className="flex flex-1 items-center gap-2 text-left"
          aria-expanded={expanded}
        >
          {expanded ? <ChevronDown className="size-4" /> : <ChevronRight className="size-4" />}
          <span className="text-sm font-medium">{role.name}</span>
          {role.description && (
            <span className="truncate text-xs text-muted-foreground">{role.description}</span>
          )}
        </button>
        <Button
          variant="ghost"
          size="sm"
          onClick={() => {
            if (confirm(`Delete role "${role.name}"? Its grants and assignments are removed.`)) {
              deleteRole.mutate(role.id);
            }
          }}
          disabled={deleteRole.isPending}
          aria-label={`Delete role ${role.name}`}
        >
          <Trash2 className="size-4 text-muted-foreground" />
        </Button>
      </div>
      {expanded && <RoleAssignments wsId={wsId} roleId={role.id} />}
    </div>
  );
}

function RoleAssignments({ wsId, roleId }: { wsId: string; roleId: string }) {
  const { data: assignments = [], isLoading } = useQuery(roleAssignmentsOptions(wsId, roleId));
  const assign = useAssignRole();
  const unassign = useUnassignRole();
  const [subjectType, setSubjectType] = useState("member");
  const [subjectId, setSubjectId] = useState("");

  const canAssign = subjectId.trim().length > 0 && !assign.isPending;

  return (
    <div className="border-t px-3 py-3">
      <div className="mb-2 text-xs font-medium uppercase text-muted-foreground">Assigned to</div>
      {isLoading && <div className="text-sm text-muted-foreground">Loading…</div>}
      {!isLoading && assignments.length === 0 && (
        <div className="text-sm text-muted-foreground">No members or agents assigned yet.</div>
      )}
      <div className="flex flex-col gap-1">
        {assignments.map((a) => (
          <div
            key={`${a.subject_type}:${a.subject_id}`}
            className="flex items-center gap-2 text-sm"
          >
            <span className="rounded-sm bg-muted px-1.5 py-0.5 text-[10px] uppercase text-muted-foreground">
              {a.subject_type}
            </span>
            <span className="flex-1 truncate text-xs" title={a.subject_id}>
              {a.subject_display_name ?? a.subject_id}
            </span>
            <Button
              variant="ghost"
              size="sm"
              onClick={() =>
                unassign.mutate({ roleId, subjectType: a.subject_type, subjectId: a.subject_id })
              }
              disabled={unassign.isPending}
              aria-label="Remove assignment"
            >
              <Trash2 className="size-3.5 text-muted-foreground" />
            </Button>
          </div>
        ))}
      </div>

      <form
        className="mt-3 flex flex-wrap items-end gap-2"
        onSubmit={(event) => {
          event.preventDefault();
          if (!canAssign) return;
          assign.mutate(
            { roleId, subjectType, subjectId: subjectId.trim() },
            { onSuccess: () => setSubjectId("") },
          );
        }}
      >
        <label>
          <span className="mb-1 block text-xs font-medium text-muted-foreground">Type</span>
          <Select value={subjectType} onValueChange={(v) => setSubjectType(v ?? "member")}>
            <SelectTrigger size="sm">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="member">member</SelectItem>
              <SelectItem value="agent">agent</SelectItem>
            </SelectContent>
          </Select>
        </label>
        <label className="flex-1">
          <span className="mb-1 block text-xs font-medium text-muted-foreground">
            {subjectType === "agent" ? "Agent ID" : "Member user ID"}
          </span>
          <Input value={subjectId} onChange={(e) => setSubjectId(e.target.value)} placeholder="UUID" />
        </label>
        <Button type="submit" variant="outline" size="sm" disabled={!canAssign}>
          <UserPlus className="size-4" />
          Assign
        </Button>
      </form>
      {assign.isError && (
        <div className="mt-2 text-xs text-destructive">
          {assign.error instanceof Error ? assign.error.message : "Could not assign"}
        </div>
      )}
    </div>
  );
}
