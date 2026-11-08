export const LINK_ICON_KEYS = ["globe", "link", "book-open", "chart-bar", "code", "terminal", "folder", "file-text", "database", "server", "rocket", "shield"] as const;
export interface LinkContext { workspaceSlug: string; workspaceId: string; projectId?: string }

export function expandLinkUrl(template: string, context: LinkContext): string {
  const values: Record<string, string | undefined> = { workspace_slug: context.workspaceSlug, workspace_id: context.workspaceId, project_id: context.projectId };
  return template.replace(/\{([^{}]*)\}/g, (token, key: string) => {
    const value = values[key];
    if (value === undefined) throw new Error(`Unresolved variable ${token}`);
    return encodeURIComponent(value);
  });
}

export type LinkUrlError = "length" | "variable" | "project" | "location" | "https" | "userinfo" | null;
export function validateLinkUrl(template: string, project = false): LinkUrlError {
  if (new TextEncoder().encode(template).length > 2048) return "length";
  for (const match of template.matchAll(/\{([^{}]*)\}/g)) {
    if (!["workspace_slug", "workspace_id", "project_id"].includes(match[1]!)) return "variable";
    if (match[1] === "project_id" && !project) return "project";
  }
  const sample = template.replace(/\{([^{}]*)\}/g, "sample");
  if (/[{}\\\r\n\t]/.test(sample)) return "https";
  try {
    const url = new URL(sample);
    if (url.protocol !== "https:" || !url.hostname || !template.startsWith("https://")) return "https";
    if (url.username || url.password || /^https:\/\/[^/?#]*@/.test(template)) return "userinfo";
    const authority = template.match(/^.*?:\/\/[^/?#]*/)?.[0] ?? "";
    if (/[{}]/.test(authority) || /[{}]/.test(template.split("#").slice(1).join("#"))) return "location";
    return null;
  } catch { return "https"; }
}
