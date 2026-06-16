export type GraphNodeType =
  | "domain"
  | "repo"
  | "frontend_app"
  | "code_file"
  | "service"
  | "markdown"
  | "prd"
  | "business_insight"
  | "alert_sop"
  | "api_contract"
  | "term"
  | "flow"
  | "owner"
  | "tracking"
  | "data_asset"
  | "alert_policy"
  | "config";

export type Confidence = "verified" | "high" | "medium" | "inferred" | "conflicted";

export type GraphNode = {
  id: string;
  label: string;
  displayName?: string;
  type: GraphNodeType;
  scene: string[];
  domain?: string;
  confidence?: Confidence;
  path?: string;
  repo?: string;
  app?: string;
  updated?: string;
  summary?: string;
  details?: Array<{ title: string; items: string[] }>;
  evidence?: Array<{ title: string; description: string }>;
  related?: string[];
  x: number;
  y: number;
  size?: "sm" | "md" | "lg";
};

export type GraphEdge = {
  id: string;
  source: string;
  target: string;
  label: string;
};
