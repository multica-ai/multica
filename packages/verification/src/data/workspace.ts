import { z } from "zod";
import { jsonSchema, toJson, type Json } from "../contracts/index.js";
import type { Bindings } from "../sources/bindings.js";
import { digest, canonicalJson } from "../host/storage.js";
import { problem, VerificationError } from "../host/problem.js";
const entity = z.object({ id: z.uuid() }).catchall(jsonSchema);
const attachment = z
  .object({
    id: z.uuid(),
    filename: z.string(),
    content_type: z.string(),
    size_bytes: z.number().int().nonnegative(),
    issue_id: z.string().nullable(),
    comment_id: z.string().nullable(),
    data: z.string(),
    sha256: z.string(),
  })
  .strict();
export const workspaceContentSchema = z
  .object({
    schemaVersion: z.literal(1),
    sourceOrigin: z.url(),
    workspace: entity,
    statuses: z.array(entity),
    workflows: z.array(entity),
    projects: z.array(entity),
    issues: z.array(entity),
    comments: z.array(entity),
    labels: z.array(entity),
    attachments: z.array(attachment),
  })
  .strict();
export type WorkspaceContent = z.infer<typeof workspaceContentSchema>;
const sha = async (data: Uint8Array) =>
  (await import("node:crypto")).createHash("sha256").update(data).digest("hex");
