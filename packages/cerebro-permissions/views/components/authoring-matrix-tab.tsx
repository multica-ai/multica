"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { Check, ShieldQuestion } from "lucide-react";
import {
  CAPABILITY_CATALOG,
  DEFAULT_GRANTS_FILTER,
  grantsListOptions,
  rolesListOptions,
  useCreatePersonaGrant,
  useUpdatePersonaGrant,
  useDeletePersonaGrant,
  type PersonaGrant,
} from "../../core";

type CellState = "none" | "allow" | "approval";

interface SubjectRow {
  key: string;
  type: string;
  id: string | null;
  label: string;
}

// Authoring matrix — FIR-2130. Tools-UI-style grid of subject × capability.
// Each cell is a workspace-wide (resource "*") grant for that subject and
// capability. Clicking a cell cycles none → allow → requires-approval → none,
// creating, updating, or deleting the underlying grant. The "Effective" tab
// shows how these resolve per actor including the winning override layer.
export function AuthoringMatrixTab({ wsId }: { wsId: string }) {
  const matrixFilter = { ...DEFAULT_GRANTS_FILTER, limit: 200 };
  const { data: grantPage } = useQuery(grantsListOptions(wsId, matrixFilter));
  const { data: roles = [] } = useQuery(rolesListOptions(wsId));

  const createGrant = useCreatePersonaGrant();
  const updateGrant = useUpdatePersonaGrant();
  const deleteGrant = useDeletePersonaGrant();
  const busy = createGrant.isPending || updateGrant.isPending || deleteGrant.isPending;

  const rows: SubjectRow[] = useMemo(
    () => [
      { key: "workspace_default:", type: "workspace_default", id: null, label: "Everyone (workspace default)" },
      ...roles.map((r) => ({ key: `role:${r.id}`, type: "role", id: r.id, label: r.name })),
    ],
    [roles],
  );

  // Index workspace-wide ("*") active grants by subject + capability.
  const grantIndex = useMemo(() => {
    const map = new Map<string, PersonaGrant>();
    for (const g of grantPage?.items ?? []) {
      if (g.resource.pattern !== "*") continue;
      if (g.status !== "active") continue;
      map.set(cellKey(g.subject.type, g.subject.id ?? null, g.capability), g);
    }
    return map;
  }, [grantPage]);

  function cellFor(row: SubjectRow, capability: string): { state: CellState; grant?: PersonaGrant } {
    const g = grantIndex.get(cellKey(row.type, row.id, capability));
    if (!g) return { state: "none" };
    return { state: g.approval_required ? "approval" : "allow", grant: g };
  }

  function onCellClick(row: SubjectRow, capability: string) {
    if (busy) return;
    const { state, grant } = cellFor(row, capability);
    if (state === "none") {
      createGrant.mutate({
        subject: { type: row.type, id: row.id, display_name: row.label },
        resource: { type: "workspace", pattern: "*" },
        capability,
        classification_ceiling: "unclassified",
        approval_required: false,
      });
    } else if (state === "allow" && grant) {
      updateGrant.mutate({ id: grant.id, body: { approval_required: true } });
    } else if (state === "approval" && grant) {
      deleteGrant.mutate(grant.id);
    }
  }

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <div className="flex items-center gap-4 border-b px-4 py-2 text-xs text-muted-foreground">
        <span>Click a cell to cycle:</span>
        <LegendSwatch state="none" /> <span>none</span>
        <LegendSwatch state="allow" /> <span>allow</span>
        <LegendSwatch state="approval" /> <span>requires approval</span>
        {busy && <span className="ml-auto animate-pulse">Saving…</span>}
      </div>
      <div className="flex-1 min-h-0 overflow-auto">
        <table className="border-separate border-spacing-0 text-sm">
          <thead>
            <tr>
              <th className="sticky left-0 top-0 z-20 border-b border-r bg-background px-3 py-2 text-left font-medium">
                Subject
              </th>
              {CAPABILITY_CATALOG.map((cap) => (
                <th
                  key={cap.key}
                  className="sticky top-0 z-10 border-b bg-background px-2 py-2 text-left align-bottom"
                  title={cap.description}
                >
                  <div className="w-28 whitespace-normal text-xs font-medium leading-tight">
                    {cap.label}
                  </div>
                  <div className="mt-0.5 font-mono text-[10px] text-muted-foreground">{cap.key}</div>
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => (
              <tr key={row.key}>
                <th className="sticky left-0 z-10 border-b border-r bg-background px-3 py-2 text-left font-normal">
                  <div className="flex items-center gap-2">
                    <span className="rounded-sm bg-muted px-1.5 py-0.5 text-[10px] uppercase text-muted-foreground">
                      {row.type === "workspace_default" ? "default" : "role"}
                    </span>
                    <span className="truncate">{row.label}</span>
                  </div>
                </th>
                {CAPABILITY_CATALOG.map((cap) => {
                  const { state } = cellFor(row, cap.key);
                  return (
                    <td key={cap.key} className="border-b p-1 text-center">
                      <MatrixCell
                        state={state}
                        disabled={busy}
                        onClick={() => onCellClick(row, cap.key)}
                        label={`${row.label} · ${cap.label}`}
                      />
                    </td>
                  );
                })}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function MatrixCell({
  state,
  disabled,
  onClick,
  label,
}: {
  state: CellState;
  disabled: boolean;
  onClick: () => void;
  label: string;
}) {
  const classes =
    state === "allow"
      ? "bg-emerald-500/15 text-emerald-600 border-emerald-500/30"
      : state === "approval"
        ? "bg-amber-500/15 text-amber-600 border-amber-500/30"
        : "bg-transparent text-muted-foreground border-border hover:bg-muted";
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      aria-label={`${label}: ${state}`}
      data-state={state}
      className={`flex size-7 items-center justify-center rounded-sm border ${classes} disabled:opacity-50`}
    >
      {state === "allow" && <Check className="size-4" />}
      {state === "approval" && <ShieldQuestion className="size-4" />}
    </button>
  );
}

function LegendSwatch({ state }: { state: CellState }) {
  const classes =
    state === "allow"
      ? "bg-emerald-500/15 border-emerald-500/30"
      : state === "approval"
        ? "bg-amber-500/15 border-amber-500/30"
        : "bg-transparent border-border";
  return <span className={`inline-block size-3 rounded-sm border ${classes}`} />;
}

function cellKey(subjectType: string, subjectId: string | null, capability: string): string {
  return `${subjectType}:${subjectId ?? ""}:${capability}`;
}
