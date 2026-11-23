export type WorkflowTemplateId = "delivery" | "content" | "community";

/**
 * Starting points for a new workflow (MUL-7420). Each lists only its middle
 * columns; `status` names a status in the library, `name` creates one (or
 * reuses a status of that name) when the workflow is saved. Names are i18n
 * keys under `workflows.templates.<id>.statuses`.
 */
export const WORKFLOW_TEMPLATES: {
  id: WorkflowTemplateId;
  columns: ({ status: string } | { name: string; category: "unstarted" | "started" })[];
}[] = [
  { id: "delivery", columns: [{ status: "in_progress" }, { status: "in_review" }, { name: "qa", category: "started" }] },
  {
    id: "content",
    columns: [
      { name: "drafting", category: "started" },
      { name: "editing", category: "started" },
      { name: "scheduled", category: "started" },
    ],
  },
  {
    id: "community",
    columns: [
      { name: "triage", category: "unstarted" },
      { name: "replying", category: "started" },
      { name: "waiting", category: "started" },
    ],
  },
];
