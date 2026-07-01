import { JIRA_METADATA_KEYS, JIRA_SOURCE_VALUE } from "./metadata-keys";
import { jiraIssueToCreateRequest, jiraIssueToUpdateRequest } from "./mapping";
import { adfToText } from "./adf";
import { parseJiraSearch, parseJiraIssue } from "./types";
import type { JiraIssue, SyncDeps, SyncResult } from "./types";

interface ExistingRef {
  issueId: string;
  jiraUpdatedAt: string;
  commentsSyncedAt: string;
}

/** Pull Jira issues matching the configured JQL into Multica. One-way:
 *  create unseen issues, update changed ones (Jira authoritative), skip
 *  unchanged. Dedup is by the `jira_key` metadata key on existing issues. */
export async function syncJiraIssues(deps: SyncDeps): Promise<SyncResult> {
  const { transport, config } = deps;
  const result: SyncResult = { created: 0, updated: 0, skipped: 0, commentsAdded: 0, errors: [] };

  const index = await buildJiraKeyIndex(deps);

  const search = parseJiraSearch(
    await transport({ method: "GET", path: searchPath(config.jql) }),
  );

  for (const issue of search.issues) {
    try {
      await syncOne(deps, issue, index, null, result);
    } catch (err) {
      result.errors.push({ jiraKey: issue.key, message: errMessage(err) });
    }
  }
  return result;
}

/** Delete every Multica issue created by the Jira sync (metadata
 *  source = "jira"), so the next sync repopulates from a clean slate under a
 *  new JQL. User-created issues (no jira source marker) are never touched.
 *  Returns how many issues were deleted. */
export async function clearSyncedJiraIssues(
  api: SyncDeps["api"],
): Promise<{ deleted: number }> {
  const { issues } = await api.listIssues({
    metadata: { [JIRA_METADATA_KEYS.source]: JIRA_SOURCE_VALUE },
    limit: 1000,
  });
  const ids = issues
    .filter(
      (i) =>
        (i as { metadata?: Record<string, unknown> }).metadata?.[
          JIRA_METADATA_KEYS.source
        ] === JIRA_SOURCE_VALUE,
    )
    .map((i) => i.id);
  if (ids.length === 0) return { deleted: 0 };
  await api.batchDeleteIssues(ids);
  return { deleted: ids.length };
}

async function syncOne(
  deps: SyncDeps,
  issue: JiraIssue,
  index: Map<string, ExistingRef>,
  parentIssueId: string | null,
  result: SyncResult,
): Promise<string> {
  const { api, config, currentMemberId } = deps;
  const existing = index.get(issue.key);
  let issueId: string;

  if (!existing) {
    const req = jiraIssueToCreateRequest(issue, config.statusMapping, currentMemberId);
    if (parentIssueId) req.parent_issue_id = parentIssueId;
    const created = await api.createIssue(req);
    issueId = created.id;
    await stampMetadata(deps, issueId, issue, issue.fields.updated);
    index.set(issue.key, {
      issueId,
      jiraUpdatedAt: issue.fields.updated,
      commentsSyncedAt: issue.fields.updated,
    });
    result.created += 1;
  } else {
    issueId = existing.issueId;
    if (issue.fields.updated > existing.jiraUpdatedAt) {
      await api.updateIssue(issueId, jiraIssueToUpdateRequest(issue, config.statusMapping));
      await api.setIssueMetadata(issueId, JIRA_METADATA_KEYS.jiraStatus, issue.fields.status.name);
      await api.setIssueMetadata(issueId, JIRA_METADATA_KEYS.jiraUpdatedAt, issue.fields.updated);
      existing.jiraUpdatedAt = issue.fields.updated;
      result.updated += 1;
    } else {
      result.skipped += 1;
    }
  }

  await syncComments(deps, issueId, issue, index, result);
  await syncSubtasks(deps, issue, issueId, index, result);
  return issueId;
}

