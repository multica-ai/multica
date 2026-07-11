"use client";

import { useMemo, useState } from "react";
import { Layers3, ChevronsUpDown } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import type { Space } from "@multica/core/types";
import { matchesPinyin } from "../../editor/extensions/pinyin-match";
import { SpaceIcon } from "../../spaces/components/space-icon";
import {
  PickerEmpty,
  PickerItem,
  PickerSection,
  PropertyPicker,
} from "../../issues/components/pickers/property-picker";
import { useT } from "../../i18n";

/** New-chat context control. null is the explicit All-spaces selection. */
export function ChatContextPicker({
  spaces,
  value,
  onChange,
}: {
  spaces: Space[];
  value: string | null;
  onChange: (spaceId: string | null) => void;
}) {
  const { t } = useT("chat");
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState("");
  const selected = spaces.find((space) => space.id === value) ?? null;
  const query = filter.trim().toLowerCase();
  const filtered = useMemo(
    () =>
      spaces.filter(
        (space) =>
          !query ||
          space.name.toLowerCase().includes(query) ||
          space.key.toLowerCase().includes(query) ||
          matchesPinyin(space.name, query),
      ),
    [query, spaces],
  );

  const choose = (spaceId: string | null) => {
    onChange(spaceId);
    setOpen(false);
    setFilter("");
  };

  const label = selected?.name ?? t(($) => $.context.all_spaces);

  return (
    <PropertyPicker
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) setFilter("");
      }}
      width="w-72"
      align="start"
      side="top"
      searchable
      searchPlaceholder={t(($) => $.context.search_spaces)}
      onSearchChange={setFilter}
      triggerRender={
        <Button
          variant="ghost"
          size="sm"
          className="h-7 max-w-full gap-1.5 px-2 text-xs font-medium text-muted-foreground hover:text-foreground"
          aria-label={t(($) => $.context.picker_aria, { selection: label })}
        />
      }
      trigger={
        <>
          {selected ? (
            <SpaceIcon space={selected} className="size-3.5" />
          ) : (
            <Layers3 className="size-3.5" aria-hidden />
          )}
          <span className="truncate">{label}</span>
          <ChevronsUpDown className="size-3 opacity-60" aria-hidden />
        </>
      }
    >
      {!query || t(($) => $.context.all_spaces).toLowerCase().includes(query) ? (
        <PickerSection label={t(($) => $.context.scope_label)}>
          <PickerItem selected={value === null} onClick={() => choose(null)}>
            <span className="flex size-6 items-center justify-center rounded-md bg-muted">
              <Layers3 className="size-3.5" aria-hidden />
            </span>
            <span className="min-w-0 flex-1">
              <span className="block truncate">{t(($) => $.context.all_spaces)}</span>
              <span className="block truncate text-xs text-muted-foreground">
                {t(($) => $.context.all_spaces_description, { count: spaces.length })}
              </span>
            </span>
          </PickerItem>
        </PickerSection>
      ) : null}

      {filtered.length > 0 ? (
        <PickerSection label={t(($) => $.context.spaces_label)}>
          {filtered.map((space) => (
            <PickerItem
              key={space.id}
              selected={value === space.id}
              onClick={() => choose(space.id)}
            >
              <SpaceIcon space={space} className="size-6" />
              <span className="min-w-0 flex-1 truncate">{space.name}</span>
              <span className="shrink-0 text-xs text-muted-foreground">
                {space.key}
              </span>
            </PickerItem>
          ))}
        </PickerSection>
      ) : query ? (
        <PickerEmpty />
      ) : null}
    </PropertyPicker>
  );
}

export function ChatContextLabel({
  spaces,
  spaceId,
}: {
  spaces: Space[];
  spaceId?: string | null;
}) {
  const { t } = useT("chat");
  const space = spaces.find((item) => item.id === spaceId);
  return (
    <span className="inline-flex min-w-0 items-center gap-1 truncate">
      {space ? (
        <SpaceIcon space={space} className="size-3" />
      ) : (
        <Layers3 className="size-3" aria-hidden />
      )}
      <span className="truncate">{space?.name ?? t(($) => $.context.all_spaces)}</span>
    </span>
  );
}
