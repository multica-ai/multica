import type {
  BatchCreateIssueInput,
  BatchCreateIssueRowError,
  BatchCreateIssuesRequest,
  IssueAssigneeType,
  IssueStatus,
} from "../types";

export const BATCH_ISSUE_TEMPLATE_FILENAME = "multica-batch-issues-template.json";

const ISSUE_STATUSES = new Set<IssueStatus>([
  "backlog",
  "todo",
  "in_progress",
  "in_review",
  "done",
  "blocked",
  "cancelled",
]);

const ASSIGNEE_TYPES = new Set<IssueAssigneeType>(["member", "agent"]);

const ROOT_FIELDS = new Set(["issues", "validate_only", "confirm_batch_create"]);
const ROW_FIELDS = new Set([
  "title",
  "description",
  "status",
  "assignee_type",
  "assignee_id",
  "project_id",
]);

export type BatchCreateIssuesParseResult =
  | { ok: true; request: BatchCreateIssuesRequest; errors: [] }
  | { ok: false; request?: undefined; errors: BatchCreateIssueRowError[] };

export interface BatchIssueTemplateReferences {
  memberAssigneeId?: string | null;
  agentAssigneeId?: string | null;
  projectId?: string | null;
}

export function createBatchIssueTemplateJSON(
  references: BatchIssueTemplateReferences = {},
): string {
  const memberAssigneeId =
    references.memberAssigneeId ?? "aaaaaaaa-1111-1111-1111-111111111111";
  const agentAssigneeId =
    references.agentAssigneeId ?? "cccccccc-3333-3333-3333-333333333333";
  const projectId =
    references.projectId ?? "bbbbbbbb-2222-2222-2222-222222222222";

  return `${JSON.stringify(
    {
      issues: [
        {
          title: "Fix login empty state copy",
          description: "Markdown description is supported.",
          status: "todo",
          assignee_type: "member",
          assignee_id: memberAssigneeId,
          project_id: projectId,
        },
        {
          title: "Audit importer logs",
          description:
            "This starts in backlog, so the assigned agent will not run until the issue moves out of backlog.",
          status: "backlog",
          assignee_type: "agent",
          assignee_id: agentAssigneeId,
        },
      ],
    },
    null,
    2,
  )}\n`;
}

export function parseBatchCreateIssuesJSON(text: string): BatchCreateIssuesParseResult {
  let parsed: unknown;
  try {
    parsed = JSON.parse(text);
  } catch (error) {
    return {
      ok: false,
      errors: [rowError(0, "json", "invalid_json", formatJSONParseError(error))],
    };
  }

  if (!isPlainObject(parsed)) {
    return {
      ok: false,
      errors: [rowError(0, "root", "invalid_root", "Root value must be a JSON object.")],
    };
  }

  const errors: BatchCreateIssueRowError[] = [];
  for (const key of Object.keys(parsed)) {
    if (!ROOT_FIELDS.has(key)) {
      errors.push(rowError(0, key, "unsupported_field", `${key} is not supported at the root.`));
    }
  }

  const issues = parsed.issues;
  if (!Array.isArray(issues) || issues.length === 0) {
    errors.push(rowError(0, "issues", "required", "issues must be a non-empty array."));
    return { ok: false, errors };
  }

  const normalized: BatchCreateIssueInput[] = [];
  issues.forEach((issue, index) => {
    const row = index + 1;
    if (!isPlainObject(issue)) {
      errors.push(rowError(row, "issues", "invalid_row", "Each issue row must be a JSON object."));
      return;
    }

    for (const key of Object.keys(issue)) {
      if (!ROW_FIELDS.has(key)) {
        errors.push(
          rowError(row, key, "unsupported_field", `${key} is not supported for batch issue creation.`),
        );
      }
    }

    const title = readOptionalString(issue, "title", row, errors);
    const description = readOptionalString(issue, "description", row, errors);
    const status = readOptionalString(issue, "status", row, errors);
    const assigneeType = readOptionalString(issue, "assignee_type", row, errors);
    const assigneeId = readOptionalString(issue, "assignee_id", row, errors);
    const projectId = readOptionalString(issue, "project_id", row, errors);

    const trimmedTitle = title?.trim() ?? "";
    if (!trimmedTitle) {
      errors.push(rowError(row, "title", "required", "title is required."));
    }

    const trimmedStatus = status?.trim();
    if (trimmedStatus && !ISSUE_STATUSES.has(trimmedStatus as IssueStatus)) {
      errors.push(
        rowError(
          row,
          "status",
          "invalid_status",
          "status must be one of backlog, todo, in_progress, in_review, done, blocked, or cancelled.",
        ),
      );
    }

    const trimmedAssigneeType = assigneeType?.trim();
    const trimmedAssigneeId = assigneeId?.trim();
    if (!!trimmedAssigneeType !== !!trimmedAssigneeId) {
      errors.push(
        rowError(
          row,
          trimmedAssigneeType ? "assignee_id" : "assignee_type",
          "assignee_pair_required",
          "assignee_type and assignee_id must be provided together.",
        ),
      );
    }
    if (trimmedAssigneeType && !ASSIGNEE_TYPES.has(trimmedAssigneeType as IssueAssigneeType)) {
      errors.push(
        rowError(row, "assignee_type", "invalid_assignee_type", "assignee_type must be member or agent."),
      );
    }

    normalized.push({
      title: trimmedTitle,
      ...(description !== undefined ? { description } : {}),
      ...(trimmedStatus ? { status: trimmedStatus as IssueStatus } : {}),
      ...(trimmedAssigneeType ? { assignee_type: trimmedAssigneeType as IssueAssigneeType } : {}),
      ...(trimmedAssigneeId ? { assignee_id: trimmedAssigneeId } : {}),
      ...(projectId?.trim() ? { project_id: projectId.trim() } : {}),
    });
  });

  if (errors.length > 0) {
    return { ok: false, errors };
  }
  return { ok: true, request: { issues: normalized }, errors: [] };
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === "object" && !Array.isArray(value);
}

function readOptionalString(
  value: Record<string, unknown>,
  field: string,
  row: number,
  errors: BatchCreateIssueRowError[],
): string | undefined {
  if (!(field in value) || value[field] === null) {
    return undefined;
  }
  if (typeof value[field] !== "string") {
    errors.push(rowError(row, field, "invalid_type", `${field} must be a string.`));
    return undefined;
  }
  return value[field];
}

function rowError(row: number, field: string, code: string, message: string): BatchCreateIssueRowError {
  return { row, field, code, message };
}

function formatJSONParseError(error: unknown): string {
  if (!(error instanceof Error)) {
    return "JSON could not be parsed.";
  }
  const detail = error.message.trim().replace(/\.$/, "");
  return detail ? `JSON could not be parsed: ${detail}.` : "JSON could not be parsed.";
}
