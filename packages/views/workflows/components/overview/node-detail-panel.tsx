import { useMemo } from "react";
import type { WorkflowNode, WorkflowEdge } from "@multica/core/types";
import { useWorkspacePaths } from "@multica/core/paths";
import { useNavigation } from "../../../navigation";
import { useT } from "../../../i18n";

interface NodeDetailPanelProps {
  nodeId: string;
  workflowId: string;
  nodes: WorkflowNode[];
  edges: WorkflowEdge[];
  onClose: () => void;
}

export function NodeDetailPanel({
  nodeId,
  workflowId,
  nodes,
  edges,
  onClose,
}: NodeDetailPanelProps) {
  const { t } = useT("workflows");
  const nav = useNavigation();
  const wsPaths = useWorkspacePaths();

  const node = useMemo(
    () => nodes.find((n) => n.id === nodeId) ?? null,
    [nodes, nodeId],
  );

  // Compute upstream/downstream within the same stage
  const upstreamNodes = useMemo(
    () =>
      edges
        .filter((e) => e.target_node_id === nodeId)
        .map((e) => nodes.find((n) => n.id === e.source_node_id))
        .filter(Boolean) as WorkflowNode[],
    [edges, nodes, nodeId],
  );

  const downstreamNodes = useMemo(
    () =>
      edges
        .filter((e) => e.source_node_id === nodeId)
        .map((e) => nodes.find((n) => n.id === e.target_node_id))
        .filter(Boolean) as WorkflowNode[],
    [edges, nodes, nodeId],
  );

  const formatSchema = useMemo(() => {
    if (!node?.format_schema) return null;
    try {
      if (typeof node.format_schema === "string") {
        return JSON.stringify(JSON.parse(node.format_schema), null, 2);
      }
      // Already an object — pretty-print directly
      return JSON.stringify(node.format_schema, null, 2);
    } catch {
      return String(node.format_schema);
    }
  }, [node?.format_schema]);

  if (!node) {
    return null;
  }

  return (
    <div
      className="fixed right-0 top-0 bottom-0 w-[380px] bg-background border-l shadow-lg z-50 overflow-y-auto"
      data-testid="node-detail-panel"
    >
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-3 border-b sticky top-0 bg-background">
        <h2 className="font-semibold text-sm">
          {t(($) => $.overview.detail_panel.title)}
        </h2>
        <button
          onClick={onClose}
          className="text-muted-foreground hover:text-foreground"
          data-testid="node-detail-close"
        >
          ×
        </button>
      </div>

      <div className="p-4 space-y-5">
        {/* Basic Info */}
        <section>
          <h3 className="font-medium text-sm">{node.title}</h3>
          {node.description && (
            <p className="text-xs text-muted-foreground mt-1">
              {node.description}
            </p>
          )}
        </section>

        {/* Worker */}
        <section>
          <h4 className="text-xs font-medium text-muted-foreground uppercase tracking-wider mb-1">
            {t(($) => $.overview.detail_panel.worker)}
          </h4>
          {node.worker_type ? (
            <div className="text-sm">
              <span className="inline-block px-2 py-0.5 rounded bg-muted text-xs">
                {node.worker_type}
              </span>
              {node.worker_id && (
                <span className="ml-2 text-xs text-muted-foreground">
                  {node.worker_id}
                </span>
              )}
            </div>
          ) : (
            <p className="text-xs text-muted-foreground">
              {t(($) => $.overview.detail_panel.not_configured)}
            </p>
          )}
        </section>

        {/* Critic */}
        <section>
          <h4 className="text-xs font-medium text-muted-foreground uppercase tracking-wider mb-1">
            {t(($) => $.overview.detail_panel.critic)}
          </h4>
          {node.critic_type ? (
            <div className="text-sm">
              <span className="inline-block px-2 py-0.5 rounded bg-muted text-xs">
                {node.critic_type}
              </span>
              {node.critic_id && (
                <span className="ml-2 text-xs text-muted-foreground">
                  {node.critic_id}
                </span>
              )}
            </div>
          ) : (
            <p className="text-xs text-muted-foreground">
              {t(($) => $.overview.detail_panel.not_configured)}
            </p>
          )}
        </section>

        {/* Format Schema */}
        <section>
          <h4 className="text-xs font-medium text-muted-foreground uppercase tracking-wider mb-1">
            {t(($) => $.overview.detail_panel.format_schema)}
          </h4>
          {formatSchema ? (
            <pre className="text-xs bg-muted p-2 rounded overflow-auto max-h-32">
              {formatSchema}
            </pre>
          ) : (
            <p className="text-xs text-muted-foreground">
              {t(($) => $.overview.detail_panel.no_schema)}
            </p>
          )}
        </section>

        {/* Relations */}
        <section>
          <h4 className="text-xs font-medium text-muted-foreground uppercase tracking-wider mb-1">
            {t(($) => $.overview.detail_panel.relations)}
          </h4>
          {upstreamNodes.length > 0 && (
            <div className="mb-2">
              <span className="text-xs text-muted-foreground">
                {t(($) => $.overview.detail_panel.upstream)}:{" "}
              </span>
              {upstreamNodes.map((n) => (
                <span
                  key={n.id}
                  className="text-xs mr-1 px-1.5 py-0.5 rounded bg-muted"
                >
                  {n.title}
                </span>
              ))}
            </div>
          )}
          {downstreamNodes.length > 0 && (
            <div>
              <span className="text-xs text-muted-foreground">
                {t(($) => $.overview.detail_panel.downstream)}:{" "}
              </span>
              {downstreamNodes.map((n) => (
                <span
                  key={n.id}
                  className="text-xs mr-1 px-1.5 py-0.5 rounded bg-muted"
                >
                  {n.title}
                </span>
              ))}
            </div>
          )}
          {upstreamNodes.length === 0 && downstreamNodes.length === 0 && (
            <p className="text-xs text-muted-foreground">
              {t(($) => $.overview.detail_panel.not_configured)}
            </p>
          )}
        </section>
      </div>

      {/* Footer */}
      <div className="sticky bottom-0 bg-background border-t p-3">
        <button
          onClick={() => nav.push(wsPaths.workflowDetail(workflowId))}
          className="w-full py-2 text-sm bg-primary text-primary-foreground rounded-md hover:opacity-90"
        >
          {t(($) => $.overview.detail_panel.open_in_editor)}
        </button>
      </div>
    </div>
  );
}
