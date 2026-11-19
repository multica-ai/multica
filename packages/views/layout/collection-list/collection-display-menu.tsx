"use client";

import type { ComponentProps, ReactNode } from "react";
import { ArrowDown, ArrowUp, ChevronDown } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
import { Switch } from "@multica/ui/components/ui/switch";
import { useT } from "../../i18n";
import { CollectionDisplayButton } from "../collection-page";
import type { CollectionColumn, CollectionSort } from "./collection-list";

export interface CollectionGrouping<G extends string = string> {
  value: G;
  options: readonly { value: G; label: string }[];
  onChange: (value: G) => void;
}

/**
 * Display settings derived from the list's own column definitions: every
 * column with a `sortKey` is a sort option, every `hideable` column gets a
 * switch. `extra` renders above them all (e.g. a view-mode toggle), then the
 * optional grouping.
 */
export function CollectionDisplayMenu<T>({
  columns,
  hiddenColumns,
  onToggleColumn,
  sort,
  showColumns = true,
  extra,
  grouping,
  triggerIcon,
}: {
  columns: readonly CollectionColumn<T>[];
  hiddenColumns?: readonly string[];
  onToggleColumn?: (key: string) => void;
  sort?: CollectionSort;
  /** Off when the current view has no columns (e.g. a card grid). */
  showColumns?: boolean;
  extra?: ReactNode;
  grouping?: CollectionGrouping;
  triggerIcon?: ComponentProps<typeof CollectionDisplayButton>["icon"];
}) {
  const { t } = useT("layout");
  const sortOptions = columns.flatMap((col) =>
    col.sortKey && col.header ? [{ value: col.sortKey, label: col.header }] : [],
  );
  const toggles =
    showColumns && onToggleColumn
      ? columns.filter((col, i) => i > 0 && col.hideable && col.header)
      : [];
  const activeSort = sort ? sortOptions.find((o) => o.value === sort.field) : undefined;
  const activeGroup = grouping?.options.find((o) => o.value === grouping.value);

  return (
    <Popover>
      <PopoverTrigger render={<CollectionDisplayButton icon={triggerIcon} />} />
      <PopoverContent align="end" className="w-64 p-0">
        {extra ? <div className="border-b px-3 py-2.5 last:border-b-0">{extra}</div> : null}
        {grouping ? (
          <div className="border-b px-3 py-2.5 last:border-b-0">
            <span className="text-caption font-medium text-muted-foreground">
              {t(($) => $.collection.group_by)}
            </span>
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <Button
                    variant="outline"
                    size="sm"
                    className="mt-2 w-full justify-between text-caption"
                  >
                    {activeGroup?.label ?? grouping.value}
                    <ChevronDown className="size-3 text-muted-foreground" />
                  </Button>
                }
              />
              <DropdownMenuContent align="start" className="w-auto">
                <DropdownMenuRadioGroup
                  value={grouping.value}
                  onValueChange={(value) => grouping.onChange(value as string)}
                >
                  {grouping.options.map((option) => (
                    <DropdownMenuRadioItem key={option.value} value={option.value}>
                      {option.label}
                    </DropdownMenuRadioItem>
                  ))}
                </DropdownMenuRadioGroup>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        ) : null}
        {sort && sortOptions.length > 0 ? (
          <div className="border-b px-3 py-2.5 last:border-b-0">
            <span className="text-caption font-medium text-muted-foreground">
              {t(($) => $.collection.sort_by)}
            </span>
            <div className="mt-2 flex items-center gap-1.5">
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={
                    <Button
                      variant="outline"
                      size="sm"
                      className="flex-1 justify-between text-caption"
                    >
                      {activeSort?.label ?? sort.field}
                      <ChevronDown className="size-3 text-muted-foreground" />
                    </Button>
                  }
                />
                <DropdownMenuContent align="start" className="w-auto">
                  <DropdownMenuRadioGroup
                    value={sort.field}
                    onValueChange={(value) =>
                      (sort.onFieldChange ?? sort.onToggle)(value as string)
                    }
                  >
                    {sortOptions.map((option) => (
                      <DropdownMenuRadioItem key={option.value} value={option.value}>
                        {option.label}
                      </DropdownMenuRadioItem>
                    ))}
                  </DropdownMenuRadioGroup>
                </DropdownMenuContent>
              </DropdownMenu>
              {sort.onDirectionChange ? (
                <IconButton
                  variant="outline"
                  size="icon-sm"
                  onClick={() =>
                    sort.onDirectionChange?.(sort.direction === "asc" ? "desc" : "asc")
                  }
                  label={
                    sort.direction === "asc"
                      ? t(($) => $.collection.direction_asc)
                      : t(($) => $.collection.direction_desc)
                  }
                >
                  {sort.direction === "asc" ? (
                    <ArrowUp className="size-3.5" />
                  ) : (
                    <ArrowDown className="size-3.5" />
                  )}
                </IconButton>
              ) : null}
            </div>
          </div>
        ) : null}
        {toggles.length > 0 ? (
          <div className="px-3 py-2.5">
            <span className="text-caption font-medium text-muted-foreground">
              {t(($) => $.collection.section_columns)}
            </span>
            <div className="mt-2 space-y-2">
              {toggles.map((col) => (
                <label
                  key={col.key}
                  className="flex cursor-pointer items-center justify-between"
                >
                  <span className="text-body">{col.header}</span>
                  <Switch
                    size="sm"
                    checked={!hiddenColumns?.includes(col.key)}
                    onCheckedChange={() => onToggleColumn?.(col.key)}
                  />
                </label>
              ))}
            </div>
          </div>
        ) : null}
      </PopoverContent>
    </Popover>
  );
}
