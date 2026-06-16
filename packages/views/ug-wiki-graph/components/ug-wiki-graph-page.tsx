/* eslint-disable i18next/no-literal-string */
"use client";

import { useMemo, useState } from "react";
import { DRAFT_NEW_SESSION, useChatStore } from "@multica/core/chat";
import { CONFIDENCE_LABELS, DOMAIN_OPTIONS, DOMAIN_ROUTE_NODE_IDS, GRAPH_EDGES, GRAPH_NODES, HIDDEN_GRAPH_NODE_TYPES, NODE_TYPE_LABELS } from "../mock-data";
import { FilterSidebar } from "./filter-sidebar";
import { GraphCanvas } from "./graph-canvas";
import { NodeInspector } from "./node-inspector";
import "../styles.css";

const DEFAULT_DOMAIN = "domestic-influencer";
const DEFAULT_NODE_ID = "AuditPlatformView";
const DOMAIN_LABELS: ReadonlyMap<string, string> = new Map(DOMAIN_OPTIONS.map((item) => [item.id, item.label]));
const NODE_BY_ID: ReadonlyMap<string, (typeof GRAPH_NODES)[number]> = new Map(GRAPH_NODES.map((node) => [node.id, node]));
const NODE_LABELS: ReadonlyMap<string, string> = new Map(GRAPH_NODES.map((node) => [node.id, node.displayName ?? node.label]));
const TECHNICAL_TYPES = new Set<(typeof GRAPH_NODES)[number]["type"]>(["repo", "frontend_app", "service", "api_contract"]);
const INTERNAL_KNOWLEDGE_PREFIXES = ["wiki/", "knowledge/", ".ai/", "raw/", "manifest.yaml", "AGENTS.md"];

function isVisibleGraphNodeId(nodeId: string) {
  const node = NODE_BY_ID.get(nodeId);
  return Boolean(node && !HIDDEN_GRAPH_NODE_TYPES.has(node.type));
}

function collectRelatedNodeIds(seedId: string, depth: 1 | 2) {
  const visited = new Set<string>([seedId]);
  let frontier = new Set<string>([seedId]);

  for (let level = 0; level < depth; level += 1) {
    const next = new Set<string>();
    for (const edge of GRAPH_EDGES) {
      if (frontier.has(edge.source) && !visited.has(edge.target) && isVisibleGraphNodeId(edge.target)) next.add(edge.target);
      if (frontier.has(edge.target) && !visited.has(edge.source) && isVisibleGraphNodeId(edge.source)) next.add(edge.source);
    }
    for (const id of next) visited.add(id);
    frontier = next;
  }

  return visited;
}

function isInternalKnowledgePath(value: string | undefined) {
  return value ? INTERNAL_KNOWLEDGE_PREFIXES.some((prefix) => value.startsWith(prefix)) : false;
}

