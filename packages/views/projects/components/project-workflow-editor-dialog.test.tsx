import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { vi, it, expect } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import { effectiveIssueWorkflowOptions } from "@multica/core/issue-workflows";
import type { IssueWorkflowResponse } from "@multica/core/types";
import { ProjectWorkflowEditorDialog } from "./project-workflow-editor-dialog";
import en from "../../locales/en/projects.json";

const apply = vi.hoisted(() => vi.fn());
vi.mock("@multica/core/api", () => ({ api: { applyProjectWorkflow: apply } }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
vi.mock("./workflow-editor", () => ({ WorkflowEditor: () => <div>Workflow editor</div> }));

it("previews existing issues, requires missing mappings, and confirms stable keys rather than temporary node IDs", async () => {
  const user = userEvent.setup();
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  const definition: IssueWorkflowResponse = {
    workflow: { id: "flow", workspace_id: "ws", scope_type: "project", scope_id: "project", name: "Engineering", revision: 3, initial_status_id: "todo", created_at: "", updated_at: "" },
    mode: "custom",
    statuses: [{ id: "todo", workflow_id: "flow", legacy_status_key: "todo", spec_key: "todo", name: "Todo", description: "", color: "#123456", position: 0, phase: "unstarted", outcome: null, entry_policy: { executor: { type: "none" }, instructions: "" }, entry_policy_revision: 1, archived_at: null, created_at: "", updated_at: "" }],
  };
  client.setQueryData(effectiveIssueWorkflowOptions("ws", "project", true).queryKey, definition);
  apply.mockResolvedValueOnce({
    ...definition, dry_run: true,
    statuses: [{ ...definition.statuses[0], id: "rolled-back-node" }],
    plan: { migration: { fingerprint: "reviewed-snapshot", issue_count: 2, view_count: 1, blocked_issue_ids: [], rows: [{ source_status_id: "old", source_name: "Old status", source_phase: "started", count: 2, required: true, target_key: "", phase_changed: false }] } },
  }).mockResolvedValueOnce(definition);
  const onClose = vi.fn();
  render(<I18nProvider locale="en" resources={{ en: { projects: en } }}><QueryClientProvider client={client}><ProjectWorkflowEditorDialog projectId="project" definition={definition} onClose={onClose} /></QueryClientProvider></I18nProvider>);
  await user.click(screen.getByRole("button", { name: en.workflow.save }));
  expect(await screen.findByText("2 issues and 1 saved views will be updated.")).toBeInTheDocument();
  expect(onClose).not.toHaveBeenCalled();
  const confirm = screen.getByRole("button", { name: en.workflow.confirm_migration });
  expect(confirm).toBeDisabled();
  await user.selectOptions(screen.getByRole("combobox", { name: "Old status · 2" }), "todo");
  expect(screen.getByRole("alert")).toHaveTextContent(en.workflow.phase_warning);
  await user.click(confirm);
  await waitFor(() => expect(onClose).toHaveBeenCalledOnce());
  expect(apply.mock.calls[0]?.[1]).toMatchObject({ dry_run: true, expected_revision: 3 });
  expect(apply.mock.calls[1]?.[1]).toMatchObject({ dry_run: false, confirm_migration: true, migration_fingerprint: "reviewed-snapshot", status_mapping: { old: "todo" } });
  client.clear();
});
