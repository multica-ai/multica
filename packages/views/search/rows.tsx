"use client";

import type { ReactNode } from "react";
import { Check } from "@multica/ui/icons";
import { Command as CommandPrimitive } from "cmdk";
import { cn } from "@multica/ui/lib/utils";
import { ShortcutKeycaps } from "../common/shortcut-keycaps";
import { HighlightText } from "./highlight-text";
import { stripEllipsis } from "./matching";
import { usePalette } from "./palette-context";
import type { PaletteCommand, PaletteCommandGroup } from "./types";

// One heading treatment for every group. Headings go through cmdk's `heading`
// prop rather than a hand-rolled div: cmdk renders it into a
// [cmdk-group-heading] node and points the group's aria-labelledby at it, so a
// screen reader announces which group a row belongs to.
export const GROUP_CLASS =
  "px-2 py-1 [&_[cmdk-group-heading]]:flex [&_[cmdk-group-heading]]:min-w-0 [&_[cmdk-group-heading]]:items-center [&_[cmdk-group-heading]]:gap-1.5 [&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:pt-1.5 [&_[cmdk-group-heading]]:pb-1 [&_[cmdk-group-heading]]:text-caption [&_[cmdk-group-heading]]:font-medium [&_[cmdk-group-heading]]:text-muted-foreground";

export const ROW_CLASS =
  "group/row flex min-h-[var(--issue-row-height)] cursor-default select-none items-center gap-2.5 rounded-md px-2 py-1 text-body outline-none data-[disabled=true]:pointer-events-none data-[disabled=true]:opacity-50 data-selected:bg-accent";

export const ROW_ICON_CLASS = "size-4 shrink-0 text-muted-foreground";

export function PaletteGroup({
  heading,
  children,
  className,
}: {
  heading: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <CommandPrimitive.Group heading={heading} className={cn(GROUP_CLASS, className)}>
      {children}
    </CommandPrimitive.Group>
  );
}

/** Trailing muted text, e.g. a type label or "3 available". */
export function RowHint({ children }: { children: ReactNode }) {
  return <span className="ml-auto shrink-0 text-caption text-muted-foreground">{children}</span>;
}

export function RowCheck({ label }: { label?: string }) {
  return (
    <Check
      aria-label={label}
      aria-hidden={label ? undefined : true}
      className="ml-auto size-4 shrink-0 text-muted-foreground"
    />
  );
}

export function CommandRow({
  command,
  query,
  scope,
}: {
  command: PaletteCommand;
  query: string;
  /**
   * The group the row belongs to. cmdk tracks selection by value, so two groups
   * offering the same command (the previewed issue and the list selection
   * around it) need distinct values or Enter runs the first group's row.
   */
  scope?: string;
}) {
  const palette = usePalette();
  const Icon = command.icon;
  return (
    <CommandPrimitive.Item
      value={scope ? `command:${scope}:${command.id}` : `command:${command.id}`}
      onSelect={() => {
        if (command.page) {
          palette.push({
            page: command.page,
            crumbs: [command.crumb, stripEllipsis(command.label)].filter(
              (crumb): crumb is string => !!crumb,
            ),
          });
          return;
        }
        palette.close();
        command.run?.();
      }}
      className={cn(ROW_CLASS, command.destructive && "text-destructive")}
    >
      <Icon className={cn(ROW_ICON_CLASS, command.destructive && "text-destructive")} />
      <span className="min-w-0 flex-1 truncate">
        <HighlightText text={command.label} query={query} />
      </span>
      {command.trailing}
      {command.hint && <RowHint>{command.hint}</RowHint>}
      {command.shortcut && (
        <ShortcutKeycaps shortcut={command.shortcut} decorative className="shrink-0" />
      )}
    </CommandPrimitive.Item>
  );
}

export function CommandGroupView({ group, query }: { group: PaletteCommandGroup; query: string }) {
  if (group.commands.length === 0) return null;
  return (
    <PaletteGroup heading={group.heading}>
      {group.commands.map((command) => (
        <CommandRow key={command.id} command={command} query={query} scope={group.id} />
      ))}
    </PaletteGroup>
  );
}
