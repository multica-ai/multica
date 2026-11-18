"use client";

import {
  createContext,
  Fragment,
  useCallback,
  useContext,
  useLayoutEffect,
  useState,
  type ReactNode,
} from "react";
import { Plus } from "@multica/ui/icons";
import { IconButton } from "@multica/ui/components/common/icon-button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { useT } from "../../i18n";

/** Menu items an empty section offers from the sidebar's "+" menu. */
export interface IssueSidebarAddEntry {
  /** Position among the other entries; sidebar order. */
  order: number;
  items: ReactNode;
}

type Entries = ReadonlyMap<string, IssueSidebarAddEntry>;
type SetEntry = (key: string, entry: IssueSidebarAddEntry | null) => void;

// Two contexts: sections only set entries, so a change to the entries
// re-renders the menu without re-rendering the sections that offer them.
const SetEntryContext = createContext<SetEntry | null>(null);
const EntriesContext = createContext<Entries>(new Map());

export function IssueSidebarAddMenuProvider({ children }: { children: ReactNode }) {
  const [entries, setEntries] = useState<Entries>(() => new Map());
  const setEntry = useCallback<SetEntry>((key, entry) => {
    setEntries((current) => {
      if ((current.get(key) ?? null) === entry) return current;
      const next = new Map(current);
      if (entry) next.set(key, entry);
      else next.delete(key);
      return next;
    });
  }, []);
  return (
    <SetEntryContext.Provider value={setEntry}>
      <EntriesContext.Provider value={entries}>{children}</EntriesContext.Provider>
    </SetEntryContext.Provider>
  );
}

/**
 * Offers `entry` from the sidebar's "+" menu while it is non-null. A section
 * with nothing to list renders nothing, so its card can go, and hands the way
 * to add its first item to this menu instead. The items render in the menu
 * but keep the section's state and handlers, since the section stays mounted.
 */
export function useIssueSidebarAddEntry(key: string, entry: IssueSidebarAddEntry | null) {
  const setEntry = useContext(SetEntryContext);
  // Every render: the items close over the section's latest state. Only the
  // menu reads the entries, so this cannot feed back into the section. A
  // layout effect commits the "+" with the section that went empty, ready to
  // open, instead of a frame later with its menu still being set up.
  useLayoutEffect(() => {
    setEntry?.(key, entry);
  });
  useLayoutEffect(() => () => setEntry?.(key, null), [setEntry, key]);
}

/** The sidebar's "+": what empty sections offer to add. Absent when none do. */
export function IssueSidebarAddMenu() {
  const { t } = useT("issues");
  const entries = useContext(EntriesContext);
  if (entries.size === 0) return null;
  const sorted = [...entries.entries()].sort(([, a], [, b]) => a.order - b.order);

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <IconButton
            variant="ghost"
            size="icon-xs"
            label={t(($) => $.detail.sidebar_add)}
            className="text-muted-foreground data-popup-open:bg-accent data-popup-open:text-foreground"
          >
            <Plus />
          </IconButton>
        }
      />
      <DropdownMenuContent align="end" className="w-64 max-w-(--available-width)">
        {sorted.map(([key, entry], index) => (
          <Fragment key={key}>
            {index > 0 && <DropdownMenuSeparator />}
            {entry.items}
          </Fragment>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
