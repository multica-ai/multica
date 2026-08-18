"use client";

import { useState } from "react";
import { FolderKanban } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { projectListOptions } from "@multica/core/projects/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import type { UpdateIssueRequest } from "@multica/core/types";
import { ProjectIcon } from "./project-icon";
import {
  PropertyPicker,
  PickerItem,
  PickerEmpty,
  PICKER_TRIGGER_CLASS,
} from "../../issues/components/pickers/property-picker";
import { matchesPinyin } from "../../editor/extensions/pinyin-match";
import { useT } from "../../i18n";

export function ProjectPicker({
  projectId,
  onUpdate,
  triggerRender,
  align = "start",
  defaultOpen = false,
  open: controlledOpen,
  onOpenChange,
  disabled = false,
}: {
  projectId: string | null;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
  triggerRender?: React.ReactElement;
  align?: "start" | "center" | "end";
  /** Open the dropdown on first mount. Used by progressive-disclosure
   *  sidebars so a newly-added field immediately enters edit state. */
  defaultOpen?: boolean;
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  /** Read-only lock. When true the trigger is disabled and out of the tab
   *  order and the menu can never open, so no project-context mutation can
   *  fire — pointer OR keyboard. Clearing lives inside the menu, so locking
   *  the menu locks clearing too. Callers that must freeze the selection
   *  during a transient window (an in-flight chat send) pass this. */
  disabled?: boolean;
}) {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const current = projects.find((p) => p.id === projectId);
  const [filter, setFilter] = useState("");
  // Normalize to an always-boolean controlled `open`, matching the other
  // pickers (status/priority/assignee/labels). Base UI latches a controlled
  // `open={true}` — a later `undefined` does NOT close it — so callers wiring
  // `open={cond ? true : undefined}` (create-issue dialog) would otherwise
  // leave the popup stuck open after selecting a project.
  const [internalOpen, setInternalOpen] = useState(defaultOpen);
  // A disabled picker can never be open, and no interaction may reopen it.
  const open = disabled ? false : controlledOpen ?? internalOpen;
  const setOpen = disabled ? () => {} : onOpenChange ?? setInternalOpen;

  // Client-side filter: substring match plus pinyin so Chinese project names
  // are reachable by latin input (e.g. "sjtmh" → "数据透明化").
  const query = filter.trim().toLowerCase();
  const filtered = projects.filter(
    (p) => p.title.toLowerCase().includes(query) || matchesPinyin(p.title, query),
  );

  // Callers that bring their own trigger (create pill, chat pill, autopilot
  // card, table cell) take over the trigger entirely.
  const resolvedTriggerRender = triggerRender ?? (
    <button type="button" disabled={disabled} className={PICKER_TRIGGER_CLASS} />
  );

  return (
    <div className="inline-flex min-w-0">
      <PropertyPicker
        open={open}
        onOpenChange={setOpen}
        width="w-52"
        align={align}
        searchable
        searchPlaceholder={t(($) => $.picker.search_placeholder)}
        onSearchChange={setFilter}
        triggerRender={resolvedTriggerRender}
        trigger={
          current ? (
            <>
              <ProjectIcon project={current} size="sm" />
              <span className="truncate">{current.title}</span>
            </>
          ) : (
            <>
              <FolderKanban className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
              <span className="truncate">{t(($) => $.picker.no_project)}</span>
            </>
          )
        }
      >
        {/* "No project" — always the first row, search active or not, and the
            only clear entry now that the pill carries no inline ×. Mirrors
            the unassigned row in the assignee picker. */}
        <PickerItem
          emptyValue
          selected={!projectId}
          onClick={() => {
            onUpdate({ project_id: null });
            setOpen(false);
          }}
        >
          <FolderKanban className="h-3.5 w-3.5 text-muted-foreground" />
          <span className="text-muted-foreground">{t(($) => $.picker.no_project)}</span>
        </PickerItem>

        {filtered.map((p) => (
          <PickerItem
            key={p.id}
            selected={p.id === projectId}
            onClick={() => {
              onUpdate({ project_id: p.id });
              setOpen(false);
            }}
          >
            <ProjectIcon project={p} size="sm" />
            <span className="truncate">{p.title}</span>
          </PickerItem>
        ))}

        {projects.length === 0 && (
          <div className="px-2 py-1.5 text-caption text-muted-foreground">{t(($) => $.picker.empty)}</div>
        )}
        {projects.length > 0 && filtered.length === 0 && query && <PickerEmpty />}
      </PropertyPicker>
    </div>
  );
}
