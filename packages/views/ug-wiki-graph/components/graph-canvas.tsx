/* eslint-disable i18next/no-literal-string */
import { useEffect, useMemo, useRef, useState } from "react";
import * as d3 from "d3";
import { Minus, Plus } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { CONFIDENCE_LABELS, GRAPH_EDGES, NODE_TYPE_LABELS } from "../mock-data";
import type { GraphEdge, GraphNode, GraphNodeType } from "../types";

const MIN_ZOOM = 0.45;
const MAX_ZOOM = 2.25;
const MIN_RADIUS = 9;
const MAX_RADIUS = 21;
const NODE_PADDING = 72;

type D3GraphNode = GraphNode &
  d3.SimulationNodeDatum & {
    degree: number;
    radius: number;
    seedX: number;
    seedY: number;
  };

type D3GraphEdge = Omit<GraphEdge, "source" | "target"> &
  d3.SimulationLinkDatum<D3GraphNode> & {
    source: string | D3GraphNode;
    target: string | D3GraphNode;
  };

type TooltipState = {
  visible: boolean;
  x: number;
  y: number;
  title: string;
  meta: string;
};

type GraphData = {
  nodes: D3GraphNode[];
  edges: D3GraphEdge[];
};

const TYPE_CLASSES: Record<GraphNodeType, string> = {
  domain: "ug-d3-node-domain",
  repo: "ug-d3-node-code",
  frontend_app: "ug-d3-node-code",
  code_file: "ug-d3-node-code",
  service: "ug-d3-node-service",
  markdown: "ug-d3-node-doc",
  prd: "ug-d3-node-doc",
  business_insight: "ug-d3-node-insight",
  alert_sop: "ug-d3-node-alert",
  api_contract: "ug-d3-node-service",
  term: "ug-d3-node-term",
  flow: "ug-d3-node-flow",
  owner: "ug-d3-node-owner",
  tracking: "ug-d3-node-tracking",
  data_asset: "ug-d3-node-asset",
  alert_policy: "ug-d3-node-alert",
  config: "ug-d3-node-config",
};

function edgeEndpointId(endpoint: string | D3GraphNode | undefined) {
  if (!endpoint) return "";
  return typeof endpoint === "string" ? endpoint : endpoint.id;
}

function edgeTouches(edge: D3GraphEdge, nodeId: string) {
  return edgeEndpointId(edge.source) === nodeId || edgeEndpointId(edge.target) === nodeId;
}

function edgeConnectsHighlighted(edge: D3GraphEdge, highlightedIds: Set<string>) {
  return highlightedIds.has(edgeEndpointId(edge.source)) && highlightedIds.has(edgeEndpointId(edge.target));
}

function radiusForNode(node: GraphNode, degree: number, maxDegree: number) {
  const sizeBoost = node.size === "lg" ? 4 : node.size === "md" ? 2 : 0;
  if (maxDegree <= 0) return MIN_RADIUS + sizeBoost;
  return MIN_RADIUS + sizeBoost + (degree / maxDegree) * (MAX_RADIUS - MIN_RADIUS);
}

function tooltipMeta(node: D3GraphNode) {
  const type = NODE_TYPE_LABELS[node.type];
  const confidence = node.confidence ? CONFIDENCE_LABELS[node.confidence] : "未标注";
  return `${type} · ${confidence} · ${node.degree} 条连接`;
}

function buildGraphData(nodes: GraphNode[]): GraphData {
  const visibleIds = new Set(nodes.map((node) => node.id));
  const edges = GRAPH_EDGES.filter((edge) => visibleIds.has(edge.source) && visibleIds.has(edge.target));
  const degreeMap = new Map<string, number>();

  for (const edge of edges) {
    degreeMap.set(edge.source, (degreeMap.get(edge.source) ?? 0) + 1);
    degreeMap.set(edge.target, (degreeMap.get(edge.target) ?? 0) + 1);
  }

  const maxDegree = Math.max(0, ...nodes.map((node) => degreeMap.get(node.id) ?? 0));
  const graphNodes = nodes.map<D3GraphNode>((node) => {
    const degree = degreeMap.get(node.id) ?? 0;
    return {
      ...node,
      degree,
      radius: radiusForNode(node, degree, maxDegree),
      seedX: Number.isFinite(node.x) ? node.x : 0,
      seedY: Number.isFinite(node.y) ? node.y : 0,
      x: Number.isFinite(node.x) ? node.x : 0,
      y: Number.isFinite(node.y) ? node.y : 0,
    };
  });

  return {
    nodes: graphNodes,
    edges: edges.map((edge) => ({ ...edge })),
  };
}

