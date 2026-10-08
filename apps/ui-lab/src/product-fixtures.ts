import en from "./locales/en.json";
import zh from "./locales/zh.json";
import { productLocale, type LabLocale } from "./locale";
import type {
  Issue,
  User,
  Workspace,
  MemberWithUser,
  StorageAdapter,
  IssueTableQuerySpec,
  Comment,
} from "@multica/core/types";
import { STATUS_ORDER } from "@multica/core/issues/config";
import { ApiClient } from "@multica/core/api";

const time = "2026-09-14T06:00:00Z";
export const workspace: Workspace = {
  id: "10000000-0000-4000-8000-000000000001",
  slug: "ui-lab",
  name: "Multica",
  description: "UI Lab fixture workspace",
  context: null,
  settings: {},
  repos: [],
  issue_prefix: "MUL",
  avatar_url: null,
  created_at: time,
  updated_at: time,
};
export const user: User = {
  id: "10000000-0000-4000-8000-000000000002",
  name: "Jiayuan",
  email: "designer@example.test",
  avatar_url: null,
  onboarded_at: time,
  onboarding_questionnaire: {},
  starter_content_state: "imported",
  language: "en",
  profile_description: "",
  timezone: "Asia/Shanghai",
  created_at: time,
  updated_at: time,
};
export const members: MemberWithUser[] = [
  {
    id: "10000000-0000-4000-8000-000000000003",
    workspace_id: workspace.id,
    user_id: user.id,
    role: "owner",
    created_at: time,
    name: user.name,
    email: user.email,
    avatar_url: null,
  },
];
const titles = en.fixtures.issues.titles;
export const issues: Issue[] = titles.map((title, index) => ({
  id: `20000000-0000-4000-8000-${String(index + 1).padStart(12, "0")}`,
  workspace_id: workspace.id,
  number: 241 + index,
  identifier: `MUL-${241 + index}`,
  title,
  description: en.fixtures.issues.description,
  status: index < 3 ? "in_progress" : index < 6 ? "todo" : "done",
  priority: index % 2 ? "medium" : "high",
  assignee_type: "member",
  assignee_id: user.id,
  creator_type: "member",
  creator_id: user.id,
  parent_issue_id: null,
  project_id: null,
  position: index * 1000,
  stage: null,
  start_date: null,
  due_date: null,
  metadata: {},
  properties: {},
  labels: [],
  created_at: time,
  updated_at: time,
  revision: 1,
}));
export const memoryStorage = (): StorageAdapter => {
  const values = new Map<string, string>();
  return {
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => {
      values.set(key, value);
    },
    removeItem: (key) => {
      values.delete(key);
    },
    keys: () => [...values.keys()],
  };
};