const failure = (message: string): never => {
  throw new VerificationError(
    problem("coverageIncomplete", message, "inspect"),
  );
};
export async function exportWorkspace(
  bindings: Bindings,
  ref: string,
  selection: { projectIds?: string[]; issueIds?: string[] },
): Promise<Json> {
  const binding = bindings.get(ref);
  const get = (path: string) => bindings.request(ref, path);
  const workspace = entity.parse(
    await get(
      `/api/workspaces/${encodeURIComponent(binding.scope.workspaceId)}`,
    ),
  );
  const workspaceId = workspace.id;
  if (workspaceId !== binding.scope.workspaceId)
    failure("The selected workspace changed");
  const projectIds = new Set(selection.projectIds ?? []),
    issueIds = new Set(selection.issueIds ?? []);
  if (!projectIds.size && !issueIds.size)
    failure("Select at least one project or issue");
  const issues: WorkspaceContent["issues"] = [],
    projects: WorkspaceContent["projects"] = [],
    comments: WorkspaceContent["comments"] = [];
  for (const projectId of projectIds) {
    let offset = 0;
    for (;;) {
      const page = z
        .object({
          issues: z.array(entity),
          total: z.number().int().nonnegative(),
        })
        .parse(
          await get(
            `/api/issues?project_id=${encodeURIComponent(projectId)}&limit=100&offset=${offset}`,
          ),
        );
      for (const item of page.issues) issueIds.add(item.id);
      offset += page.issues.length;
      if (offset >= page.total) break;
      if (!page.issues.length || offset > 1000)
        failure(
          "Project issue enumeration is incomplete or exceeds 1000 objects",
        );
    }
  }
  for (const id of issueIds) {
    if (issueIds.size > 1000)
      failure("Issue relationship closure exceeds 1000 objects");
    const issue = entity.parse(
      await get(`/api/issues/${encodeURIComponent(id)}`),
    );
    if (issue.workspace_id !== workspaceId)
      failure("Issue belongs to another workspace");
    if (
      issue.source_context ||
      (issue.properties &&
        Object.keys(z.record(z.string(), jsonSchema).parse(issue.properties))
          .length)
    )
      throw new VerificationError(
        problem(
          "unsupportedCapability",
          "This snapshot format does not import source-context executions or custom property definitions; select supported content",
        ),
      );
    if (typeof issue.parent_issue_id === "string")
      issueIds.add(issue.parent_issue_id);
    if (typeof issue.project_id === "string") projectIds.add(issue.project_id);
    issues.push(issue);
    // The default comments endpoint returns all complete threads. A bounded or
    // projected response is rejected by Bindings rather than labelled complete.
    const rows = z
      .array(entity)
      .parse(await get(`/api/issues/${encodeURIComponent(id)}/comments`));
    if (rows.length > 10000)
      failure("Comment selection exceeds its bounded snapshot scope");
    comments.push(...rows.map((row) => ({ ...row, issue_id: id })));
  }
  for (const id of projectIds) {
    const project = entity.parse(
      await get(`/api/projects/${encodeURIComponent(id)}`),
    );
    if (project.workspace_id !== workspaceId)
      failure("Project belongs to another workspace");
    if (
      typeof project.resource_count === "number" &&
      project.resource_count > 0
    )
      throw new VerificationError(
        problem(
          "unsupportedCapability",
          "Project execution resources are not portable in this content snapshot format; use a supported selection or live read-only pages",
        ),
      );
    projects.push(project);
  }
  for (const issue of issues) {
    const final = z
      .array(entity)
      .parse(await get(`/api/issues/${encodeURIComponent(issue.id)}/comments`))
      .map((row) => ({ ...row, issue_id: issue.id }));
    if (
      canonicalJson(toJson(final)) !==
      canonicalJson(toJson(comments.filter((row) => row.issue_id === issue.id)))
    )
      failure("Source comments changed during export; retry when stable");
  }
  const statuses = z
    .object({ statuses: z.array(entity) })
    .parse(await get("/api/issue-statuses?include_archived=true")).statuses;
  const workflows = z
    .object({ workflows: z.array(entity) })
    .parse(await get("/api/issue-workflows")).workflows;
  const labels = z
    .object({ labels: z.array(entity) })
    .parse(await get("/api/labels?resource_type=issue")).labels;
  const attachments: WorkspaceContent["attachments"] = [],
    attachmentRows = new Map<string, z.infer<typeof entity>>();
  for (const item of [...issues, ...comments]) {
    for (const row of z.array(entity).parse(item.attachments ?? []))
      attachmentRows.set(row.id, row);
  }
  let bytes = 0;
  for (const row of attachmentRows.values()) {
    const metadata = z
      .object({
        id: z.uuid(),
        filename: z.string(),
        content_type: z.string(),
        size_bytes: z
          .number()
          .int()
          .max(25 * 1024 * 1024),
        issue_id: z.string().nullable(),
        comment_id: z.string().nullable(),
      })
      .parse(row);
    const data = await bindings.bytes(
      ref,
      `/api/attachments/${encodeURIComponent(row.id)}/download`,
    );
    if (data.length !== metadata.size_bytes)
      failure("Attachment length differs from source metadata");
    bytes += data.length;
    if (bytes > 40 * 1024 * 1024)
      failure("Workspace attachments exceed snapshot size limits");
    attachments.push({
      ...metadata,
      data: data.toString("base64"),
      sha256: await sha(data),
    });
  }
  // Re-read authoritative entities to detect concurrent changes. This is per
  // entity consistency, not a global database transaction across the source.
  for (const [kind, rows] of [
    ["issues", issues],
    ["projects", projects],
  ] as const)
    for (const row of rows) {
      const final = entity.parse(
        await get(`/api/${kind}/${encodeURIComponent(row.id)}`),
      );
      if (digest(canonicalJson(row)) !== digest(canonicalJson(final)))
        failure("Source entities changed during export; retry when stable");
    }
  const attachmentLinks = new Map<string, string>();
  for (const row of attachmentRows.values())
    for (const key of [
      "url",
      "markdown_url",
      "download_url",
      "attachment_download_url",
    ])
      if (typeof row[key] === "string" && row[key])
        attachmentLinks.set(
          row[key] as string,
          `/api/attachments/${row.id}/download`,
        );
  const stripTransient = (value: Json): Json => {
    if (Array.isArray(value)) return value.map(stripTransient);
    if (value && typeof value === "object") {
      const result: Record<string, Json> = {};
      for (const [key, child] of Object.entries(value)) {
        if (
          [
            "download_url",
            "attachment_download_url",
            "token",
            "access_token",
            "refresh_token",
          ].includes(key)
        )
          continue;
        result[key] =
          key === "url" &&
          typeof value.id === "string" &&
          typeof value.filename === "string"
            ? `/api/attachments/${value.id}/download`
            : stripTransient(child);
      }
      return result;
    }
    if (typeof value === "string")
      for (const [source, target] of attachmentLinks)
        value = value.split(source).join(target);
    return value;
  };
  return stripTransient(
    toJson(
      workspaceContentSchema.parse({
        schemaVersion: 1,
        sourceOrigin: binding.scope.apiOrigin,
        workspace,
        statuses,
        workflows,
        projects,
        issues,
        comments,
        labels,
        attachments,
      }),
    ),
  );
}
