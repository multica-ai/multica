"use client";

// Cerebro workflow v2b (FIR-1550) — status rows for menu surfaces.
//
// The upstream 3-dot / right-click action menu renders a status submenu. With
// no status model in scope (every non-project surface, or a project without a
// model) this hook returns the 7 base statuses unchanged, so the menu renders
// exactly as upstream does. When the surrounding project has a model, it
// returns the model's custom statuses (no dedup by base) plus the uncovered
// base statuses underneath — the same ordering and pinning contract the shared
// StatusPicker uses, so both status entry points agree.

import { useMemo } from "react";
import type { IssueStatus } from "@multica/core/types";
import { ALL_STATUSES } from "@multica/core/issues/config";
import { useStatusPresentation } from "./status-presentation";

export interface StatusMenuRow {
  /** Stable React key for the menu item. */
  id: string;
  /** Upstream base status to send on select. */
  baseStatus: IssueStatus;
  /**
   * Custom status key to pin alongside the base. Undefined for a plain base
   * pick — the server auto-resolver then picks the first matching custom
   * status (or clears the sidecar when none maps / no model).
   */
  customStatusKey?: string;
  /** Model label, or undefined → caller falls back to the upstream i18n label. */
  label?: string;
  /** Model color (hex), or undefined → caller uses the default status color. */
  color?: string;
  /** True when this row is the issue's current selection. */
  selected: boolean;
}

export function useStatusMenuRows(
  status: IssueStatus,
  customStatusKey?: string | null,
): StatusMenuRow[] {
  const pres = useStatusPresentation();

  return useMemo<StatusMenuRow[]>(() => {
    // No model in scope: the plain 7 base statuses, upstream behavior.
    if (!pres.hasModel) {
      return ALL_STATUSES.map((s) => ({
        id: `base:${s}`,
        baseStatus: s,
        selected: !customStatusKey && s === status,
      }));
    }

    const rows: StatusMenuRow[] = [];
    for (const row of pres.customStatuses ?? []) {
      rows.push({
        id: `custom:${row.key}`,
        baseStatus: row.baseStatus,
        customStatusKey: row.key,
        label: row.label,
        color: row.color,
        selected: customStatusKey === row.key,
      });
    }
    const covered = new Set(pres.orderedBaseStatuses ?? []);
    for (const s of ALL_STATUSES) {
      if (covered.has(s)) continue;
      rows.push({
        id: `base:${s}`,
        baseStatus: s,
        label: pres.getLabel(s),
        color: pres.getColor(s),
        selected: !customStatusKey && s === status,
      });
    }
    return rows;
  }, [pres, status, customStatusKey]);
}