// Only data is substituted. Views, hooks, stores and mutations are the production modules.
// There is deliberately no fallback to ApiClient's network implementation.
export function createFixtureApi(getLocale: () => LabLocale = () => "en") {
  const messages = () => (getLocale() === "zh" ? zh.fixtures : en.fixtures);
  // Translate untouched fixtures at the data boundary; preserve user edits.
  const localizeIssue = (issue: Issue): Issue => {
    const index = issues.findIndex((original) => original.id === issue.id);
    const original = issues[index];
    return {
      ...issue,
      title:
        original && issue.title === original.title
          ? messages().issues.titles[index]!
          : issue.title,
      description:
        original && issue.description === original.description
          ? messages().issues.description
          : issue.description,
    };
  };
  const localizeComment = (comment: Comment): Comment => ({
    ...comment,
    content:
      comment.id === "30000000-0000-4000-8000-000000000001"
        ? messages().comments.first
        : comment.content,
  });
  let rows = structuredClone(issues);
  const comments: Comment[] = [
    {
      id: "30000000-0000-4000-8000-000000000001",
      issue_id: issues[0]!.id,
      author_type: "member",
      author_id: user.id,
      content: en.fixtures.comments.first,
      type: "comment",
      parent_id: null,
      reactions: [],
      attachments: [],
      created_at: time,
      updated_at: time,
      revision: 1,
      resolved_at: null,
      resolved_by_type: null,
      resolved_by_id: null,
    },
  ];
  const queryRows = (query: IssueTableQuerySpec) =>
    rows.filter((issue) => {
      const filters = query.filters;
      return (
        (!filters.statuses?.length ||
          filters.statuses.includes(issue.status)) &&
        (!filters.priorities?.length ||
          filters.priorities.includes(issue.priority)) &&
        (!query.search ||
          `${issue.identifier} ${localizeIssue(issue).title}`
            .toLowerCase()
            .includes(query.search.toLowerCase())) &&
        (!filters.working_issue_ids ||
          filters.working_issue_ids.includes(issue.id)) &&
        (!("assignee_types" in query.scope) ||
          !query.scope.assignee_types?.length ||
          query.scope.assignee_types.includes(issue.assignee_type!))
      );
    });
  const handlers: Partial<ApiClient> = {
    getBaseUrl: () => "/ui-lab-fixtures",
    getMe: async () => ({ ...user, language: productLocale(getLocale()) }),
    listWorkspaces: async () => [workspace],
    getWorkspace: async () => workspace,
    listMembers: async () => members,
    listSquads: async () => [],
    listAgents: async () => [],
    listRuntimes: async () => [],
    listProjects: async () => ({ projects: [], total: 0 }),
    listIssues: async (params) => {
      const filtered = rows.filter(
        (issue) =>
          (!params?.statuses?.length ||
            params.statuses.includes(issue.status)) &&
          (!params?.ids || params.ids.includes(issue.id)) &&
          (!params?.priorities?.length ||
            params.priorities.includes(issue.priority)),
      );
      return {
        issues: filtered
          .slice(
            params?.offset ?? 0,
            (params?.offset ?? 0) + (params?.limit ?? 100),
          )
          .map(localizeIssue),
        total: filtered.length,
      };
    },
    listIssueTableRows: async (request) => {
      const matched = queryRows(request.query);
      const group = request.group_key?.replace(/^status(?:_category)?:/, "");
      const branch = request.parent_id
        ? []
        : matched.filter((issue) => !group || issue.status === group);
      return {
        query_fingerprint: JSON.stringify(request.query),
        group_key: request.group_key,
        parent_id: request.parent_id,
        total: matched.length,
        branch_total: branch.length,
        rows: branch.map((issue) => ({
          issue: localizeIssue(issue),
          direct_child_count: 0,
        })),
        next_cursor: null,
      };
    },
    listIssueTableGroups: async (request) => ({
      query_fingerprint: JSON.stringify(request.query),
      total: queryRows(request.query).length,
      groups: STATUS_ORDER.map((status) => ({
        key: `status:${status}`,
        value: { kind: "status", status },
        count: queryRows(request.query).filter(
          (issue) => issue.status === status,
        ).length,
      })),
      next_cursor: null,
    }),
    listIssueTableFacets: async (request) => {
      const matched = queryRows(request.query);
      return {
        query_fingerprint: JSON.stringify(request.query),
        total: matched.length,
        facets: request.facets.map((facet) => ({
          ...facet,
          values:
            facet.kind === "status"
              ? STATUS_ORDER.map((key) => ({
                  key,
                  count: matched.filter((issue) => issue.status === key).length,
                }))
              : [],
        })),
      };
    },
    getIssue: async (id) => {
      const issue = rows.find((row) => row.id === id || row.identifier === id);
      if (!issue) throw new Error(`Unknown fixture issue: ${id}`);
      return structuredClone(localizeIssue(issue));
    },
    updateIssue: async (id, updates) => {
      const current = rows.find((row) => row.id === id);
      if (!current) throw new Error("Unknown fixture issue");
      const next = {
        ...current,
        ...updates,
        revision: (current.revision ?? 0) + 1,
      };
      rows = rows.map((row) => (row.id === id ? next : row));
      return structuredClone(localizeIssue(next));
    },
    listChildIssues: async () => ({ issues: [] }),
    listChildrenByParents: async () => ({ issues: [] }),
    getChildIssueProgress: async () => ({ progress: [] }),
    listComments: async (id) =>
      comments
        .filter((comment) => comment.issue_id === id)
        .map(localizeComment),
    listTimeline: async (id) =>
      comments
        .filter((comment) => comment.issue_id === id)
        .map((comment) => ({
          ...localizeComment(comment),
          type: "comment",
          actor_type: comment.author_type,
          actor_id: comment.author_id,
          actor_name: user.name,
        })),
    previewCommentTriggers: async () => ({ agents: [] }),
    createComment: async (issueId, content, _type, parentId) => {
      const comment: Comment = {
        id: crypto.randomUUID(),
        issue_id: issueId,
        author_type: "member",
        author_id: user.id,
        content,
        type: "comment",
        parent_id: parentId ?? null,
        reactions: [],
        attachments: [],
        created_at: time,
        updated_at: time,
        revision: 1,
        resolved_at: null,
        resolved_by_type: null,
        resolved_by_id: null,
      };
      comments.push(comment);
      return comment;
    },
    listTasksByIssue: async () => [],
    getActiveTasksForIssue: async () => ({ tasks: [] }),
    getAgentTaskSnapshot: async () => [],
    getWorkspaceWorkingAgents: async () => [],
    listGitHubInstallations: async () => ({
      installations: [],
      configured: false,
    }),
    listIssuePullRequests: async () => ({ pull_requests: [], auto_complete: null }),
    listAttachments: async () => [],
    listIssueSubscribers: async () => [],
    getAssigneeFrequency: async () => [],
    listLabelsForIssue: async () => ({ labels: [] }),
    listLabels: async () => ({ labels: [], total: 0 }),
    listProperties: async () => ({ properties: [], total: 0 }),
    listQuickActions: async () => ({ quick_actions: [] }),
    listIssueStatuses: async () => ({
      statuses: STATUS_ORDER.map((key, index) => ({
        id: key,
        workspace_id: workspace.id,
        key,
        name: key,
        description: "",
        category: key,
        color: "#777777",
        is_system: true,
        position: index,
        archived_at: null,
        created_at: time,
        updated_at: time,
      })),
      categories: STATUS_ORDER,
      total: STATUS_ORDER.length,
    }),
    listPins: async () => [],
    listMyInvitations: async () => [],
    getInboxUnreadSummary: async () => [],
    getUnreadInboxCount: async () => ({ count: 0 }),
    listChatSessions: async () => [],
    listIssueViews: async () => [],
    getIssueViewPreference: async (params) => ({
      ...params,
      prefs: { hidden: [], order: [] },
      updated_at: time,
    }),
    listPluginInstallations: async () => ({ plugins: [] }),
    getWorkspaceSubscriptionSummary: async () => null,
    getIssueLimitUsage: async () => null,
  };
  return new Proxy(new ApiClient("/ui-lab-fixtures"), {
    get(_target, property: string) {
      const handler = handlers[property as keyof ApiClient];
      if (handler !== undefined) return handler;
      if (property === "then") return undefined;
      return () => {
        const message = `UI Lab has no fixture for this operation: ${property}`;
        console.warn(message);
        return Promise.reject(new Error(message));
      };
    },
  });
}