function seedInitialPositions(nodes: D3GraphNode[], width: number, height: number) {
  if (nodes.length === 0) return;
  const xs = nodes.map((node) => node.seedX);
  const ys = nodes.map((node) => node.seedY);
  const minX = Math.min(...xs);
  const maxX = Math.max(...xs);
  const minY = Math.min(...ys);
  const maxY = Math.max(...ys);
  const spanX = Math.max(1, maxX - minX);
  const spanY = Math.max(1, maxY - minY);

  for (const [index, node] of nodes.entries()) {
    const fallbackAngle = (index / Math.max(1, nodes.length)) * Math.PI * 2;
    const fallbackX = width / 2 + Math.cos(fallbackAngle) * width * 0.22;
    const fallbackY = height / 2 + Math.sin(fallbackAngle) * height * 0.22;
    node.x = Number.isFinite(node.seedX) ? NODE_PADDING + ((node.seedX - minX) / spanX) * Math.max(1, width - NODE_PADDING * 2) : fallbackX;
    node.y = Number.isFinite(node.seedY) ? NODE_PADDING + ((node.seedY - minY) / spanY) * Math.max(1, height - NODE_PADDING * 2) : fallbackY;
  }
}

function centeredZoomTransform(width: number, height: number, zoom: number) {
  return d3.zoomIdentity.translate((width * (1 - zoom)) / 2, (height * (1 - zoom)) / 2).scale(zoom);
}

function useCanvasSize(ref: React.RefObject<HTMLDivElement | null>) {
  const [size, setSize] = useState({ width: 900, height: 560 });

  useEffect(() => {
    const el = ref.current;
    if (!el) return;

    const update = () => {
      const rect = el.getBoundingClientRect();
      setSize({
        width: Math.max(320, Math.round(rect.width)),
        height: Math.max(360, Math.round(rect.height)),
      });
    };

    update();
    const observer = new ResizeObserver(update);
    observer.observe(el);
    return () => observer.disconnect();
  }, [ref]);

  return size;
}

