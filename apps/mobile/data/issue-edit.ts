import type { UpdateIssueRequest } from "@multica/core/types";

/**
 * Mobile keeps text edits last-write-wins until it has a conflict comparison
 * flow. Sending title_base/description_base without that recovery UI would
 * turn a real 409 into an unrecoverable retry loop for the in-memory draft.
 */
export function buildIssueTextUpdate(
  title: string,
  description: string,
): UpdateIssueRequest {
  return {
    title: title.trim(),
    description: description.trim(),
  };
}
