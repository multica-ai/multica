/* eslint-disable i18next/no-literal-string */
import { Copy, ExternalLink, GitBranch, MessageSquareText } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { CONFIDENCE_LABELS, DOMAIN_OPTIONS, GRAPH_NODES, HIDDEN_GRAPH_NODE_TYPES, NODE_TYPE_LABELS } from "../mock-data";
import type { GraphNode } from "../types";

const DOMAIN_LABELS: ReadonlyMap<string, string> = new Map(DOMAIN_OPTIONS.map((domain) => [domain.id, domain.label]));
const NODE_LABELS: ReadonlyMap<string, string> = new Map(GRAPH_NODES.map((node) => [node.id, node.displayName ?? node.label]));
const NODE_BY_ID: ReadonlyMap<string, GraphNode> = new Map(GRAPH_NODES.map((node) => [node.id, node]));
const TECHNICAL_TYPES = new Set<GraphNode["type"]>(["repo", "frontend_app", "service", "api_contract"]);
const INTERNAL_KNOWLEDGE_PREFIXES = ["wiki/", "knowledge/", ".ai/", "raw/", "manifest.yaml", "AGENTS.md"];

function displayValue(value: string | undefined) {
  return value && value.length > 0 ? value : "未标注";
}

function displayDomain(value: string | undefined) {
  return value ? (DOMAIN_LABELS.get(value) ?? value) : "未标注";
}

function isInternalKnowledgePath(value: string | undefined) {
  return value ? INTERNAL_KNOWLEDGE_PREFIXES.some((prefix) => value.startsWith(prefix)) : false;
}

function shouldShowTechnicalLocation(node: GraphNode) {
  return Boolean(node.path) && TECHNICAL_TYPES.has(node.type) && !isInternalKnowledgePath(node.path);
}

function displayRelatedLabel(value: string) {
  const label = NODE_LABELS.get(value) ?? value;
  if (!isInternalKnowledgePath(label)) return label;
  return label.split("/").pop()?.replace(/\.(md|yaml|yml)$/i, "") ?? "知识节点";
}

function isVisibleRelatedNode(value: string) {
  const node = NODE_BY_ID.get(value);
  return !node || !HIDDEN_GRAPH_NODE_TYPES.has(node.type);
}

export function NodeInspector({
  node,
  onAskNode,
  onExpand,
}: {
  node: GraphNode;
  onAskNode: () => void;
  onExpand: () => void;
}) {
  const showTechnicalLocation = shouldShowTechnicalLocation(node);

  return (
    <aside className="ug-panel ug-inspector">
      <div>
        <p className="ug-section-label">节点详情</p>
        <h2 className="ug-inspector-title">{node.displayName ?? node.label}</h2>
        <div className="ug-badge-row">
          <span className="ug-type-badge">{NODE_TYPE_LABELS[node.type]}</span>
          {node.confidence && <span className={`ug-confidence-badge ug-confidence-${node.confidence}`}>{CONFIDENCE_LABELS[node.confidence]}</span>}
        </div>
      </div>

      {showTechnicalLocation && (
        <div className="ug-inspector-block">
          <p className="ug-section-label">技术定位</p>
          <p className="ug-code-path">
            {displayValue(node.path)}
          </p>
        </div>
      )}

      <div className="ug-inspector-block">
        <p className="ug-section-label">摘要</p>
        <p className="ug-summary-text">{displayValue(node.summary)}</p>
      </div>

      {(node.details ?? []).length > 0 && (
        <div className="ug-inspector-block">
          <p className="ug-section-label">知识要点</p>
          <div className="ug-stack">
            {node.details?.map((section) => (
              <div key={section.title} className="ug-evidence-card">
                <p className="ug-evidence-title">{section.title}</p>
                <ul className="ug-evidence-list">
                  {section.items.map((item) => <li key={item}>{item}</li>)}
                </ul>
              </div>
            ))}
          </div>
        </div>
      )}

      <div className="ug-info-grid">
        <Info label="业务域" value={displayDomain(node.domain)} />
        <Info label="仓库" value={node.repo} />
        <Info label="应用" value={node.app} />
        <Info label="更新" value={node.updated} />
      </div>

      <div className="ug-inspector-block">
        <p className="ug-section-label">依据</p>
        <div className="ug-stack">
          {(node.evidence ?? []).length > 0 ? (
            node.evidence?.map((item, index) => (
              <div key={`${item.title}-${index}`} className="ug-evidence-card">
                <p className="ug-evidence-title">{index + 1}. {item.title}</p>
                <p className="ug-evidence-copy">{item.description}</p>
              </div>
            ))
          ) : (
            <p className="ug-muted-text">当前节点暂无展开依据。</p>
          )}
        </div>
      </div>

      <div className="ug-inspector-block">
          <p className="ug-section-label">关联节点</p>
        <div className="ug-related-list">
          {(node.related ?? []).filter(isVisibleRelatedNode).map((item) => <span key={item} className="ug-related-chip">{displayRelatedLabel(item)}</span>)}
          {(node.related ?? []).filter(isVisibleRelatedNode).length === 0 && <span className="ug-muted-text">暂无关联节点</span>}
        </div>
      </div>

      <div className="ug-inspector-actions">
        {showTechnicalLocation && (
          <>
            <Button variant="outline" size="sm" onClick={() => node.path && navigator.clipboard?.writeText(node.path)}>
              <Copy className="size-3.5" />
              复制定位
            </Button>
            <Button variant="outline" size="sm">
              <ExternalLink className="size-3.5" />
              打开源码
            </Button>
          </>
        )}
        <Button variant="outline" size="sm" onClick={onExpand}>
          <GitBranch className="size-3.5" />
          展开邻居
        </Button>
        <Button variant="default" size="sm" onClick={onAskNode}>
          <MessageSquareText className="size-3.5" />
          询问节点
        </Button>
      </div>
    </aside>
  );
}

function Info({ label, value }: { label: string; value: string | undefined }) {
  return (
    <div className="ug-info-cell">
      <p>{label}</p>
      <span>{displayValue(value)}</span>
    </div>
  );
}
