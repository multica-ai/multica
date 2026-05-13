// JEH-1152 — toast helper for the structured 403 the cerebro runtime-access
// gate emits when a non-admin tries to create an agent on a runtime that
// hasn't been approved by their group.
//
// The backend returns:
//
//   { error: "This runtime must be approved by your group's admin...",
//     code: "runtime_not_approved_by_group",
//     approval_issue: { id, identifier, title } }
//
// `error` is shown as the toast title; the approval_issue (when present) is
// surfaced as a sonner action so the user can jump to the auto-created issue
// instead of just being told "ask your admin". When the body lacks the
// structured fields (older server, or issue-creation side-effect failed) we
// fall through and the caller's generic toast.error runs.
//
// Lives in cerebro-groups/views because runtime approval is a group-permission
// concept; importing it from the upstream-zone create-agent-dialog requires a
// `// CEREBRO-PATCH(runtime-approval-toast):` marker on the import line.
import { ApiError } from "@multica/core/api";
import { toast } from "sonner";

const RUNTIME_NOT_APPROVED_CODE = "runtime_not_approved_by_group";

type ApprovalIssueRef = {
  id: string;
  identifier: string;
  title: string;
};

// Narrowly type-guard the response body. ApiError.body is `unknown` because
// the server contract drifts (per API Response Compatibility rules) — we
// can't assume the shape, so we check every field before reading it.
function readApprovalIssueRef(body: unknown): ApprovalIssueRef | null {
  if (typeof body !== "object" || body === null) return null;
  const ref = (body as { approval_issue?: unknown }).approval_issue;
  if (typeof ref !== "object" || ref === null) return null;
  const id = (ref as { id?: unknown }).id;
  const identifier = (ref as { identifier?: unknown }).identifier;
  const title = (ref as { title?: unknown }).title;
  if (typeof id !== "string" || typeof identifier !== "string" || typeof title !== "string") {
    return null;
  }
  return { id, identifier, title };
}

function readErrorCode(body: unknown): string | null {
  if (typeof body !== "object" || body === null) return null;
  const code = (body as { code?: unknown }).code;
  return typeof code === "string" ? code : null;
}

export type RuntimeApprovalToastDeps = {
  // Issue URL builder — wire to `useWorkspacePaths().issueDetail` at the call
  // site. Kept as a callable so this helper stays headless (no react hooks
  // here; the dialog already owns the workspace context).
  issuePath: (id: string) => string;
  // Navigation push — wire to `useNavigation().push`. Pulled out of the
  // helper so it stays platform-agnostic (web and desktop share this code).
  push: (path: string) => void;
};

// showRuntimeApprovalToastIfApplicable returns true when the error was a
// runtime-approval denial (and the toast was shown). Callers must then skip
// their own toast.error fallback so the user doesn't see two stacked toasts.
// Returns false for any other error shape — caller falls through.
export function showRuntimeApprovalToastIfApplicable(
  err: unknown,
  deps: RuntimeApprovalToastDeps,
): boolean {
  if (!(err instanceof ApiError) || err.status !== 403) return false;
  if (readErrorCode(err.body) !== RUNTIME_NOT_APPROVED_CODE) return false;

  const approval = readApprovalIssueRef(err.body);
  if (!approval) {
    // Server denied with the right code but didn't include an issue ref
    // (issue creation failed server-side). Still show the new copy so the
    // user gets the actionable message; just skip the action button.
    toast.error(err.message);
    return true;
  }

  toast.error(err.message, {
    description: `${approval.identifier} · ${approval.title}`,
    duration: 12_000,
    action: {
      label: "View issue",
      onClick: () => deps.push(deps.issuePath(approval.id)),
    },
  });
  return true;
}
