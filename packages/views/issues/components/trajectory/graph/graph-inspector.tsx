import { runExecutionSymbol, trajectorySymbol, type TrajectoryGraph, type TrajectoryNode } from "@multica/core/trajectory/graph";
import { issueRouteRef, useWorkspacePaths } from "@multica/core/paths";
import { runConversationTarget } from "@multica/core/trajectory/record-display";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { ArrowUpRight, Pause, Terminal } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { AppLink } from "../../../../navigation";
import { useLocale, useT } from "../../../../i18n";
import { GraphProvenance, GraphSymbol } from "./graph-symbol";
import { GraphText, recordKindKey } from "./graph-record-text";

const runTone = {
  success: "bg-success/15 text-foreground", info: "bg-info/15 text-foreground", danger: "bg-destructive/15 text-foreground",
  warning: "bg-warning/15 text-foreground", neutral: "bg-muted text-muted-foreground", merged: "bg-muted text-muted-foreground",
};
const string = (value: unknown) => typeof value === "string" && value.trim() ? value : "";
export function GraphInspector({ graph, node, select, openLog, canOpenLog = true }: { graph: TrajectoryGraph; node: TrajectoryNode; select: (id: string) => void; openLog: () => void; canOpenLog?: boolean }) {
  const { t } = useT("issues"), paths = useWorkspacePaths();
  const locale = useLocale();
  const isRun = node.kind === "run";
  const kindLabel = (kind: string) => t($ => $.trajectory_detail[recordKindKey(kind)]);
  const owner = graph.nodes.find(item => item.kind === "issue" && item.issue_id === node.issue_id);
  const ownerLabel = owner && [t($ => $.trajectory_graph.owner_label), string(owner.details.issue_key), owner.title].filter(Boolean).join(" · ");
  const triggeringComment = node.kind === "run" ? string(node.details.trigger_comment_id) : "";
  const records = graph.nodes.filter(item => item.id !== node.id && item.issue_id === node.issue_id && (node.kind === "run" ? Boolean(node.run_id) && (item.run_id === node.run_id || Boolean(triggeringComment) && item.kind === "comment" && string(item.details.comment_id) === triggeringComment) : node.kind === "issue" ? ["comment", "artifact"].includes(item.kind) : false));
  const referencedComment = string(node.details.comment_id) && graph.nodes.find(item => item.kind === "comment" && item.issue_id === node.issue_id && string(item.details.comment_id) === string(node.details.comment_id));
  const links = graph.edges.filter(edge => edge.from === node.id || edge.to === node.id);
  const relations = links.flatMap(edge => {
    const target = graph.nodes.find(item => item.id === (edge.from === node.id ? edge.to : edge.from));
    return target ? [{ edge, target }] : [];
  });
  const findings = graph.findings.filter(finding => finding.node_id === node.id);
  const conversation = runConversationTarget(graph, node);
  const conversationHref = conversation && `${paths.issueDetail(issueRouteRef({ id: conversation.issueId, identifier: conversation.identifier }))}#comment-${conversation.commentId}`;
  const conversationLabel = conversation?.relation === "reply" ? t($ => $.trajectory_graph.open_reply) : t($ => $.trajectory_graph.open_trigger_comment);
  const result = node.details.result;
  const hasResult = Object.hasOwn(node.details, "result") && result != null;
  const nativeOutput = typeof result === "string" ? result : result && typeof result === "object" && !Array.isArray(result) ? Reflect.get(result, "output") : undefined;
  const output = hasResult ? string(nativeOutput) : string(node.details.output);
  const structured = hasResult && typeof result !== "string" && !output;
  const relationLabel = (kind: string) => kind === "parent" ? t($ => $.trajectory_detail.relation_parent) : kind === "contains" ? t($ => $.trajectory_detail.relation_contains) : kind === "retry" ? t($ => $.trajectory.retry) : kind === "rerun" ? t($ => $.trajectory_graph.rerun) : kind.trim() ? kind : t($ => $.trajectory.unknown);
  const error = string(node.details.error) || string(node.details.failure_reason);
  const purpose = string(node.details.trigger_summary) || string(node.details.trigger_content);
  const instructions = string(node.details.instructions), trigger = string(node.details.trigger_content);
  const state = node.kind === "run" ? runExecutionSymbol(node.status) : trajectorySymbol(node);
  const hasSnapshot = node.details.issue_snapshot != null;
  const content = string(node.kind === "issue" ? node.details.description : node.details.content) || (node.kind === "artifact" ? string(node.title) : node.kind === "platform_event" && node.details.record != null ? JSON.stringify(node.details.record, null, 2) : "");
  const literalRecord = node.kind === "platform_event" && node.details.record != null && !string(node.details.content);
  const times = [
    { label: t($ => $.trajectory_detail.created_at), value: node.created_at },
    { label: t($ => $.trajectory_detail.started_at), value: string(node.details.started_at) },
    { label: t($ => $.trajectory_detail.completed_at), value: string(node.details.completed_at) },
    { label: t($ => $.table.columns.updated_at), value: string(node.details.updated_at) },
  ].filter(time => time.value && Number.isFinite(Date.parse(time.value)));
  const stateLabel = `${kindLabel(node.kind)} · ${t($ => $.trajectory_graph.symbols[state.meaning])}`;
  return <div className={cn("trajectory-inspector", isRun && "trajectory-run-inspector")}>
    <div className="trajectory-inspector-header">
      {owner ? <Button size={isRun ? "sm" : "xs"} variant="ghost" className="trajectory-owner-button" aria-label={ownerLabel} onClick={() => select(owner.id)}>{ownerLabel}</Button>
        : <p data-testid="trajectory-owner-unavailable" className="text-muted-foreground">{t($ => $.trajectory_detail.owner_unavailable)}</p>}
      {isRun ? <p className="trajectory-run-status">
        <Badge variant="secondary" className={runTone[state.tone]}>{stateLabel}</Badge>
        {node.details.workflow_step === "blocked" && <Badge variant="destructive" className="text-foreground"><Pause aria-hidden="true"/>{t($ => $.trajectory_graph.workflow_blocked)}</Badge>}
      </p> : <p><GraphSymbol node={node}/><span>{stateLabel}</span><GraphProvenance node={node}/></p>}
      <dl className="trajectory-record-meta">
        <div><dt>{t($ => $.trajectory_detail.stable_id)}</dt><dd className="break-all">{node.id}</dd></div>
        {isRun ? times.map(({ label, value }) => <div key={label}>
          <dt>{label}</dt><dd><time dateTime={value} title={value}>{new Date(value).toLocaleString(locale, {
            year: "numeric", month: "short", day: "numeric", hour: "2-digit", minute: "2-digit", second: "2-digit",
          })}</time></dd>
        </div>) : times.length > 0 && <div><dt>{t($ => $.trajectory_detail.time)}</dt><dd>{times.map(time => <span key={time.label} className="trajectory-record-time">{time.label} · <time dateTime={time.value}>{time.value}</time></span>)}</dd></div>}
      </dl>
      {owner && <Button size={isRun ? "sm" : "xs"} variant={isRun ? "outline" : "ghost"} aria-label={t($ => $.trajectory_graph.open_task)} onClick={() => select(owner.id)}>{t($ => $.trajectory_graph.open_task)}</Button>}
    </div>
    {node.kind === "run" && <>
      {output && <section className="trajectory-inspector-section"><h3>{t($ => $.trajectory_detail.result)}</h3><GraphText content={output} testId="trajectory-output-preview" literal/></section>}
      {error && <section className="trajectory-inspector-section"><h3>{t($ => $.trajectory_detail.error)}</h3><div data-testid="trajectory-run-error"><GraphText content={error}/></div></section>}
      {structured && <details className="trajectory-disclosure"><summary>{t($ => $.trajectory_detail.structured_result)}</summary><pre data-testid="trajectory-structured-result" className="trajectory-raw-text">{JSON.stringify(result, null, 2)}</pre></details>}
      {!output && !error && !structured && <p data-testid="trajectory-no-result" className="text-muted-foreground">{hasResult ? t($ => $.trajectory_detail.no_content) : t($ => $.trajectory_detail.no_result)}</p>}
      {node.details.output_truncated === true && <p className="text-muted-foreground">{t($ => $.trajectory_graph.output_preview)}</p>}
      {purpose && <section className="trajectory-inspector-section"><h3>{t($ => $.trajectory_detail.purpose)}</h3><div data-testid="trajectory-run-purpose"><GraphText content={purpose}/></div></section>}
      <details className="trajectory-disclosure"><summary>{t($ => $.trajectory_detail.input)}</summary>
        {!hasSnapshot && <p data-testid="trajectory-missing-snapshot" className="text-muted-foreground">{t($ => $.trajectory_detail.missing_snapshot)}</p>}
        {hasSnapshot && <section className="trajectory-inspector-section"><h3>{t($ => $.trajectory_detail.snapshot)}</h3><GraphText content={JSON.stringify(node.details.issue_snapshot, null, 2)} literal/></section>}
        {instructions && <section className="trajectory-inspector-section"><h3>{t($ => $.trajectory_detail.instructions)}</h3><GraphText content={instructions} literal/></section>}
        {trigger && <section className="trajectory-inspector-section"><h3>{t($ => $.trajectory_detail.trigger)}</h3><GraphText content={trigger}/></section>}
      </details>
    </>}
    {node.kind !== "run" && <>
      {content ? <div data-testid="trajectory-record-content"><GraphText content={content} literal={literalRecord}/></div> : <p className="text-muted-foreground">{t($ => $.trajectory_detail.no_content)}</p>}
      {node.details.content_truncated === true && <p data-testid="trajectory-content-truncated" className="text-muted-foreground">{t($ => $.trajectory_detail.source_truncated)}</p>}
    </>}
    {!["issue", "run"].includes(node.kind) && <>
      {referencedComment && referencedComment.id !== node.id && <Button size="xs" variant="ghost" onClick={() => select(referencedComment.id)}>{t($ => $.trajectory_graph.open_comment)}</Button>}
      {string(node.details.attachment_id) && <AppLink href={paths.attachmentPreview(string(node.details.attachment_id))} target="_blank" className="block text-primary underline">{t($ => $.trajectory.preview)}</AppLink>}
    </>}
    {findings.filter(finding => !(node.kind === "run" && finding.code === "missing_input_snapshot")).map(finding => <GraphText key={finding.id} content={finding.message}/>)}
    {records.length > 0 && <section className="trajectory-inspector-section"><h3>{t($ => $.trajectory_graph.evidence_label)}</h3>{records.map(record => <button key={record.id} type="button" className="trajectory-record-button" aria-label={record.title} onClick={() => select(record.id)}><span>{record.title}</span> · {kindLabel(record.kind)}</button>)}</section>}
    {relations.length > 0 && <section className="trajectory-inspector-section"><h3>{t($ => $.trajectory_graph.relations_label)}</h3>{relations.map(({ edge, target }) => <button key={edge.id} type="button" className="trajectory-record-button trajectory-relation-button" aria-label={`${relationLabel(edge.kind)} · ${kindLabel(target.kind)} · ${target.title}`} onClick={() => select(target.id)}><span>{edge.from === node.id ? "→" : "←"} {relationLabel(edge.kind)}</span> · {kindLabel(target.kind)} · {target.title}</button>)}</section>}
    {(conversationHref || node.run_id && canOpenLog) && <div className={isRun ? "trajectory-run-actions" : "contents"}>
      {conversationHref && (isRun
        ? <Button size="sm" variant="outline" nativeButton={false} role="link" render={<AppLink href={conversationHref}/>}>
          <ArrowUpRight aria-hidden="true"/>{conversationLabel}
        </Button>
        : <AppLink href={conversationHref} className="block text-primary underline">{conversationLabel}</AppLink>)}
      {node.run_id && canOpenLog && <Button size="sm" variant="outline" aria-label={t($ => $.trajectory_graph.open_log)} onClick={openLog}>
        {isRun && <Terminal aria-hidden="true"/>}{t($ => $.trajectory_graph.open_log_label)}</Button>}
    </div>}
    <details className="trajectory-disclosure"><summary>{t($ => $.trajectory_detail.raw_data)}</summary>
      <pre data-testid="trajectory-raw-node" className="trajectory-raw-text">{JSON.stringify(node, null, 2)}</pre>
      {links.length > 0 && <pre className="trajectory-raw-text">{JSON.stringify(links, null, 2)}</pre>}
      {findings.length > 0 && <pre className="trajectory-raw-text">{JSON.stringify(findings, null, 2)}</pre>}
    </details>
  </div>;
}
