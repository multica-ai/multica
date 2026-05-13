"use client";

import { Plus, Search, X } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
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
  GRANT_CLASSIFICATIONS,
  permissionsKeys,
  usePermissionsStore,
  type GrantClassification,
  type GrantStatus,
  type GrantSubjectType,
} from "../../core";

const SUBJECT_TYPES: GrantSubjectType[] = [
  "actor",
  "agent",
  "member",
  "group",
  "role",
  "workspace_default",
  "anonymous",
];

const STATUSES: GrantStatus[] = [
  "active",
  "pending_approval",
  "expired",
  "revoked",
];

// Resource types track JEH-978's A/C/F tables. Kept as a flat list so an
// admin can pick from the same menu the API understands.
const RESOURCE_TYPES = [
  "issue",
  "project",
  "agent",
  "runtime",
  "repo",
  "mcp_server",
  "api_token",
  "secret",
  "webhook",
  "skill",
];

interface GrantsToolbarProps {
  wsId: string;
  onCreate: () => void;
}

export function GrantsToolbar({ wsId, onCreate }: GrantsToolbarProps) {
  const search = usePermissionsStore((s) => s.search);
  const subjectType = usePermissionsStore((s) => s.subjectType);
  const resourceType = usePermissionsStore((s) => s.resourceType);
  const status = usePermissionsStore((s) => s.status);
  const classification = usePermissionsStore((s) => s.classification);
  const setSubjectType = usePermissionsStore((s) => s.setSubjectType);
  const setResourceType = usePermissionsStore((s) => s.setResourceType);
  const setStatus = usePermissionsStore((s) => s.setStatus);
  const setClassification = usePermissionsStore((s) => s.setClassification);
  const setSearch = usePermissionsStore((s) => s.setSearch);
  const reset = usePermissionsStore((s) => s.reset);
  const qc = useQueryClient();

  const hasFilter =
    !!subjectType ||
    !!resourceType ||
    !!status ||
    !!classification ||
    search.trim().length > 0;

  return (
    <div className="flex flex-wrap items-center gap-2 border-b bg-background/50 px-4 py-3">
      <div className="relative">
        <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
        <Input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Søg kapabilitet / ressource…"
          className="h-8 w-64 pl-8 text-sm"
          aria-label="Søg grants"
        />
      </div>

      <FilterSelect
        label="Subjekt"
        value={subjectType ?? ""}
        onValueChange={(v) => setSubjectType((v as GrantSubjectType) || null)}
        options={SUBJECT_TYPES.map((t) => ({ value: t, label: formatSubject(t) }))}
      />

      <FilterSelect
        label="Ressource"
        value={resourceType ?? ""}
        onValueChange={(v) => setResourceType(v || null)}
        options={RESOURCE_TYPES.map((t) => ({ value: t, label: t }))}
      />

      <FilterSelect
        label="Status"
        value={status ?? ""}
        onValueChange={(v) => setStatus((v as GrantStatus) || null)}
        options={STATUSES.map((s) => ({ value: s, label: formatStatus(s) }))}
      />

      <FilterSelect
        label="Klassifikation"
        value={classification ?? ""}
        onValueChange={(v) =>
          setClassification((v as GrantClassification) || null)
        }
        options={GRANT_CLASSIFICATIONS.map((c) => ({ value: c, label: c }))}
      />

      {hasFilter && (
        <Button
          variant="ghost"
          size="sm"
          onClick={reset}
          className="text-muted-foreground"
        >
          <X className="size-3" />
          Nulstil
        </Button>
      )}

      <div className="ml-auto flex items-center gap-2">
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={() => {
            qc.invalidateQueries({ queryKey: permissionsKeys.all(wsId) });
          }}
        >
          Genindlæs
        </Button>
        <Button type="button" size="sm" onClick={onCreate}>
          <Plus className="size-3" />
          Nyt grant
        </Button>
      </div>
    </div>
  );
}

interface FilterSelectProps {
  label: string;
  value: string;
  onValueChange: (value: string) => void;
  options: { value: string; label: string }[];
}

function FilterSelect({
  label,
  value,
  onValueChange,
  options,
}: FilterSelectProps) {
  return (
    <Select
      value={value || "__any__"}
      onValueChange={(v) => onValueChange(!v || v === "__any__" ? "" : v)}
    >
      <SelectTrigger size="sm" className="h-8 w-40 text-xs" aria-label={label}>
        <SelectValue placeholder={label} />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="__any__">Alle {label.toLowerCase()}</SelectItem>
        {options.map((opt) => (
          <SelectItem key={opt.value} value={opt.value}>
            {opt.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

function formatSubject(t: GrantSubjectType): string {
  switch (t) {
    case "actor":
      return "Actor";
    case "agent":
      return "Agent";
    case "member":
      return "Member";
    case "group":
      return "Group";
    case "role":
      return "Role";
    case "workspace_default":
      return "Workspace default";
    case "anonymous":
      return "Anonymous";
    default:
      return t;
  }
}

function formatStatus(s: GrantStatus): string {
  switch (s) {
    case "active":
      return "Aktiv";
    case "pending_approval":
      return "Afventer approval";
    case "expired":
      return "Udløbet";
    case "revoked":
      return "Tilbagekaldt";
    default:
      return s;
  }
}