async function buildJiraKeyIndex(deps: SyncDeps): Promise<Map<string, ExistingRef>> {
  const index = new Map<string, ExistingRef>();
  const { issues } = await deps.api.listIssues({
    metadata: { [JIRA_METADATA_KEYS.source]: JIRA_SOURCE_VALUE },
    limit: 1000,
  });
  for (const i of issues) {
    const md = (i as { metadata?: Record<string, unknown> }).metadata ?? {};
    const key = md[JIRA_METADATA_KEYS.jiraKey];
    if (typeof key === "string" && key) {
      index.set(key, {
        issueId: i.id,
        jiraUpdatedAt: String(md[JIRA_METADATA_KEYS.jiraUpdatedAt] ?? ""),
        commentsSyncedAt: String(md[JIRA_METADATA_KEYS.jiraCommentsSyncedAt] ?? ""),
      });
    }
  }
  return index;
}

async function stampMetadata(
  deps: SyncDeps,
  issueId: string,
  issue: JiraIssue,
  commentsHighWater: string,
): Promise<void> {
  const { api, config } = deps;
  await api.setIssueMetadata(issueId, JIRA_METADATA_KEYS.source, JIRA_SOURCE_VALUE);
  await api.setIssueMetadata(issueId, JIRA_METADATA_KEYS.jiraKey, issue.key);
  await api.setIssueMetadata(issueId, JIRA_METADATA_KEYS.jiraUrl, `${config.siteUrl}/browse/${issue.key}`);
  await api.setIssueMetadata(issueId, JIRA_METADATA_KEYS.jiraStatus, issue.fields.status.name);
  await api.setIssueMetadata(issueId, JIRA_METADATA_KEYS.jiraUpdatedAt, issue.fields.updated);
  await api.setIssueMetadata(issueId, JIRA_METADATA_KEYS.jiraCommentsSyncedAt, commentsHighWater);
}

async function syncComments(
  deps: SyncDeps,
  issueId: string,
  issue: JiraIssue,
  index: Map<string, ExistingRef>,
  result: SyncResult,
): Promise<void> {
  const ref = index.get(issue.key);
  const highWater = ref?.commentsSyncedAt ?? "";
  let maxCreated = highWater;
  const fresh = issue.fields.comment.comments.filter((c) => c.created > highWater);
  for (const c of fresh) {
    const author = c.author?.displayName ? `**${c.author.displayName}** (Jira):\n\n` : "";
    await deps.api.createComment(issueId, `${author}${adfToText(c.body)}`);
    result.commentsAdded += 1;
    if (c.created > maxCreated) maxCreated = c.created;
  }
  if (maxCreated !== highWater) {
    await deps.api.setIssueMetadata(issueId, JIRA_METADATA_KEYS.jiraCommentsSyncedAt, maxCreated);
    if (ref) ref.commentsSyncedAt = maxCreated;
  }
}

async function syncSubtasks(
  deps: SyncDeps,
  parent: JiraIssue,
  parentIssueId: string,
  index: Map<string, ExistingRef>,
  result: SyncResult,
): Promise<void> {
  for (const sub of parent.fields.subtasks) {
    try {
      const raw = await deps.transport({ method: "GET", path: issuePath(sub.key) });
      const child = parseJiraIssue(raw);
      if (child) await syncOne(deps, child, index, parentIssueId, result);
    } catch (err) {
      result.errors.push({ jiraKey: sub.key, message: errMessage(err) });
    }
  }
}

function searchPath(jql: string): string {
  // Atlassian removed the legacy /rest/api/3/search endpoint (CHANGE-2046);
  // the enhanced search lives at /rest/api/3/search/jql. It returns
  // { issues, nextPageToken, isLast } with no `total` field — our schema
  // defaults total to 0, so parsing is unaffected.
  return `/rest/api/3/search/jql?jql=${encodeURIComponent(jql)}&fields=summary,description,status,priority,duedate,updated,subtasks,comment&maxResults=100`;
}

function issuePath(key: string): string {
  return `/rest/api/3/issue/${encodeURIComponent(key)}?fields=summary,description,status,priority,duedate,updated,subtasks,comment`;
}

function errMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
