import type { CSSProperties } from "react";
import { trajectorySymbol, issueCreationContext, type TrajectoryNode, type TrajectorySymbol } from "@multica/core/trajectory/graph";
import { Check, Circle, Clock, FileText, Pause, Play, X } from "@multica/ui/icons";
import { useT } from "../../../../i18n";

const glyphs = { check: Check, circle: Circle, clock: Clock, file: FileText, pause: Pause, play: Play, x: X };
export function GraphSymbol({ node }: { node: TrajectoryNode }) {
  return <GraphSymbolValue symbol={trajectorySymbol(node)} status={node.status}/>;
}
export function GraphSymbolValue({ symbol, status }: { symbol: TrajectorySymbol; status?: string }) {
  const { t } = useT("issues");
  const Icon = glyphs[symbol.glyph];
  const label = t($ => $.trajectory_graph.symbols[symbol.meaning]);
  return <span role="img" aria-label={label} title={[label, status].filter(Boolean).join(" · ")}
    data-symbol={symbol.meaning} data-tone={symbol.tone} className="trajectory-symbol"
    style={{ "--trajectory-tone": `var(--trajectory-${symbol.tone === "neutral" ? "muted-foreground" : symbol.tone})` } as CSSProperties}>
    <Icon className="size-3" aria-hidden="true" fill={symbol.glyph === "play" ? "currentColor" : "none"} />
  </span>;
}
export function GraphProvenance({ node }: { node: TrajectoryNode }) {
  const { t } = useT("issues");
  const context = issueCreationContext(node);
  return <>
    {context && <span className="trajectory-provenance" data-creation-kind={context.kind} data-source-run={context.runId} data-source-record={context.sourceId}>
      {context.kind === "after_blocked" ? <Pause className="size-3" /> : <X className="size-3" />}
      {context.kind === "after_blocked" ? t($ => $.trajectory_graph.after_blocked) : t($ => $.trajectory_graph.after_failure)}
    </span>}
    {node.kind === "run" && node.details.workflow_step === "blocked" && <span className="trajectory-provenance" aria-label={t($ => $.trajectory_graph.workflow_blocked)}><Pause className="size-3"/>{t($ => $.trajectory_graph.workflow_blocked)}</span>}
  </>;
}
