import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import enIssues from "../../locales/en/issues.json";
import { IssueProjectScopePicker } from "./issue-project-scope-picker";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/api", () => ({ api: { listProjects: async () => ({projects: [
  { id: "p1", title: "Product Design", icon: null, color: null },
  { id: "p2", title: "Engineering", icon: null, color: null },
]}) } }));

describe("project scope selector", () => {
  it("selects a searched project by keyboard and can return to Workspace", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const renderPicker = (id: string | null) => <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={{ en: { issues: enIssues } }}>
        <IssueProjectScopePicker projectId={id} onChange={onChange} />
      </I18nProvider>
    </QueryClientProvider>;
    const view = render(renderPicker(null));
    const trigger = screen.getByRole("button", { name: enIssues.project_scope.label });
    expect(trigger).toHaveTextContent("Workspace");
    await user.click(trigger);
    await user.type(await screen.findByPlaceholderText(enIssues.project_scope.search), "engineering");
    await user.keyboard("{Enter}");
    expect(onChange).toHaveBeenLastCalledWith("p2");
    view.rerender(renderPicker("p2"));
    expect(trigger).toHaveTextContent("Engineering");
    await user.click(trigger);
    await user.click(await screen.findByRole("button", { name: "Workspace" }));
    expect(onChange).toHaveBeenLastCalledWith(null);
    qc.clear();
  });
});