export function GraphCanvas({
  nodes,
  selectedId,
  highlightedIds,
  scale,
  onScaleChange,
  onSelectNode,
}: {
  nodes: GraphNode[];
  selectedId: string;
  highlightedIds: Set<string>;
  scale: number;
  onScaleChange: (scale: number) => void;
  onSelectNode: (nodeId: string) => void;
}) {
  const canvasRef = useRef<HTMLDivElement>(null);
  const svgRef = useRef<SVGSVGElement>(null);
  const zoomRef = useRef<d3.ZoomBehavior<SVGSVGElement, unknown> | null>(null);
  const scaleRef = useRef(scale);
  const nodeSelectionRef = useRef<d3.Selection<SVGGElement, D3GraphNode, SVGGElement, unknown> | null>(null);
  const edgeSelectionRef = useRef<d3.Selection<SVGLineElement, D3GraphEdge, SVGGElement, unknown> | null>(null);
  const simulationRef = useRef<d3.Simulation<D3GraphNode, D3GraphEdge> | null>(null);
  const [hoveredId, setHoveredId] = useState<string | null>(null);
  const [tooltip, setTooltip] = useState<TooltipState>({
    visible: false,
    x: 0,
    y: 0,
    title: "",
    meta: "",
  });
  const canvasSize = useCanvasSize(canvasRef);
  const graphData = useMemo(() => buildGraphData(nodes), [nodes]);

  useEffect(() => {
    scaleRef.current = scale;
  }, [scale]);

  useEffect(() => {
    const svgEl = svgRef.current;
    if (!svgEl) return;

    simulationRef.current?.stop();
    seedInitialPositions(graphData.nodes, canvasSize.width, canvasSize.height);

    const svg = d3.select(svgEl);
    svg.selectAll("*").remove();
    svg.attr("viewBox", `0 0 ${canvasSize.width} ${canvasSize.height}`);

    const root = svg.append("g").attr("class", "ug-d3-root");
    const edgeLayer = root.append("g").attr("class", "ug-d3-edge-layer");
    const nodeLayer = root.append("g").attr("class", "ug-d3-node-layer");

    const edgeSelection = edgeLayer
      .selectAll<SVGLineElement, D3GraphEdge>("line")
      .data(graphData.edges, (edge) => edge.id)
      .join("line")
      .attr("class", "ug-d3-edge");

    const nodeSelection = nodeLayer
      .selectAll<SVGGElement, D3GraphNode>("g")
      .data(graphData.nodes, (node) => node.id)
      .join("g")
      .attr("class", (node) => `ug-d3-node ${TYPE_CLASSES[node.type]}`)
      .style("cursor", "pointer");

    nodeSelection
      .append("circle")
      .attr("class", "ug-d3-node-circle")
      .attr("r", (node) => node.radius);

    nodeSelection
      .append("text")
      .attr("class", "ug-d3-label")
      .attr("text-anchor", "middle")
      .attr("y", (node) => node.radius + 12)
      .text((node) => node.displayName ?? node.label);

    const simulation = d3
      .forceSimulation<D3GraphNode>(graphData.nodes)
      .force(
        "link",
        d3
          .forceLink<D3GraphNode, D3GraphEdge>(graphData.edges)
          .id((node) => node.id)
          .distance((edge) => {
            const source = edge.source as D3GraphNode;
            const target = edge.target as D3GraphNode;
            return 88 + source.radius + target.radius;
          })
          .strength(0.72),
      )
      .force("charge", d3.forceManyBody<D3GraphNode>().strength((node) => -260 - node.radius * 8))
      .force("collide", d3.forceCollide<D3GraphNode>().radius((node) => node.radius + 32).iterations(2))
      .force("center", d3.forceCenter(canvasSize.width / 2, canvasSize.height / 2))
      .force("x", d3.forceX<D3GraphNode>(canvasSize.width / 2).strength(0.035))
      .force("y", d3.forceY<D3GraphNode>(canvasSize.height / 2).strength(0.045));

    const drag = d3
      .drag<SVGGElement, D3GraphNode>()
      .on("start", (event, node) => {
        if (!event.active) simulation.alphaTarget(0.26).restart();
        node.fx = node.x;
        node.fy = node.y;
      })
      .on("drag", (event, node) => {
        node.fx = event.x;
        node.fy = event.y;
      })
      .on("end", (event, node) => {
        if (!event.active) simulation.alphaTarget(0);
        node.fx = null;
        node.fy = null;
      });

    nodeSelection
      .call(drag)
      .on("mouseenter", (event, node) => {
        const rect = canvasRef.current?.getBoundingClientRect();
        setHoveredId(node.id);
        setTooltip({
          visible: true,
          x: rect ? event.clientX - rect.left + 14 : 0,
          y: rect ? event.clientY - rect.top - 56 : 0,
          title: node.displayName ?? node.label,
          meta: tooltipMeta(node),
        });
      })
      .on("mousemove", (event, node) => {
        const rect = canvasRef.current?.getBoundingClientRect();
        setTooltip((current) => ({
          ...current,
          visible: true,
          x: rect ? event.clientX - rect.left + 14 : current.x,
          y: rect ? event.clientY - rect.top - 56 : current.y,
          title: node.displayName ?? node.label,
          meta: tooltipMeta(node),
        }));
      })
      .on("mouseleave", () => {
        setHoveredId(null);
        setTooltip((current) => ({ ...current, visible: false }));
      })
      .on("click", (_event, node) => {
        onSelectNode(node.id);
      });

    const zoom = d3
      .zoom<SVGSVGElement, unknown>()
      .scaleExtent([MIN_ZOOM, MAX_ZOOM])
      .on("zoom", (event) => {
        root.attr("transform", event.transform.toString());
        const nextScale = Number(event.transform.k.toFixed(2));
        if (Math.abs(nextScale - scaleRef.current) >= 0.01) {
          scaleRef.current = nextScale;
          onScaleChange(nextScale);
        }
      });

    svg.call(zoom).on("dblclick.zoom", null);
    svg.call(zoom.transform, centeredZoomTransform(canvasSize.width, canvasSize.height, scaleRef.current));

    simulation.on("tick", () => {
      for (const node of graphData.nodes) {
        const padding = node.radius + 10;
        node.x = Math.max(padding, Math.min(canvasSize.width - padding, node.x ?? canvasSize.width / 2));
        node.y = Math.max(padding, Math.min(canvasSize.height - padding, node.y ?? canvasSize.height / 2));
      }

      edgeSelection
        .attr("x1", (edge) => (edge.source as D3GraphNode).x ?? 0)
        .attr("y1", (edge) => (edge.source as D3GraphNode).y ?? 0)
        .attr("x2", (edge) => (edge.target as D3GraphNode).x ?? 0)
        .attr("y2", (edge) => (edge.target as D3GraphNode).y ?? 0);

      nodeSelection.attr("transform", (node) => `translate(${node.x ?? 0},${node.y ?? 0})`);
    });

    zoomRef.current = zoom;
    nodeSelectionRef.current = nodeSelection;
    edgeSelectionRef.current = edgeSelection;
    simulationRef.current = simulation;

    return () => {
      simulation.stop();
      svg.on(".zoom", null);
    };
  }, [canvasSize.height, canvasSize.width, graphData, onScaleChange, onSelectNode]);

  useEffect(() => {
    const svgEl = svgRef.current;
    const zoom = zoomRef.current;
    if (!svgEl || !zoom) return;

    const current = d3.zoomTransform(svgEl);
    if (Math.abs(current.k - scale) < 0.01) return;
    d3.select(svgEl).transition().duration(140).call(zoom.scaleTo, scale);
  }, [scale]);

  useEffect(() => {
    const nodeSelection = nodeSelectionRef.current;
    const edgeSelection = edgeSelectionRef.current;
    if (!nodeSelection || !edgeSelection) return;

    const activeNeighborIds = new Set<string>();
    if (hoveredId) {
      activeNeighborIds.add(hoveredId);
      edgeSelection.each((edge) => {
        if (!edgeTouches(edge, hoveredId)) return;
        activeNeighborIds.add(edgeEndpointId(edge.source));
        activeNeighborIds.add(edgeEndpointId(edge.target));
      });
    }

    const hasHighlight = highlightedIds.size > 0;
    const hasHover = hoveredId !== null;

    edgeSelection
      .classed("ug-d3-edge-active", (edge) => {
        if (hasHover && hoveredId) return edgeTouches(edge, hoveredId);
        if (hasHighlight) return edgeConnectsHighlighted(edge, highlightedIds);
        return edgeTouches(edge, selectedId);
      })
      .classed("ug-d3-edge-dimmed", (edge) => {
        if (hasHover && hoveredId) return !edgeTouches(edge, hoveredId);
        if (hasHighlight) return !edgeConnectsHighlighted(edge, highlightedIds);
        return false;
      });

    nodeSelection
      .classed("ug-d3-node-selected", (node) => node.id === selectedId)
      .classed("ug-d3-node-hot", (node) => activeNeighborIds.has(node.id))
      .classed("ug-d3-node-dimmed", (node) => {
        if (hasHover) return !activeNeighborIds.has(node.id);
        if (hasHighlight && !highlightedIds.has(node.id)) return true;
        return false;
      })
      .classed("ug-d3-node-muted", false);
  }, [highlightedIds, hoveredId, selectedId]);

  return (
    <section className="ug-graph-shell">
      <div className="ug-graph-toolbar">
        <div>
          <div className="ug-graph-title">知识图谱</div>
          <div className="ug-graph-meta">
            {nodes.length} 个节点 · {graphData.edges.length} 条关系
          </div>
        </div>
      </div>

      <div className="ug-zoom-toolbar">
        <Button variant="outline" size="icon-sm" onClick={() => onScaleChange(Math.max(MIN_ZOOM, Number((scale - 0.1).toFixed(2))))}>
          <Minus className="size-3.5" />
        </Button>
        <span>{Math.round(scale * 100)}%</span>
        <Button variant="outline" size="icon-sm" onClick={() => onScaleChange(Math.min(MAX_ZOOM, Number((scale + 0.1).toFixed(2))))}>
          <Plus className="size-3.5" />
        </Button>
      </div>

      <div ref={canvasRef} className="ug-graph-canvas">
        <svg ref={svgRef} className="ug-d3-svg" role="img" aria-label="UG Wiki 知识图谱" />
        {tooltip.visible && (
          <div className="ug-graph-tooltip" style={{ left: tooltip.x, top: tooltip.y }}>
            <div className="ug-graph-tooltip-title">{tooltip.title}</div>
            <div className="ug-graph-tooltip-meta">{tooltip.meta}</div>
            <div className="ug-graph-tooltip-hint">点击查看详情，拖拽调整位置</div>
          </div>
        )}
      </div>
    </section>
  );
}
