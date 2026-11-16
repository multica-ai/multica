import { getServerConfig } from "@/data/server-config-store";
import { joinServerPath } from "./server-url";

/** Web's canonical detail route is plural; native navigation is singular. */
export function issueWebUrl(workspace: string, identifier: string, commentId?: string): string {
  const path = `/${encodeURIComponent(workspace)}/issues/${encodeURIComponent(identifier)}`;
  return joinServerPath(getServerConfig().webUrl, path) +
    (commentId ? `#comment-${encodeURIComponent(commentId)}` : "");
}
