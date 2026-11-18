"use client";

import {
  createContext,
  Fragment,
  useContext,
  useEffect,
  useState,
  useSyncExternalStore,
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

interface AddMenuStore {
  subscribe: (listener: () => void) => () => void;
  get: () => Entries;
  set: (key: string, entry: IssueSidebarAddEntry | null) => void;
}

function createAddMenuStore(): AddMenuStore {
  let entries: Entries = new Map();
  const listeners = new Set<() => void>();
  return {
    subscribe: (listener) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
    get: () => entries,
    set: (key, entry) => {
      if ((entries.get(key) ?? null) === entry) return;
      const next = new Map(entries);
      if (entry) next.set(key, entry);
      else next.delete(key);
      entries = next;
      for (const listener of listeners) listener();
    },
  };
}

const AddMenuContext = createContext<AddMenuStore | null>(null);
const NO_ENTRIES: Entries = new Map();
const noSubscription = () => () => {};
const noEntries = () => NO_ENTRIES;

export function IssueSidebarAddMenuProvider({ children }: { children: ReactNode }) {
  const [store] = useState(createAddMenuStore);
  return <AddMenuContext.Provider value={store}>{children}</AddMenuContext.Provider>;
}

/**
 * Offers `entry` from the sidebar's "+" menu while it is non-null. A section
 * with nothing to list renders nothing, so its card can go, and hands the way
 * to add its first item to this menu instead. The items render in the menu
 * but keep the section's state and handlers, since the section stays mounted.
 */
export function useIssueSidebarAddEntry(key: string, entry: IssueSidebarAddEntry | null) {
  const store = useContext(AddMenuContext);
  // Every render: the items close over the section's latest state. Only the
  // menu re-renders, so this cannot feed back into the section.
  useEffect(() => {
    store?.set(key, entry);
  });
  useEffect(() => () => store?.set(key, null), [store, key]);
}

/** The sidebar's "+": what empty sections offer to add. Absent when none do. */
export function IssueSidebarAddMenu() {
  const { t } = useT("issues");
  const store = useContext(AddMenuContext);
  const entries = useSyncExternalStore(store?.subscribe ?? noSubscription, store?.get ?? noEntries, store?.get ?? noEntries);
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
