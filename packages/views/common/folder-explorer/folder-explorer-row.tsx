"use client";

import type { ReactNode } from "react";
import { ChevronRight, Folder } from "@multica/ui/icons";
import { ListGridCell, ListGridRow } from "@multica/ui/components/ui/list-grid";
import { cn } from "@multica/ui/lib/utils";

export function FolderExplorerDepth({ depth }: { depth: number }) {
  if (depth <= 0) return null;
  return (
    <span
      aria-hidden="true"
      className="relative h-full shrink-0 self-stretch"
      style={{ width: depth * 20 }}
    >
      {Array.from({ length: depth }, (_, index) => (
        <span
          key={index}
          className="absolute inset-y-0 w-px bg-border"
          style={{ left: index * 20 + 9 }}
        />
      ))}
    </span>
  );
}

interface FolderExplorerRowProps {
  name: string;
  typeLabel?: string;
  countLabel: string;
  openLabel: string;
  onOpen: () => void;
  icon?: ReactNode;
  className?: string;
  leading?: ReactNode;
  trailing?: ReactNode;
  actions?: ReactNode;
  layout?: "grid" | "container";
  preview?: ReactNode;
  depth?: number;
  expanded?: boolean;
}

export function FolderExplorerRow({
  name,
  typeLabel,
  countLabel,
  openLabel,
  onOpen,
  icon,
  className,
  leading,
  trailing,
  actions,
  layout = "grid",
  preview,
  depth = 0,
  expanded = false,
}: FolderExplorerRowProps) {
  if (layout === "container") {
    return (
      <div
        role="row"
        onClick={onOpen}
        className={cn(
          "group/row relative col-span-full flex h-14 cursor-pointer items-center border-b bg-muted/40 transition-colors hover:bg-muted/70",
          "[--folder-row-background:color-mix(in_oklab,var(--muted)_40%,var(--background))] hover:[--folder-row-background:color-mix(in_oklab,var(--muted)_70%,var(--background))]",
          className,
        )}
      >
        <ListGridCell className="sticky left-0 h-full w-full max-w-[100cqw] shrink-0 gap-3 pl-8 pr-4 @2xl:pl-15 @2xl:pr-6 before:pointer-events-none before:absolute before:inset-y-0 before:left-0 before:w-0.5 before:bg-warning before:opacity-0 before:transition-opacity group-hover/row:before:opacity-100">
          <button
            type="button"
            aria-label={openLabel}
            aria-expanded={expanded}
            onClick={(event) => {
              event.stopPropagation();
              onOpen();
            }}
            className="flex h-full min-w-0 flex-1 items-center gap-3 rounded-xs text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
          >
            <FolderExplorerDepth depth={depth} />
            <ChevronRight
              aria-hidden="true"
              className={cn(
                "size-4 shrink-0 text-muted-foreground transition-transform motion-reduce:transition-none",
                expanded && "rotate-90",
              )}
            />
            {icon ?? (
              <span className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-warning/15 text-warning">
                <Folder aria-hidden="true" className="size-5 fill-current" />
              </span>
            )}
            <span className="min-w-0 flex-1">
              <span className="flex min-w-0 items-center gap-1.5">
                <span className="truncate text-body font-semibold">{name}</span>
                {typeLabel && (
                  <span className="shrink-0 rounded-xs bg-muted px-1 py-0.5 text-caption text-muted-foreground">
                    {typeLabel}
                  </span>
                )}
              </span>
              <span className="block text-caption tabular-nums text-muted-foreground @2xl:hidden">
                {countLabel}
              </span>
            </span>
            {preview}
            <span className="hidden min-w-16 shrink-0 text-right text-caption tabular-nums text-muted-foreground @2xl:block">
              {countLabel}
            </span>
          </button>
          <div className="flex size-7 shrink-0 items-center justify-center" onClick={(event) => event.stopPropagation()}>
            {actions}
          </div>
        </ListGridCell>
      </div>
    );
  }

  return (
    <ListGridRow className={cn("cursor-pointer", className)} onClick={onOpen}>
      {leading}
      <ListGridCell className="h-full px-0 @container/folder-name">
        <button
          type="button"
          aria-label={openLabel}
          aria-expanded={expanded}
          onClick={(event) => {
            event.stopPropagation();
            onOpen();
          }}
          className="flex h-full w-full min-w-0 items-center gap-1.5 rounded-xs px-2 text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
        >
          <FolderExplorerDepth depth={depth} />
          <ChevronRight
            aria-hidden="true"
            className={cn(
              "size-4 shrink-0 text-muted-foreground transition-transform motion-reduce:transition-none",
              expanded && "rotate-90",
            )}
          />
          {icon ?? (
            <Folder aria-hidden="true" className="size-4 shrink-0 text-muted-foreground" />
          )}
          <span className="min-w-0 truncate text-body font-medium">{name}</span>
          {typeLabel && (
            <span className={cn(
              "shrink-0 rounded-xs bg-muted px-1 py-0.5 text-caption text-muted-foreground",
              preview && "@max-[22rem]/folder-name:hidden",
            )}>
              {typeLabel}
            </span>
          )}
          {preview && <span className="shrink-0 @max-[22rem]/folder-name:hidden">{preview}</span>}
          <span className={cn(
            "ml-auto shrink-0 pl-2 text-caption tabular-nums text-muted-foreground",
            preview && "@max-[22rem]/folder-name:hidden",
          )}>
            {countLabel}
          </span>
        </button>
      </ListGridCell>
      {trailing}
      <ListGridCell className="justify-end px-0" onClick={(event) => event.stopPropagation()}>
        {actions}
      </ListGridCell>
    </ListGridRow>
  );
}
