export interface PromptInspectorSection {
  id: string;
  title: string;
  content: string;
  tokens: number;
}

export interface PromptInspectorLayer {
  id: string;
  title: string;
  sourceKind: "repository" | "server" | "sidecar" | string;
  injectionPoint: string;
  filePath: string;
  sourceUrl?: string;
  content: string;
  tokens: number;
  unavailable?: boolean;
  unavailableNote?: string;
  sections?: PromptInspectorSection[];
}

export interface PromptInspectorOverlap {
  sectionA: string;
  sectionB: string;
  overlapPct: number;
  sampleText?: string;
}

export interface PromptInspectorDuplication {
  totalTokens: number;
  duplicateTokens: number;
  duplicatePct: number;
  topOverlapSections: PromptInspectorOverlap[];
}

export interface PromptInspectorPayload {
  provider: string;
  layers: PromptInspectorLayer[];
  /** Sum of all injection layers (full composed system prompt). */
  composedTokens: number;
  sections: PromptInspectorSection[];
  duplication: PromptInspectorDuplication;
  fullMarkdown: string;
}

export interface PromptInspectorResponse {
  provider: string;
  repoUrl?: string;
  inspection: PromptInspectorPayload;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function num(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value) ? value : 0;
}

function str(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function parseSection(row: unknown): PromptInspectorSection | null {
  if (!isRecord(row)) return null;
  const id = str(row.id);
  if (!id) return null;
  return {
    id,
    title: str(row.title) || id,
    content: str(row.content),
    tokens: num(row.tokens),
  };
}

function parseLayer(row: unknown): PromptInspectorLayer | null {
  if (!isRecord(row)) return null;
  const id = str(row.id);
  if (!id) return null;
  const sections: PromptInspectorSection[] = [];
  if (Array.isArray(row.sections)) {
    for (const item of row.sections) {
      const parsed = parseSection(item);
      if (parsed) sections.push(parsed);
    }
  }
  return {
    id,
    title: str(row.title) || id,
    sourceKind: str(row.source_kind) || "server",
    injectionPoint: str(row.injection_point),
    filePath: str(row.file_path),
    sourceUrl: str(row.source_url) || undefined,
    content: str(row.content),
    tokens: num(row.tokens),
    unavailable: row.unavailable === true,
    unavailableNote: str(row.unavailable_note) || undefined,
    sections: sections.length > 0 ? sections : undefined,
  };
}

function parseOverlap(row: unknown): PromptInspectorOverlap | null {
  if (!isRecord(row)) return null;
  return {
    sectionA: str(row.section_a),
    sectionB: str(row.section_b),
    overlapPct: num(row.overlap_pct),
    sampleText: str(row.sample_text) || undefined,
  };
}

function parseDuplication(row: unknown): PromptInspectorDuplication {
  if (!isRecord(row)) {
    return {
      totalTokens: 0,
      duplicateTokens: 0,
      duplicatePct: 0,
      topOverlapSections: [],
    };
  }
  const overlaps: PromptInspectorOverlap[] = [];
  if (Array.isArray(row.top_overlap_sections)) {
    for (const item of row.top_overlap_sections) {
      const parsed = parseOverlap(item);
      if (parsed) overlaps.push(parsed);
    }
  }
  return {
    totalTokens: num(row.total_tokens),
    duplicateTokens: num(row.duplicate_tokens),
    duplicatePct: num(row.duplicate_pct),
    topOverlapSections: overlaps,
  };
}

function parseInspection(row: unknown): PromptInspectorPayload {
  if (!isRecord(row)) {
    return {
      provider: "claude",
      layers: [],
      composedTokens: 0,
      sections: [],
      duplication: parseDuplication(null),
      fullMarkdown: "",
    };
  }
  const layers: PromptInspectorLayer[] = [];
  if (Array.isArray(row.layers)) {
    for (const item of row.layers) {
      const parsed = parseLayer(item);
      if (parsed) layers.push(parsed);
    }
  }
  const sections: PromptInspectorSection[] = [];
  if (Array.isArray(row.sections)) {
    for (const item of row.sections) {
      const parsed = parseSection(item);
      if (parsed) sections.push(parsed);
    }
  }
  const composedTokens = num(row.composed_tokens);
  const layerSum = layers.reduce(
    (n, layer) => n + (layer.unavailable ? 0 : layer.tokens),
    0,
  );
  return {
    provider: str(row.provider) || "claude",
    layers,
    composedTokens: composedTokens > 0 ? composedTokens : layerSum,
    sections,
    duplication: parseDuplication(row.duplication),
    fullMarkdown: str(row.full_markdown),
  };
}

export function parsePromptInspectorResponse(
  body: unknown,
): PromptInspectorResponse {
  if (!isRecord(body)) {
    return {
      provider: "claude",
      inspection: parseInspection(null),
    };
  }
  return {
    provider: str(body.provider) || "claude",
    repoUrl: str(body.repo_url) || undefined,
    inspection: parseInspection(body.inspection),
  };
}

export function sourceKindLabel(kind: string): string {
  switch (kind) {
    case "repository":
      return "Repository";
    case "server":
      return "Server (Multica)";
    case "sidecar":
      return "Sidecar file";
    default:
      return kind;
  }
}
