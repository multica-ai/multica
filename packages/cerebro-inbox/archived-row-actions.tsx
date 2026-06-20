"use client";

// FIR-1645 — the dynamic inbox's Archived block gives each archived row a
// SECOND restore action ("unarchive and mark unread") on top of the plain
// "unarchive" that every archived row already offers.
//
// The restore button is rendered by `CerebroUnarchiveAction`, which the
// upstream `InboxListItem` mounts from its `onUnarchive` slot. Rather than
// thread a new prop through that upstream-zone component, the Archived block
// wraps each row in this per-row context provider; `CerebroUnarchiveAction`
// reads it and upgrades its single restore button into a small two-action
// menu when the extra action is present. Absent (the classic archived view),
// the button stays exactly as before — so this is purely additive and lives
// entirely in the cerebro zone.

import { createContext, useContext, type ReactNode } from "react";

export interface ArchivedRowActions {
  /** Restore the row AND force it back to unread in one step. */
  onUnarchiveUnread: () => void;
}

const ArchivedRowActionsContext = createContext<ArchivedRowActions | null>(null);

export function ArchivedRowActionsProvider({
  value,
  children,
}: {
  value: ArchivedRowActions;
  children: ReactNode;
}) {
  return (
    <ArchivedRowActionsContext.Provider value={value}>
      {children}
    </ArchivedRowActionsContext.Provider>
  );
}

/** Null outside the Archived block (e.g. the classic archived view). */
export function useArchivedRowActions(): ArchivedRowActions | null {
  return useContext(ArchivedRowActionsContext);
}