function shouldIncludeTechnicalLocation(node: (typeof GRAPH_NODES)[number]) {
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

function buildNodeQuotedContextBody(node: (typeof GRAPH_NODES)[number], domainLabel: string) {
  const lines = [
    "【当前节点】",
    `知识域：${domainLabel}`,
    `节点：${node.displayName ?? node.label}`,
    `类型：${NODE_TYPE_LABELS[node.type]}`,
  ];

  if (node.confidence) lines.push(`可信度：${CONFIDENCE_LABELS[node.confidence]}`);
  if (node.repo) lines.push(`仓库：${node.repo}`);
  if (node.app) lines.push(`应用：${node.app}`);
  if (node.updated) lines.push(`更新：${node.updated}`);
  if (shouldIncludeTechnicalLocation(node)) lines.push(`技术定位：${node.path}`);
  if (node.summary) lines.push("", "【摘要】", node.summary);

  if ((node.details ?? []).length > 0) {
    lines.push("", "【知识要点】");
    for (const section of node.details ?? []) {
      lines.push(`${section.title}：`);
      for (const item of section.items) lines.push(`- ${item}`);
    }
  }

  if ((node.evidence ?? []).length > 0) {
    lines.push("", "【依据】");
    for (const item of node.evidence ?? []) lines.push(`- ${item.title}：${item.description}`);
  }

  const related = (node.related ?? []).filter(isVisibleRelatedNode).map(displayRelatedLabel);
  if (related.length > 0) lines.push("", "【关联节点】", related.map((item) => `- ${item}`).join("\n"));

  return lines.join("\n");
}

export function UgWikiGraphPage() {
  const [domain, setDomain] = useState(DEFAULT_DOMAIN);
  const [selectedId, setSelectedId] = useState(DEFAULT_NODE_ID);
  const [scale, setScale] = useState(0.66);
  const [highlightedIds, setHighlightedIds] = useState<Set<string>>(new Set());

  const visibleNodes = useMemo(() => {
    const ids = new Set<string>(DOMAIN_ROUTE_NODE_IDS[domain] ?? []);
    for (const id of highlightedIds) ids.add(id);
    return GRAPH_NODES.filter((node) => {
      if (HIDDEN_GRAPH_NODE_TYPES.has(node.type)) return false;
      if (ids.has(node.id)) return true;
      return node.domain === domain;
    });
  }, [domain, highlightedIds]);

  const selectedNode = useMemo(
    () => GRAPH_NODES.find((node) => node.id === selectedId) ?? GRAPH_NODES.find((node) => node.id === DEFAULT_NODE_ID)!,
    [selectedId],
  );

  const handleAskNode = () => {
    const state = useChatStore.getState();
    const draftKey = `${DRAFT_NEW_SESSION}:${state.selectedAgentId ?? ""}`;
    const domainLabel = DOMAIN_LABELS.get(domain) ?? domain;
    state.setActiveSession(null);
    state.setInputDraft(draftKey, "");
    state.setQuotedContext({
      type: "ug_node",
      id: selectedNode.id,
      label: selectedNode.displayName ?? selectedNode.label,
      subtitle: `${domainLabel} · ${NODE_TYPE_LABELS[selectedNode.type]}`,
      body: buildNodeQuotedContextBody(selectedNode, domainLabel),
    });
    state.setOpen(true);
  };

  const handleDomainChange = (nextDomain: string) => {
    setDomain(nextDomain);
    setHighlightedIds(new Set());
    const routeNodeIds = DOMAIN_ROUTE_NODE_IDS[nextDomain] ?? [];
    const nextNode =
      nextDomain === DEFAULT_DOMAIN
        ? GRAPH_NODES.find((node) => node.id === DEFAULT_NODE_ID)
        : GRAPH_NODES.find((node) => node.id === nextDomain) ??
          GRAPH_NODES.find((node) => node.domain === nextDomain && node.type === "domain") ??
          GRAPH_NODES.find((node) => routeNodeIds.includes(node.id)) ??
          GRAPH_NODES.find((node) => node.domain === nextDomain);
    setSelectedId(nextNode?.id ?? DEFAULT_NODE_ID);
  };

  const handleExpand = (depth: 1 | 2) => {
    setHighlightedIds(collectRelatedNodeIds(selectedNode.id, depth));
    setScale(depth === 1 ? 0.78 : 0.68);
  };

  return (
    <div className="ug-page">
      <main className="ug-workbench">
        <FilterSidebar
          selectedDomain={domain}
          onDomainChange={handleDomainChange}
        />

        <div className="ug-graph-column">
          <GraphCanvas
            nodes={visibleNodes}
            selectedId={selectedId}
            highlightedIds={highlightedIds}
            scale={scale}
            onScaleChange={setScale}
            onSelectNode={setSelectedId}
          />
        </div>

        <NodeInspector
          node={selectedNode}
          onAskNode={handleAskNode}
          onExpand={() => handleExpand(1)}
        />
      </main>
    </div>
  );
}
