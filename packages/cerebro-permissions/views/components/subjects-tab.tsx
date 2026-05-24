"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertCircle, Bot, ChevronDown, ChevronRight, Lock, Search, Users } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { toast } from "sonner";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Switch } from "@multica/ui/components/ui/switch";
import { cn } from "@multica/ui/lib/utils";
import {
  permissionsKeys,
  subjectsWithPermissionsOptions,
  updatePersonaGrant,
  type GrantSubjectType,
  type SubjectWithPermissions,
} from "../../core";

interface SubjectsTabProps {
  wsId: string;
}

const PAGE_SIZE = 50;

const SUBJECT_TYPE_OPTIONS: { value: GrantSubjectType | ""; label: string }[] = [
  { value: "", label: "All types" },
  { value: "member", label: "Members" },
  { value: "agent", label: "Agents" },
  { value: "group", label: "Groups" },
  { value: "role", label: "Roles" },
  { value: "workspace_default", label: "Workspace default" },
  { value: "anonymous", label: "Anonymous" },
];

export function SubjectsTab({ wsId }: SubjectsTabProps) {
  const [search, setSearch] = useState("");
  const [subjectType, setSubjectType] = useState<GrantSubjectType | "">("");
  const [offset, setOffset] = useState(0);

  const filter = {
    subjectType: subjectType || null,
    search,
    limit: PAGE_SIZE,
    offset,
  };

  const list = useQuery(subjectsWithPermissionsOptions(wsId, filter));
  const items = list.data?.items ?? [];
  const total = list.data?.total ?? 0;

  return (
    <div className="flex h-full flex-col">
      <div className="flex flex-wrap items-center gap-2 border-b bg-background/50 px-4 py-3">
        <div className="relative">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={search}
            onChange={(e) => { setSearch(e.target.value); setOffset(0); }}
            placeholder="Search subject…"
            className="h-8 w-56 pl-8 text-sm"
            aria-label="Search subjects"
          />
        </div>
        <Select
          value={subjectType || "__any__"}
          onValueChange={(v) => {
            setSubjectType(v === "__any__" ? "" : (v as GrantSubjectType));
            setOffset(0);
          }}
        >
          <SelectTrigger size="sm" className="h-8 w-44 text-xs" aria-label="Subject type">
            <SelectValue placeholder="Type" />
          </SelectTrigger>
          <SelectContent>
            {SUBJECT_TYPE_OPTIONS.map((o) => (
              <SelectItem key={o.value || "__any__"} value={o.value || "__any__"}>
                {o.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <div className="flex-1 overflow-y-auto">
        <SubjectList items={items} isLoading={list.isLoading} error={list.error} wsId={wsId} />
      </div>

      <div className="flex items-center justify-between border-t bg-background/50 px-4 py-2 text-xs text-muted-foreground">
        <span className="font-mono tabular-nums">
          {total === 0
            ? "0 subjects"
            : `${offset + 1}–${offset + items.length} of ${total}`}
        </span>
        <div className="flex gap-1">
          <Button
            variant="outline"
            size="sm"
            disabled={offset === 0}
            onClick={() => setOffset(Math.max(0, offset - PAGE_SIZE))}
          >
            Previous
          </Button>
          <Button
            variant="outline"
            size="sm"
            disabled={items.length === 0 || offset + items.length >= total}
            onClick={() => setOffset(offset + PAGE_SIZE)}
          >
            Next
          </Button>
        </div>
      </div>
    </div>
  );
}

function SubjectList({
  items,
  isLoading,
  error,
  wsId,
}: {
  items: SubjectWithPermissions[];
  isLoading: boolean;
  error: unknown;
  wsId: string;
}) {
  if (error) {
    return (
      <div className="m-4 flex items-start gap-3 rounded-md border border-destructive/40 bg-destructive/5 p-4 text-sm text-destructive">
        <AlertCircle className="mt-0.5 size-4 shrink-0" />
        <div className="min-w-0">
          <div className="font-medium">Couldn't load subjects</div>
          <div className="text-xs text-destructive/80 truncate">
            {error instanceof Error ? error.message : "unknown error"}
          </div>
        </div>
      </div>
    );
  }

  if (isLoading && items.length === 0) {
    return (
      <div className="space-y-1 p-4">
        {[0, 1, 2, 3].map((i) => (
          <div key={i} className="h-12 animate-pulse rounded bg-muted/60" />
        ))}
      </div>
    );
  }

  if (items.length === 0) {
    return (
      <div className="m-4 rounded-md border border-dashed p-12 text-center">
        <Users className="mx-auto size-8 text-muted-foreground/60" />
        <p className="mt-3 text-sm font-medium">No subjects yet</p>
        <p className="mt-1 text-xs text-muted-foreground">
          People, agents, and groups show up here as soon as they have at least one grant. Use the <strong>Permissions</strong> tab to create one.
        </p>
      </div>
    );
  }

  return (
    <ul className="divide-y">
      {items.map((s) => (
        <SubjectRow key={s.id} subject={s} wsId={wsId} />
      ))}
    </ul>
  );
}

function SubjectRow({ subject, wsId }: { subject: SubjectWithPermissions; wsId: string }) {
  const [expanded, setExpanded] = useState(false);

  return (
    <li>
      <button
        type="button"
        onClick={() => setExpanded((v) => !v)}
        className="flex w-full items-center gap-3 px-4 py-3 text-left text-sm transition-colors hover:bg-muted/40 focus-visible:outline-none focus-visible:bg-muted/40"
        aria-expanded={expanded}
      >
        {expanded ? (
          <ChevronDown className="size-3.5 shrink-0 text-muted-foreground" />
        ) : (
          <ChevronRight className="size-3.5 shrink-0 text-muted-foreground" />
        )}
        <SubjectIcon type={subject.type} />
        <span className="min-w-0 flex-1 truncate font-medium">
          {subject.display_name ?? subject.id}
        </span>
        <span className="shrink-0 rounded-sm bg-muted px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-muted-foreground">
          {subject.type}
        </span>
        {subject.pending_count > 0 && (
          <Badge variant="outline" className="shrink-0 text-[10px] bg-warning/10 text-warning border-warning/40">
            {subject.pending_count} pending
          </Badge>
        )}
        <span className="shrink-0 font-mono text-[11px] tabular-nums text-muted-foreground">
          {subject.permissions.length} grants
        </span>
      </button>

      {expanded && subject.permissions.length > 0 && (
        <div className="border-t bg-muted/20 px-4 py-2">
          <table className="w-full text-xs">
            <thead>
              <tr className="text-left text-[10px] uppercase tracking-wide text-muted-foreground">
                <th className="py-1 pr-3 font-medium">Capability</th>
                <th className="py-1 pr-3 font-medium">Resource</th>
                <th className="py-1 pr-3 font-medium">Status</th>
                <th className="py-1 font-medium">Requires approval</th>
              </tr>
            </thead>
            <tbody>
              {subject.permissions.map((p) => (
                <PermissionRow
                  key={p.grant_id}
                  wsId={wsId}
                  grantId={p.grant_id}
                  capability={p.capability}
                  resourcePattern={p.resource_pattern}
                  status={p.status}
                  approvalRequired={p.approval_required}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}

      {expanded && subject.permissions.length === 0 && (
        <div className="border-t bg-muted/20 px-4 py-2 text-xs text-muted-foreground">
          No active grants.
        </div>
      )}
    </li>
  );
}

function PermissionRow({
  wsId,
  grantId,
  capability,
  resourcePattern,
  status,
  approvalRequired,
}: {
  wsId: string;
  grantId: string;
  capability: string;
  resourcePattern: string;
  status: string;
  approvalRequired: boolean;
}) {
  const wsIdFromHook = useWorkspaceId();
  const qc = useQueryClient();
  const targetWsId = wsId || wsIdFromHook;

  const mutation = useMutation({
    mutationFn: (next: boolean) =>
      updatePersonaGrant(targetWsId, grantId, { approval_required: next }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: permissionsKeys.all(targetWsId) });
    },
    onError: (e) => {
      toast.error(e instanceof Error ? e.message : "Couldn't update grant");
    },
  });

  return (
    <tr className="border-t border-border/50">
      <td className="py-1.5 pr-3 font-mono">{capability}</td>
      <td className="py-1.5 pr-3 font-mono text-muted-foreground">{resourcePattern}</td>
      <td className="py-1.5 pr-3">
        <PermStatusBadge status={status} />
      </td>
      <td className="py-1.5">
        <div className="flex items-center gap-2">
          <Switch
            checked={approvalRequired}
            onCheckedChange={(next) => mutation.mutate(next)}
            disabled={mutation.isPending}
            aria-label={
              approvalRequired
                ? `Stop requiring approval for ${capability}`
                : `Require approval for ${capability}`
            }
          />
          {approvalRequired ? (
            <span className="inline-flex items-center gap-1 text-warning">
              <Lock className="size-3" />
              on
            </span>
          ) : (
            <span className="text-muted-foreground">off</span>
          )}
        </div>
      </td>
    </tr>
  );
}

function SubjectIcon({ type }: { type: string }) {
  if (type === "agent") return <Bot className="size-3.5 shrink-0 text-muted-foreground" />;
  return <Users className="size-3.5 shrink-0 text-muted-foreground" />;
}

function PermStatusBadge({ status }: { status: string }) {
  const tone =
    status === "active"
      ? "bg-success/10 text-success border-success/30"
      : status === "pending_approval"
        ? "bg-warning/10 text-warning border-warning/40"
        : status === "revoked" || status === "expired"
          ? "bg-muted text-muted-foreground border-border"
          : "bg-muted text-muted-foreground border-border";
  const label =
    status === "active"
      ? "active"
      : status === "pending_approval"
        ? "approval"
        : status === "revoked"
          ? "revoked"
          : status === "expired"
            ? "expired"
            : status;
  return (
    <span className={cn("inline-flex rounded-sm px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide border", tone)}>
      {label}
    </span>
  );
}
