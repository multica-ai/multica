"use client";

import { Component, Suspense, lazy, useState, type ComponentType, type ReactNode } from "react";
import { useStore } from "zustand";
import type { createTrajectoryEntryStore, TrajectoryNavigation } from "@multica/core/trajectory/web-entry";
import type { IssueTableQuerySpec } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../../../i18n";

export interface TrajectoryPageProps {
  query: IssueTableQuerySpec;
  navigation?: TrajectoryNavigation;
  ready: boolean;
  onNativeFallback: () => void;
  onScopeChange: () => void;
}
export type TrajectoryPageLoader = () => Promise<{ default: ComponentType<TrajectoryPageProps> }>;
const loadTrajectoryPage: TrajectoryPageLoader = () => import("./trajectory-active-page");
const snapshotQuery = (query: IssueTableQuerySpec): IssueTableQuerySpec => JSON.parse(JSON.stringify(query));

class TrajectoryBoundary extends Component<{ children: ReactNode; fallback: ReactNode }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() { return { failed: true }; }
  render() { return this.state.failed ? this.props.fallback : this.props.children; }
}

function ActivePage({ loadPage, ...props }: TrajectoryPageProps & { loadPage: TrajectoryPageLoader }) {
  const { t } = useT("issues");
  const [Page] = useState(() => lazy(loadPage));
  // The loaded page carries its own back button in its header.
  const back = <Button variant="ghost" size="sm" className="self-start" onClick={props.onNativeFallback}>{t($ => $.trajectory.exit)}</Button>;
  return <TrajectoryBoundary fallback={<div className="flex flex-col gap-2 px-4 py-2">{back}<p role="alert">{t($ => $.trajectory_graph.read_failed)}</p></div>}>
    <Suspense fallback={<div className="flex flex-col gap-2 px-4 py-2">{back}<p role="status">{t($ => $.trajectory.loading)}</p></div>}><Page {...props} /></Suspense>
  </TrajectoryBoundary>;
}

export function TrajectoryEntry({ store, query, navigation, children, ready = true, loadPage = loadTrajectoryPage }: {
  store: ReturnType<typeof createTrajectoryEntryStore>;
  query: IssueTableQuerySpec;
  navigation?: TrajectoryNavigation;
  children: ReactNode;
  ready?: boolean;
  loadPage?: TrajectoryPageLoader;
}) {
  const { t } = useT("issues");
  const requested = useStore(store, state => state.active);
  const active = requested && !navigation?.error;
  const [snapshot, setSnapshot] = useState(() => ({ active, query: snapshotQuery(query) }));
  if (snapshot.active !== active) setSnapshot({ active, query: active ? snapshotQuery(query) : snapshot.query });
  const returnNative = () => { store.getState().setActive(false); navigation?.exit?.(); };
  const closeChangedScope = () => {
    store.getState().setActive(false);
    setSnapshot({ active: false, query: { scope: { kind: "workspace" }, filters: {}, sort: { field: "created_at", direction: "asc" } } });
  };
  return <>
    {navigation?.error && <p role="alert">{t($ => $.trajectory.invalid_link)}<Button variant="ghost" size="sm" onClick={() => navigation.dismissError?.()}>{t($ => $.trajectory.dismiss)}</Button></p>}
    <div style={{ display: active && ready ? "none" : "contents" }}>{children}</div>
    {active && <section style={{ display: ready ? undefined : "none" }} className="flex min-h-0 flex-1 flex-col" aria-label={t($ => $.trajectory.title)}>
      <ActivePage query={snapshot.query} ready={ready} navigation={navigation} onNativeFallback={returnNative} onScopeChange={closeChangedScope} loadPage={loadPage} />
    </section>}
  </>;
}
