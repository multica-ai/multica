"use client";

import { useCallback, useRef, useEffect } from "react";
import {
  Search,
  LayoutGrid,
  Clock,
  AlertTriangle,
  FolderKanban,
  Tag,
  ListFilter,
  LayoutList,
} from "lucide-react";
import {
  useInboxFilterStore,
  type GroupMode,
} from "@multica/core/inbox/inbox-filter-store";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
} from "@multica/ui/components/ui/dropdown-menu";
import { useT } from "../../i18n";

const GROUP_OPTIONS: { mode: GroupMode; icon: typeof Clock; labelKey: string }[] = [
  { mode: "time", icon: Clock, labelKey: "group.time" },
  { mode: "severity", icon: AlertTriangle, labelKey: "group.severity" },
  { mode: "project", icon: FolderKanban, labelKey: "group.project" },
  { mode: "type", icon: Tag, labelKey: "group.type" },
];

export interface InboxToolbarProps {
  searchInputRef?: React.RefObject<HTMLInputElement | null>;
}

export function InboxToolbar({ searchInputRef }: InboxToolbarProps) {
  const { t } = useT("inbox");
  const groupMode = useInboxFilterStore((s) => s.groupMode);
  const unreadOnly = useInboxFilterStore((s) => s.unreadOnly);
  const density = useInboxFilterStore((s) => s.density);
  const searchQuery = useInboxFilterStore((s) => s.searchQuery);
  const setGroupMode = useInboxFilterStore((s) => s.setGroupMode);
  const toggleUnreadOnly = useInboxFilterStore((s) => s.toggleUnreadOnly);
  const setDensity = useInboxFilterStore((s) => s.setDensity);
  const setSearchQuery = useInboxFilterStore((s) => s.setSearchQuery);

  const currentGroupOption =
    GROUP_OPTIONS.find((o) => o.mode === groupMode) ?? GROUP_OPTIONS[0]!;

  const localInputRef = useRef<HTMLInputElement>(null);
  const inputRef = searchInputRef ?? localInputRef;

  // Focus search when `/` is pressed
  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (e.key === "/" && document.activeElement !== inputRef.current) {
        e.preventDefault();
        inputRef.current?.focus();
      }
    },
    [inputRef],
  );

  useEffect(() => {
    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [handleKeyDown]);

  return (
    <div className="flex items-center gap-2 border-b px-3 py-2" role="toolbar" aria-label={t(($) => $.toolbar.aria_label)}>
      {/* Search */}
      <div className="relative flex-1 min-w-0">
        <Search className="absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
        <input
          ref={inputRef}
          type="text"
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
          placeholder={t(($) => $.toolbar.search_placeholder)}
          className="h-8 w-full rounded-md border bg-background pl-7 pr-2 text-xs outline-none placeholder:text-muted-foreground/50 focus:border-brand"
          aria-label={t(($) => $.toolbar.search_aria_label)}
        />
      </div>

      {/* Group mode dropdown */}
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button
              variant="ghost"
              size="icon-sm"
              className="text-muted-foreground shrink-0"
              aria-label={t(($) => $.toolbar.group_aria_label)}
            />
          }
        >
          <currentGroupOption.icon className="h-4 w-4" />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-auto">
          <DropdownMenuLabel>{t(($) => $.toolbar.group_by)}</DropdownMenuLabel>
          {GROUP_OPTIONS.map((opt) => (
            <DropdownMenuItem
              key={opt.mode}
              onClick={() => setGroupMode(opt.mode)}
              className={groupMode === opt.mode ? "bg-accent" : ""}
            >
              <opt.icon className="mr-2 h-4 w-4" />
              {t(($) => $.toolbar[opt.labelKey as keyof typeof $.toolbar])}
            </DropdownMenuItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>

      {/* Unread-only toggle */}
      <Button
        variant={unreadOnly ? "secondary" : "ghost"}
        size="icon-sm"
        onClick={toggleUnreadOnly}
        className={`shrink-0 text-muted-foreground ${unreadOnly ? "" : ""}`}
        title={t(($) => $.toolbar.unread_only)}
        aria-label={t(($) => $.toolbar.unread_only)}
        aria-pressed={unreadOnly}
      >
        <ListFilter className="h-4 w-4" />
      </Button>

      {/* View density toggle */}
      <Button
        variant="ghost"
        size="icon-sm"
        onClick={() =>
          setDensity(density === "comfortable" ? "compact" : "comfortable")
        }
        className="shrink-0 text-muted-foreground"
        title={
          density === "comfortable"
            ? t(($) => $.toolbar.density_compact)
            : t(($) => $.toolbar.density_comfortable)
        }
        aria-label={
          density === "comfortable"
            ? t(($) => $.toolbar.density_compact)
            : t(($) => $.toolbar.density_comfortable)
        }
        aria-pressed={density === "compact"}
      >
        {density === "comfortable" ? (
          <LayoutList className="h-4 w-4" />
        ) : (
          <LayoutGrid className="h-4 w-4" />
        )}
      </Button>
    </div>
  );
}
