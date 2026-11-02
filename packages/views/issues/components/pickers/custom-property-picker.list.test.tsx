import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "@multica/core/api";
import { I18nProvider } from "@multica/core/i18n/react";
import type { Issue, IssueProperty } from "@multica/core/types";
import enIssues from "../../../locales/en/issues.json";
import { CustomPropertyValueEditor, CustomPropertyValueInput } from "./custom-property-picker";

vi.mock("@multica/core/api", () => ({
  api: { setIssueProperty: vi.fn(), unsetIssueProperty: vi.fn() },
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

const property: IssueProperty = {
  id: "p-1", workspace_id: "ws-1", name: "Related links", type: "multi_url",
  config: {}, position: 1, archived: false,
  created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z",
};
const issue: Issue = {
  id: "issue-1", workspace_id: "ws-1", number: 1, identifier: "MUL-1",
  title: "Related links", description: null, status: "todo", priority: "none",
  assignee_type: null, assignee_id: null, creator_type: "member", creator_id: "member-1",
  parent_issue_id: null, project_id: null, position: 1, stage: null,
  start_date: null, due_date: null, labels: [], metadata: {},
  properties: { [property.id]: ["https://existing.example"] },
  created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z",
};

function renderEditor() {
  const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  const editor = (currentIssue: Issue, currentProperty = property) => (
    <QueryClientProvider client={client}>
      <I18nProvider locale="en" resources={{ en: { issues: enIssues } }}>
        <CustomPropertyValueEditor issue={currentIssue} property={currentProperty} defaultOpen />
      </I18nProvider>
    </QueryClientProvider>
  );
  const view = render(editor(issue));
  return {
    ...view,
    showIssue: (next: Issue, nextProperty = property) => view.rerender(editor(next, nextProperty)),
  };
}

describe("list property editing", () => {
  beforeEach(() => vi.resetAllMocks());

  it("explains a missing URL scheme and keeps the draft editable", async () => {
    const user = userEvent.setup();
    renderEditor();
    const input = await screen.findByPlaceholderText("https://…");
    await user.type(input, "example.com{Enter}");

    expect(await screen.findByRole("alert")).toHaveTextContent("http:// or https://");
    expect(input).toHaveValue("example.com");
    expect(input).toHaveAttribute("aria-invalid", "true");
    expect(api.setIssueProperty).not.toHaveBeenCalled();

    await user.clear(input);
    await user.type(input, "https://example.com");
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("retains a server-rejected URL and clears it only after a successful retry", async () => {
    const user = userEvent.setup();
    vi.mocked(api.setIssueProperty).mockRejectedValueOnce(new Error("URL must have a host"));
    renderEditor();
    const input = await screen.findByPlaceholderText("https://…");
    await user.type(input, "https://{Enter}");

    expect(await screen.findByRole("alert")).toHaveTextContent("URL must have a host");
    expect(input).toHaveValue("https://");
    expect(api.setIssueProperty).toHaveBeenCalledWith(issue.id, property.id, [
      "https://existing.example", "https://",
    ]);

    vi.mocked(api.setIssueProperty).mockResolvedValueOnce({
      properties: { [property.id]: ["https://existing.example", "https://example.com"] },
      issue_revision: 2,
    });
    await user.type(input, "example.com{Enter}");
    await waitFor(() => expect(input).toHaveValue(""));
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("keeps the draft but clears the old error when reopened", async () => {
    const user = userEvent.setup();
    renderEditor();
    await user.type(await screen.findByRole("textbox", { name: property.name }), "example.com{Enter}");
    expect(await screen.findByRole("alert")).toBeInTheDocument();

    await user.keyboard("{Escape}");
    await user.click(screen.getByRole("button", { name: "https://existing.example" }));

    const input = await screen.findByRole("textbox", { name: property.name });
    expect(input).toHaveValue("example.com");
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(input).toHaveAttribute("aria-invalid", "false");
    expect(input).not.toHaveAttribute("aria-describedby");
  });

  it.each(["issue", "property"])("resets the draft and error when switching %s in the same editor", async (target) => {
    const user = userEvent.setup();
    const { showIssue } = renderEditor();
    await user.type(await screen.findByRole("textbox", { name: property.name }), "example.com{Enter}");
    expect(await screen.findByRole("alert")).toBeInTheDocument();

    const nextProperty = target === "property" ? { ...property, id: "p-2" } : property;
    showIssue({
      ...issue,
      id: target === "issue" ? "issue-2" : issue.id,
      properties: { [nextProperty.id]: ["https://next.example"] },
    }, nextProperty);

    expect(await screen.findByRole("textbox", { name: property.name })).toHaveValue("");
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Remove https://next.example" })).toBeEnabled();
  });

  it("does not carry an in-flight failure into another issue's draft", async () => {
    const user = userEvent.setup();
    let fail!: (error: Error) => void;
    vi.mocked(api.setIssueProperty).mockImplementationOnce(() => new Promise((_resolve, reject) => {
      fail = reject;
    }));
    const { showIssue } = renderEditor();
    await user.type(await screen.findByRole("textbox", { name: property.name }), "https://old.example{Enter}");
    await waitFor(() => expect(api.setIssueProperty).toHaveBeenCalledTimes(1));

    showIssue({ ...issue, id: "issue-2" });
    const input = await screen.findByRole("textbox", { name: property.name });
    expect(input).toHaveValue("");
    expect(input).not.toHaveAttribute("readonly");
    await user.type(input, "https://new.example");

    await act(async () => fail(new Error("Previous issue write failed")));
    expect(input).toHaveValue("https://new.example");
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("keeps the draft and prevents overlapping add/remove requests while saving", async () => {
    const user = userEvent.setup();
    let finish!: () => void;
    vi.mocked(api.setIssueProperty).mockImplementationOnce(() => new Promise((resolve) => {
      finish = () => resolve({ properties: {}, issue_revision: 2 });
    }));
    renderEditor();
    const input = await screen.findByPlaceholderText("https://…");
    await user.type(input, "https://example.com{Enter}");
    await waitFor(() => expect(api.setIssueProperty).toHaveBeenCalledTimes(1));

    expect(input).toHaveValue("https://example.com");
    expect(screen.getByRole("button", { name: "Remove https://existing.example" })).toBeDisabled();
    fireEvent.submit(input.closest("form")!);
    expect(api.setIssueProperty).toHaveBeenCalledTimes(1);

    await act(async () => finish());
    await waitFor(() => expect(input).toHaveValue(""));
  });

  it("still appends and clears a synchronous issue-creation draft", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <I18nProvider locale="en" resources={{ en: { issues: enIssues } }}>
        <CustomPropertyValueInput
          property={{ ...property, type: "multi_text" }} value={["alpha"]}
          onChange={onChange} defaultOpen
        />
      </I18nProvider>,
    );
    const input = await screen.findByPlaceholderText("Enter value…");
    await user.type(input, "Smith, John{Enter}");
    expect(onChange).toHaveBeenCalledWith(["alpha", "Smith, John"]);
    await waitFor(() => expect(input).toHaveValue(""));
  });
});
