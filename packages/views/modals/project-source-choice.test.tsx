import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import { cachedProjectSourceChoice } from "@multica/core/projects/source-choice";
import { projectKeys } from "@multica/core/projects/queries";
import { issueKeys } from "@multica/core/issues/queries";
import { issueWorkflowKeys } from "@multica/core/issue-workflows/queries";
import type { Issue, Project } from "@multica/core/types";
import { NavigationProvider } from "../navigation/context";
import en from "../locales/en";
import { ProjectSourceChoiceModal } from "./project-source-choice";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace" }));
afterEach(cleanup);
const project = { id: "project", workspace_id: "workspace", title: "Undecided", resource_count: 0, source_intent: "unspecified", workflow_id: null } as Project;
const original = { id: "issue", workspace_id: "workspace", project_id: project.id, status: "todo", assignee_type: "agent", assignee_id: "agent" } as Issue;
const navigation = { pathname: "/test/issues", searchParams: new URLSearchParams(), hash: "", push: vi.fn(), replace: vi.fn(), back: vi.fn(), getShareableUrl: (path: string) => path };
function host(issue: Issue = original) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  qc.setQueryData(projectKeys.detail("workspace", project.id), project);
  qc.setQueryData(issueKeys.detail("workspace", issue.id), issue);
  render(<QueryClientProvider client={qc}><WorkspaceSlugProvider slug="test"><I18nProvider locale="en" resources={{en}}><NavigationProvider value={navigation}><ProjectSourceChoiceModal /></NavigationProvider></I18nProvider></WorkspaceSlugProvider></QueryClientProvider>);
  return qc;
}
function choice(updates: object, previous: Issue = original) {
  return cachedProjectSourceChoice("/api/issues/issue", updates, {}, { workspaceId: "workspace", previousIssues: { issue: previous } });
}
describe("source admission before optimistic writes", () => {
  it("lets ordinary active status changes through even when cache already contains the new status", () => {
    host({ ...original, status: "in_progress" });
    expect(choice({ status: "in_progress" })).toBeUndefined();
  });
  it.each(["todo", "done", "cancelled"])("does not start runs by leaving %s for active", status => {
    host();
    expect(choice({ status: "in_progress" }, { ...original, status })).toBeUndefined();
  });
  it("retains backlog admission using the pre-optimistic status", () => {
    host({ ...original, status: "in_progress" });
    expect(choice({ status: "in_progress" }, { ...original, status: "backlog" })).toEqual(project);
  });
  it("distinguishes unchanged, removed, member and newly assigned agents", () => {
    host();
    expect(choice({ assignee_type: "agent", assignee_id: "agent" })).toBeUndefined();
    expect(choice({ assignee_type: null, assignee_id: null })).toBeUndefined();
    expect(choice({ assignee_type: "member", assignee_id: "member" })).toBeUndefined();
    expect(choice({ assignee_type: "agent", assignee_id: "other" })).toEqual(project);
    expect(choice({ status: "in_progress" }, { ...original, assignee_type: null, assignee_id: null })).toBeUndefined();
  });
  it("keeps no-run suppression and parking controls", () => {
    host();
    expect(choice({ assignee_id: "other", suppress_run: true })).toBeUndefined();
    expect(choice({ assignee_id: "other", status: "backlog" })).toBeUndefined();
  });
  it("admits a workflow status handoff but preserves administrative project moves and explicit assignees", () => {
    const qc = host();
    qc.setQueryData(issueWorkflowKeys.list("workspace"), { workflows: [{ id: "workflow", is_default: true, steps: [{status_key: "in_progress", handler: {type: "agent", id: "handler"}}] }] });
    expect(choice({ status: "in_progress" })).toEqual(project);
    const previous = { ...original, project_id: "previous", status: "in_progress" };
    expect(choice({ project_id: project.id }, previous)).toBeUndefined();
    expect(choice({ project_id: project.id, enter_workflow_step: true }, previous)).toEqual(project);
    expect(choice({ status: "in_progress", assignee_type: "member", assignee_id: "member" })).toBeUndefined();
    expect(choice({ status: "todo", enter_workflow_step: true })).toBeUndefined();
  });
  it("preserves batch, direct cached assignment and creates without sending context in a payload", () => {
    host();
    expect(cachedProjectSourceChoice("/api/issues/batch-update", { issue_ids: [original.id], updates: { assignee_id: "other" } }, {})).toEqual(project);
    expect(cachedProjectSourceChoice("/api/issues/issue/move", { assignee_id: "other" }, {})).toEqual(project);
    expect(cachedProjectSourceChoice("/api/issues", { project_id: project.id, assignee_type: "agent", assignee_id: "agent" }, {})).toEqual(project);
    expect(cachedProjectSourceChoice("/api/issues/issue", { assignee_id: "other" }, {}, {workspaceId:"other",previousIssues:{issue:original}})).toBeUndefined();
  });
  it("uses the normal write for missing prior state and respects per-write consent", () => {
    host();
    expect(cachedProjectSourceChoice("/api/issues/issue", {status:"in_progress"}, {}, {workspaceId:"workspace",previousIssues:{issue:undefined}})).toBeUndefined();
    expect(cachedProjectSourceChoice("/api/issues/issue", {assignee_id:"other"}, {[project.id]:"no_code_once"})).toBeUndefined();
  });
});
