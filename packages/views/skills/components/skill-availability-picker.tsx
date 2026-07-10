"use client";

import { useMemo, useState } from "react";
import { Globe2, Layers3, Loader2, Lock } from "lucide-react";
import type {
  SkillAvailabilityMode,
  Space,
} from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { PickerItem, PropertyPicker } from "../../issues/components/pickers";
import { useT } from "../../i18n";

export interface SkillAvailabilityChange {
  availability_mode: SkillAvailabilityMode;
  availability_space_ids: string[];
}

export function SkillAvailabilityPicker({
  mode,
  selectedSpaceIds,
  spaces,
  canEdit,
  saving = false,
  onChange,
}: {
  mode: SkillAvailabilityMode;
  selectedSpaceIds: string[];
  spaces: Space[];
  canEdit: boolean;
  saving?: boolean;
  onChange: (change: SkillAvailabilityChange) => void | Promise<void>;
}) {
  const { t } = useT("skills");
  const [open, setOpen] = useState(false);
  const [draftMode, setDraftMode] = useState<SkillAvailabilityMode>(mode);
  const [draftSelected, setDraftSelected] = useState(
    () => new Set(selectedSpaceIds),
  );

  const activeSpaces = useMemo(
    () => spaces.filter((space) => !space.archived_at),
    [spaces],
  );
  const knownSpaceIds = useMemo(() => new Set(spaces.map((space) => space.id)), [spaces]);
  const unavailableIds = [...draftSelected].filter((id) => !knownSpaceIds.has(id));
  const archivedSelected = spaces.filter(
    (space) => !!space.archived_at && draftSelected.has(space.id),
  );
  const invalidSelected = unavailableIds.length > 0 || archivedSelected.length > 0;
  const activeSelectedCount = activeSpaces.filter((space) =>
    selectedSpaceIds.includes(space.id),
  ).length;

  const summary =
    mode === "private"
      ? t(($) => $.availability.private_title)
      : mode === "selected_spaces"
        ? t(($) => $.availability.spaces_count, { count: activeSelectedCount })
        : t(($) => $.availability.workspace_title);
  const SummaryIcon = mode === "private" ? Lock : mode === "selected_spaces" ? Layers3 : Globe2;

  if (!canEdit) {
    return (
      <span className="inline-flex items-center gap-1 text-xs text-muted-foreground">
        <SummaryIcon className="size-3" aria-hidden />
        {summary}
      </span>
    );
  }

  const toggleSpace = (spaceId: string, checked: boolean) => {
    const next = new Set(draftSelected);
    if (checked) next.add(spaceId);
    else next.delete(spaceId);
    setDraftSelected(next);
    setDraftMode("selected_spaces");
  };

  const canApply =
    !saving &&
    (draftMode !== "selected_spaces" ||
      (draftSelected.size > 0 && !invalidSelected));

  const apply = async () => {
    if (!canApply) return;
    try {
      await onChange({
        availability_mode: draftMode,
        availability_space_ids:
          draftMode === "selected_spaces" ? [...draftSelected] : [],
      });
      setOpen(false);
    } catch {
      // The caller owns error presentation; keep the picker open for retry.
    }
  };

  return (
    <PropertyPicker
      open={open}
      onOpenChange={(nextOpen) => {
        setOpen(nextOpen);
        if (nextOpen) {
          setDraftMode(mode);
          setDraftSelected(new Set(selectedSpaceIds));
        }
      }}
      width="w-auto min-w-[18rem]"
      align="end"
      tooltip={t(($) => $.availability.tooltip)}
      triggerRender={
        <button
          type="button"
          className="inline-flex h-7 items-center gap-1.5 rounded-md border border-input bg-background px-2 text-xs text-foreground transition-colors hover:bg-accent"
          aria-label={t(($) => $.availability.tooltip)}
        />
      }
      trigger={
        <>
          <SummaryIcon className="size-3 text-muted-foreground" aria-hidden />
          <span className="truncate">{summary}</span>
        </>
      }
    >
      <PickerItem
        selected={draftMode === "private"}
        onClick={() => setDraftMode("private")}
      >
        <Lock className="size-3.5 text-muted-foreground" aria-hidden />
        <div className="text-left">
          <div className="font-medium">{t(($) => $.availability.private_title)}</div>
          <div className="text-xs text-muted-foreground">
            {t(($) => $.availability.private_description)}
          </div>
        </div>
      </PickerItem>

      <div className="mt-1 border-t pt-1">
        <button
          type="button"
          onClick={() => setDraftMode("selected_spaces")}
          className={`flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent ${
            draftMode === "selected_spaces" ? "bg-accent/60" : ""
          }`}
        >
          <Layers3 className="size-3.5 text-muted-foreground" aria-hidden />
          <div className="text-left">
            <div className="font-medium">{t(($) => $.availability.spaces_title)}</div>
            <div className="text-xs text-muted-foreground">
              {t(($) => $.availability.spaces_description)}
            </div>
          </div>
        </button>
        <div className="max-h-48 overflow-y-auto px-1">
          {activeSpaces.map((space) => (
            <label
              key={space.id}
              className="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent"
            >
              <Checkbox
                checked={draftSelected.has(space.id)}
                onCheckedChange={(value) => toggleSpace(space.id, value === true)}
                aria-label={space.name}
              />
              <span className="min-w-0 flex-1 truncate">{space.name}</span>
              <span className="text-[10px] text-muted-foreground">{space.key}</span>
            </label>
          ))}
          {activeSpaces.length === 0 && (
            <p className="px-2 py-2 text-xs text-muted-foreground">
              {t(($) => $.availability.spaces_empty)}
            </p>
          )}
          {archivedSelected.map((space) => (
            <label key={space.id} className="flex items-center gap-2 px-2 py-1 text-xs text-muted-foreground">
              <Checkbox
                checked
                onCheckedChange={(value) => toggleSpace(space.id, value === true)}
                aria-label={space.name}
              />
              <span className="min-w-0 flex-1 truncate line-through">{space.name}</span>
              <span>{t(($) => $.availability.archived)}</span>
            </label>
          ))}
          {unavailableIds.map((id) => (
            <label key={id} className="flex items-center gap-2 px-2 py-1 text-xs text-muted-foreground">
              <Checkbox
                checked
                onCheckedChange={(value) => toggleSpace(id, value === true)}
                aria-label={t(($) => $.availability.unavailable)}
              />
              <span className="line-through">{t(($) => $.availability.unavailable)}</span>
            </label>
          ))}
        </div>
      </div>

      <div className="mt-1 border-t pt-1">
        <PickerItem
          selected={draftMode === "workspace"}
          onClick={() => setDraftMode("workspace")}
        >
          <Globe2 className="size-3.5 text-muted-foreground" aria-hidden />
          <div className="text-left">
            <div className="font-medium">{t(($) => $.availability.workspace_title)}</div>
            <div className="text-xs text-muted-foreground">
              {t(($) => $.availability.workspace_description)}
            </div>
          </div>
        </PickerItem>
      </div>

      {draftMode === "selected_spaces" && draftSelected.size === 0 && (
        <p className="mx-1 mt-1 text-xs text-amber-700 dark:text-amber-400">
          {t(($) => $.availability.select_required)}
        </p>
      )}
      {draftMode === "selected_spaces" && invalidSelected && (
        <p className="mx-1 mt-1 text-xs text-amber-700 dark:text-amber-400">
          {t(($) => $.availability.remove_unavailable)}
        </p>
      )}
      <p className="mx-1 mt-1 border-t px-1 pt-2 text-[11px] text-muted-foreground">
        {t(($) => $.availability.access_note)}
      </p>
      <div className="mt-2 flex justify-end gap-2 border-t px-1 pt-2">
        <Button type="button" variant="ghost" size="sm" onClick={() => setOpen(false)}>
          {t(($) => $.availability.cancel)}
        </Button>
        <Button type="button" size="sm" disabled={!canApply} onClick={() => void apply()}>
          {saving && <Loader2 className="size-3 animate-spin" aria-hidden />}
          {t(($) => $.availability.apply)}
        </Button>
      </div>
    </PropertyPicker>
  );
}
