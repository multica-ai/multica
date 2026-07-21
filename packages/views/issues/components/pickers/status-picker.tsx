"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import type { IssueStatus, StatusDetail, UpdateIssueRequest } from "@multica/core/types";
import { ALL_STATUSES, STATUS_CONFIG, statusThemeForColor } from "@multica/core/issues/config";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueStatusListOptions } from "@multica/core/issue-statuses";
import { StatusIcon } from "../status-icon";
import { PropertyPicker, PickerItem } from "./property-picker";
import { localizableStatusKey } from "../../utils/status-label";
import { useT } from "../../../i18n";

// Category presentation order, so custom statuses slot in next to the built-ins
// that share their machine semantics rather than being appended at the end.
const CATEGORY_ORDER = ["backlog", "todo", "in_progress", "done", "cancelled"] as const;

export function StatusPicker({
  status,
  statusDetail,
  onUpdate,
  trigger: customTrigger,
  triggerRender,
  open: controlledOpen,
  onOpenChange: controlledOnOpenChange,
  align,
  mode = "id",
}: {
  /**
   * The currently-selected status, used to check the matching row. `null`
   * means "no single current value" (e.g. a batch selection spanning several
   * statuses) — no row is checked. Single-issue callers always pass a concrete
   * status.
   */
  status: IssueStatus | null;
  /**
   * The issue's resolved catalog entry, when it has one. Lets the picker check
   * the right row when several statuses share a Category (and therefore share
   * the legacy `status` token).
   */
  statusDetail?: StatusDetail | null;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
  trigger?: React.ReactNode;
  triggerRender?: React.ReactElement;
  open?: boolean;
  onOpenChange?: (v: boolean) => void;
  align?: "start" | "center" | "end";
  /**
   * "id" (default) emits `status_id`, which is the only way to reach a CUSTOM
   * status. "legacy" emits the legacy `status` token and offers only the
   * built-ins — for callers whose endpoint does not accept status_id yet (the
   * create-issue dialog). Remove once the create path resolves status_id too.
   */
  mode?: "id" | "legacy";
}) {
  const [internalOpen, setInternalOpen] = useState(false);
  const open = controlledOpen ?? internalOpen;
  const setOpen = controlledOnOpenChange ?? setInternalOpen;
  const { t } = useT("issues");
  const wsId = useWorkspaceId();

  const { data: catalog = [] } = useQuery(issueStatusListOptions(wsId));

  // Active statuses, Category-ordered then by position. An empty catalog means a
  // server predating custom statuses (or a workspace not seeded yet) — fall back
  // to the 7 legacy tokens so the picker always works.
  const options = useMemo(() => {
    if (mode === "legacy") return [];
    const active = catalog.filter((s) => !s.archived);
    if (active.length === 0) return [];
    return [...active].sort((a, b) => {
      const byCategory =
        CATEGORY_ORDER.indexOf(a.category as (typeof CATEGORY_ORDER)[number]) -
        CATEGORY_ORDER.indexOf(b.category as (typeof CATEGORY_ORDER)[number]);
      return byCategory !== 0 ? byCategory : a.position - b.position;
    });
  }, [catalog, mode]);

  const triggerContent =
    customTrigger ??
    (status != null ? (
      <>
        <StatusIcon
          status={status}
          detail={statusDetail ?? null}
          className="h-3.5 w-3.5 shrink-0"
        />
        <span className="truncate">{statusDetail?.name ?? t(($) => $.status[status])}</span>
      </>
    ) : null);

  return (
    <PropertyPicker
      open={open}
      onOpenChange={setOpen}
      width="w-52"
      align={align}
      triggerRender={triggerRender}
      trigger={triggerContent}
    >
      {options.length > 0
        ? options.map((s) => {
            // Match on the catalog id when the issue has one; otherwise fall back
            // to the legacy token so a not-yet-migrated issue still shows a check.
            const selected = statusDetail
              ? s.id === statusDetail.id
              : status != null && (s.system_key === status || s.category === status);
            return (
              <PickerItem
                key={s.id}
                selected={selected}
                hoverClassName={statusThemeForColor(s.color).hoverBg}
                onClick={() => {
                  onUpdate({ status_id: s.id });
                  setOpen(false);
                }}
              >
                <StatusIcon status={s.icon} icon={s.icon} color={s.color} className="h-3.5 w-3.5" />
                <span className="truncate">
                  {(() => {
                    const key = localizableStatusKey(s.system_key, s.name);
                    return key ? t(($) => $.status[key]) : s.name;
                  })()}
                </span>
              </PickerItem>
            );
          })
        : ALL_STATUSES.map((s) => {
            const c = STATUS_CONFIG[s];
            return (
              <PickerItem
                key={s}
                selected={s === status}
                hoverClassName={c.hoverBg}
                onClick={() => {
                  onUpdate({ status: s });
                  setOpen(false);
                }}
              >
                <StatusIcon status={s} className="h-3.5 w-3.5" />
                <span>{t(($) => $.status[s])}</span>
              </PickerItem>
            );
          })}
    </PropertyPicker>
  );
}
