import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { RouterProvider } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import { useActiveGroup, useTabStore } from "@/stores/tab-store";
import {
  getAppRouter,
  initTabCoordinator,
  registerActiveHostElement,
  registerCoordinatorQueryClient,
} from "@/platform/tab-coordinator";
import { useTabMementoRestore } from "@/hooks/use-tab-memento-restore";

/**
 * Renders the active tab session through THE single app router
 * (MUL-4741 single-router session architecture).
 *
 * Exactly one tab is mounted at a time. Switching tabs remounts this host
 * (the key includes the tab id), and reload() remounts it without switching
 * (the key includes mountGeneration). Inactive tabs are pure state — their
 * restorable view state lives in the session memento, captured by the
 * Coordinator on deactivation and restored here on mount:
 *
 *   - warm (query cache still populated): content commits at full height and
 *     the scroll restore is a single pre-paint assignment — no flash.
 *   - cold (inactive > gcTime): the correct shell + skeletons commit first,
 *     and scroll/anchor restore completes when the data lands. Cold is a
 *     deliberate skeleton pass, never a flash of another workspace's data.
 */
export function TabContent() {
  const group = useActiveGroup();
  const generation = useTabStore((s) => s.mountGeneration);
  const qc = useQueryClient();

  // Wire the Coordinator before the first host mount so the router already
  // projects the active session's URL when RouterProvider first renders.
  // useState's initializer is the earliest once-per-tree hook slot; the call
  // is idempotent.
  useState(() => {
    initTabCoordinator();
    return true;
  });

  useEffect(() => {
    registerCoordinatorQueryClient(qc);
  }, [qc]);

  // Sync document.title when switching tabs within the active workspace.
  useEffect(() => {
    if (!group) return;
    const tab = group.tabs.find((t) => t.id === group.activeTabId);
    if (tab) document.title = tab.title;
  }, [group?.activeTabId, group?.tabs]);

  if (!group) return null;

  return (
    <ActiveTabHost
      key={`${group.activeTabId}:${generation}`}
      tabId={group.activeTabId}
    />
  );
}

function ActiveTabHost({ tabId }: { tabId: string }) {
  const hostRef = useRef<HTMLDivElement>(null);
  const router = getAppRouter();

  // The Coordinator captures the outgoing memento from this element while
  // the store notification is still synchronous (pre-unmount).
  useLayoutEffect(() => {
    registerActiveHostElement(hostRef.current);
    return () => registerActiveHostElement(null);
  }, []);

  useTabMementoRestore(tabId, hostRef);

  // `display: contents` keeps the wrapper transparent to the surrounding
  // flex layout.
  return (
    <div ref={hostRef} style={{ display: "contents" }}>
      <RouterProvider router={router} />
    </div>
  );
}
