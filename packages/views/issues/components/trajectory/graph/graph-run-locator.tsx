import { useState } from "react";
import type { TrajectoryGraph } from "@multica/core/trajectory/graph";
import { loadedRunPositions } from "@multica/core/trajectory/record-display";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../../../i18n";

export function GraphRunLocator({ graph, issueId, locate }: { graph: TrajectoryGraph; issueId: string; locate: (nodeId: string) => void }) {
  const { t } = useT("issues");
  const [position, setPosition] = useState(-1);
  const { runningIds, latestId } = loadedRunPositions(graph, issueId);
  const owner = graph.nodes.find(node => node.kind === "issue" && node.issue_id === issueId);
  const label = typeof owner?.details.issue_key === "string" ? owner.details.issue_key : issueId;
  const hasRuns = graph.nodes.some(node => node.kind === "run" && node.issue_id === issueId);
  return <div className="trajectory-run-locator" role="group" aria-label={t($ => $.trajectory_detail.run) + " · " + label}>
    <span className="trajectory-locator-owner">{label}</span>
    <Button size="xs" variant="ghost" disabled={!runningIds.length} aria-label={t($ => $.trajectory_detail.locate_running)} onClick={() => {
      const next = (position + 1) % runningIds.length;
      setPosition(next); locate(runningIds[next]!);
    }}>{t($ => $.trajectory_detail.locate_running)}</Button>
    <Button size="xs" variant="ghost" disabled={!latestId} onClick={() => { if (latestId) locate(latestId); }}>{t($ => $.trajectory_detail.locate_latest)}</Button>
    {!hasRuns ? <span>{t($ => $.trajectory_detail.no_records)}</span> : !runningIds.length ? <span>{t($ => $.trajectory_detail.no_running)}</span>
      : runningIds.length > 1 ? <span>{t($ => $.trajectory_detail.running_position, { index: position < 0 ? 1 : position % runningIds.length + 1, count: runningIds.length })}</span> : null}
  </div>;
}
