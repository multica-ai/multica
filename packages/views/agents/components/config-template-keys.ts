// Shared query-key factory for agent config templates. Lives in its own module
// so the dialog and the per-template editor can both depend on it without a
// circular import between the two component files.

export const configTemplateKeys = {
  all: (workspaceId: string) =>
    ["agent-config-templates", workspaceId] as const,
  list: (workspaceId: string, scope?: string) =>
    [...configTemplateKeys.all(workspaceId), "list", scope] as const,
  detail: (workspaceId: string, templateId: string) =>
    [...configTemplateKeys.all(workspaceId), "detail", templateId] as const,
};
