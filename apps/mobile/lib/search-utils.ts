import type { ComponentProps } from "react";
import { Ionicons } from "@expo/vector-icons";
import type { SearchIssueResult } from "@multica/core/types";

export function searchGroupTitle(label: string, total: number) {
  return total > 0 ? `${label} · ${total}` : label;
}

export function issueSnippet(
  issue: SearchIssueResult,
): { icon: ComponentProps<typeof Ionicons>["name"]; text: string } | null {
  if (issue.match_source === "description" && issue.matched_description_snippet) {
    return {
      icon: "document-text-outline",
      text: issue.matched_description_snippet,
    };
  }
  if (issue.match_source === "comment" && issue.matched_comment_snippet) {
    return { icon: "chatbubble-outline", text: issue.matched_comment_snippet };
  }
  return null;
}

export function issueResultTarget(
  issue: SearchIssueResult,
  slug: string,
  query?: string,
) {
  const params = new URLSearchParams();
  if (issue.match_source === "comment" && issue.matched_comment_id) {
    params.set("highlight", issue.matched_comment_id);
    params.set("h", String(Date.now()));
  } else if (issue.match_source === "description") {
    params.set("focus", "description");
  } else if (issue.match_source === "title") {
    params.set("focus", "title");
  }

  const trimmed = query?.trim();
  if (trimmed) params.set("q", trimmed);

  const qs = params.toString();
  return `/${slug}/issue/${issue.id}${qs ? `?${qs}` : ""}`;
}
