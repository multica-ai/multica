// Types of the knowledge taxonomy. The schemas in taxonomy.ts parse the server's
// answers into these; a field the server leaves out takes its default there.

/** The publish policy of a scope. "unknown" is a mode this client cannot read. */
export type KnowledgePublishMode = "manual" | "auto_after_review" | "auto" | "unknown";

/** The default mode, and the knowledge kind's freshness and template. */
export interface KnowledgeKind {
  key: string;
  name: string;
  default_base_key: string;
  template: string;
  review_days: number;
  required_sections: string[];
}

export interface KnowledgeKinds {
  kinds: KnowledgeKind[];
}

/** An admin's edit of a kind. A field left out is kept. */
export interface UpdateKnowledgeKindRequest {
  name?: string;
  template?: string;
  review_days?: number;
  required_sections?: string[];
}

export interface KnowledgePublishPolicy {
  /** "base" or "kind". */
  scope: string;
  key: string;
  mode: KnowledgePublishMode;
  updated_at: string;
}

export interface KnowledgePublishPolicies {
  policies: KnowledgePublishPolicy[];
  default_mode: KnowledgePublishMode;
}

export interface SetKnowledgePolicyRequest {
  scope: "base" | "kind";
  key: string;
  mode: "manual" | "auto_after_review" | "auto";
}

export interface ReviewKnowledgeDocumentRequest {
  verdict: "ok" | "changes";
  note?: string;
}

/** One document a Librarian routine points at. */
export interface KnowledgeLibrarianFinding {
  link_id: string;
  title: string;
  knowledge_base: string;
  knowledge_kind: string;
  squad_id: string;
  reason: string;
  missing_sections: string[];
  other_link_id: string;
}

export interface KnowledgeLibrarianReport {
  job: string;
  ran_at: string;
  counts: Record<string, number>;
  findings: KnowledgeLibrarianFinding[];
  truncated: boolean;
}

export interface KnowledgeHealthJob {
  job: string;
  ran_at: string;
  report: KnowledgeLibrarianReport;
}

export interface KnowledgeHealth {
  jobs: KnowledgeHealthJob[];
}

export interface KnowledgeEvalCase {
  id: string;
  question: string;
  expected_kind: string;
  expected_document_id: string;
  owner_squad_id: string | null;
  created_by_type: string;
  created_at: string;
}

export interface KnowledgeEvalCases {
  cases: KnowledgeEvalCase[];
}

export interface CreateKnowledgeEvalCaseRequest {
  question: string;
  expected_kind?: string;
  expected_document_id?: string;
  owner_squad_id?: string;
}

export interface KnowledgeEvalResult {
  case_id: string;
  question: string;
  expected_kind: string;
  expected_document_id: string;
  hit: boolean;
  rank: number;
}

/** "offline": WeKnora did not answer, and nothing was recorded. */
export interface KnowledgeEvalRun {
  id: string;
  state: "ready" | "offline" | "unknown";
  total: number;
  hits: number;
  hit_rate: number;
  ran_at: string;
  results: KnowledgeEvalResult[];
}

export interface KnowledgeEvalRuns {
  runs: KnowledgeEvalRun[];
}
