// packages/core/jira/metadata-keys.ts

/** The literal value stored under the `source` metadata key for issues that
 *  originated from a Jira sync. Lets the UI distinguish Jira issues from
 *  user-created ones without any server-side schema change. */
export const JIRA_SOURCE_VALUE = "jira" as const;

/** Per-issue metadata keys used by the Jira sync. All values are primitive
 *  strings (the issue metadata API only accepts string/number/bool). */
export const JIRA_METADATA_KEYS = {
  source: "source",
  jiraKey: "jira_key",
  jiraUrl: "jira_url",
  jiraStatus: "jira_status",
  jiraUpdatedAt: "jira_updated_at",
  jiraCommentsSyncedAt: "jira_comments_synced_at",
} as const;
