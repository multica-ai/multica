"use client";

import {
  createContext,
  use,
  useCallback,
  useEffect,
  useMemo,
  useState,
  useTransition,
} from "react";
import type { NavigationAdapter } from "./types";

const NavigationContext = createContext<NavigationAdapter | null>(null);
const NavigationPendingContext = createContext<boolean>(false);

/** `+1` while a reported swap is in flight, `-1` when it settles. */
type PendingReporter = (delta: 1 | -1) => void;
const NavigationPendingReportContext = createContext<PendingReporter | null>(
  null,
);

export function NavigationProvider({
  value,
  children,
}: {
  value: NavigationAdapter;
  children: React.ReactNode;
}) {
  // Wrap push/replace in startTransition so any caller of useNavigation()
  // (sidebar AppLink, command palette, modal post-create jumps) gets a
  // React pending signal during route commit. On web this stays true until
  // Next.js commits the new RSC payload; on desktop it flips off quickly
  // because react-router commits synchronously — both are correct.
  const [isPending, startTransition] = useTransition();
  // Second pending source: in-page content swaps reported through
  // `useReportNavigating`. The transition above only spans the adapter's
  // push/replace, which on desktop is a synchronous store write — it is
  // already settled while the destination content is still being rendered,
  // so on its own it can never keep the progress bar on screen there.
  const [reportedPending, setReportedPending] = useState(0);
  const report = useCallback<PendingReporter>((delta) => {
    setReportedPending((n) => Math.max(0, n + delta));
  }, []);
  const wrapped = useMemo<NavigationAdapter>(
    () => ({
      ...value,
      push: (path: string) => startTransition(() => value.push(path)),
      replace: (path: string) => startTransition(() => value.replace(path)),
    }),
    [value],
  );
  return (
    <NavigationContext.Provider value={wrapped}>
      <NavigationPendingReportContext.Provider value={report}>
        <NavigationPendingContext.Provider
          value={isPending || reportedPending > 0}
        >
          {children}
        </NavigationPendingContext.Provider>
      </NavigationPendingReportContext.Provider>
    </NavigationContext.Provider>
  );
}

/**
 * Non-throwing read of the adapter. For components that legitimately render
 * outside a provider — leaf editor UI mounted in isolation, and the tests that
 * exercise it — where the navigation-dependent behaviour has a sane fallback.
 * Anything that must navigate should use `useNavigation()` and keep the throw.
 */
export function useOptionalNavigation(): NavigationAdapter | null {
  return use(NavigationContext);
}

export function useNavigation(): NavigationAdapter {
  const ctx = use(NavigationContext);
  if (!ctx)
    throw new Error("useNavigation must be used within NavigationProvider");
  return ctx;
}

/**
 * True while a transition-wrapped push/replace is committing, or while any
 * mounted view is reporting an in-page content swap.
 */
export function useIsNavigating(): boolean {
  return use(NavigationPendingContext);
}

/**
 * Report an in-page content swap to the shell's `<NavigationProgress />`.
 *
 * For destinations that never change the route — the inbox's detail pane is
 * the case this exists for. There the click updates the list highlight and
 * the URL immediately and the expensive part (mounting the new issue detail)
 * runs behind a `useDeferredValue`, so nothing the navigation adapter can see
 * is still pending while the user waits. Pass that gap in here and the same
 * bar a real route change gets covers it.
 *
 * Safe to call outside a `NavigationProvider` (leaf views mounted in
 * isolation, and their tests) — it simply reports nowhere.
 */
export function useReportNavigating(pending: boolean): void {
  const report = use(NavigationPendingReportContext);
  useEffect(() => {
    if (!pending || !report) return;
    report(1);
    return () => report(-1);
  }, [pending, report]);
}
