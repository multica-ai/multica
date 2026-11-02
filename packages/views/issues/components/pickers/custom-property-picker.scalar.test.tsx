import { useState } from "react";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider, useQuery } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { I18nProvider } from "@multica/core/i18n/react";
import { issueDetailOptions, issueKeys } from "@multica/core/issues/queries";
import { onIssuePropertiesChanged } from "@multica/core/issues/ws-updaters";
import type { Issue, IssueProperty, IssuePropertyValue } from "@multica/core/types";
import enIssues from "../../../locales/en/issues.json";
import { CustomPropertyValueEditor, CustomPropertyValueInput } from "./custom-property-picker";

vi.mock("@multica/core/api", () => ({
  api: { getIssue: vi.fn(), setIssueProperty: vi.fn(), unsetIssueProperty: vi.fn() },
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("sonner", () => ({ toast: { error: vi.fn() } }));

const property: IssueProperty = {
  id: "p-1", workspace_id: "ws-1", name: "Reference", type: "text",
  config: {}, position: 1, archived: false,
  created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z",
};
const issue: Issue = {
  id: "issue-1", workspace_id: "ws-1", number: 1, identifier: "MUL-1",
  title: "Scalar editing", description: null, status: "todo", priority: "none",
  assignee_type: null, assignee_id: null, creator_type: "member", creator_id: "member-1",
  parent_issue_id: null, project_id: null, position: 1, stage: null,
  start_date: null, due_date: null, labels: [], metadata: {}, revision: 1,
  properties: { [property.id]: "original" },
  created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z",
};

function Locale({ children }: { children: React.ReactNode }) {
  return <I18nProvider locale="en" resources={{ en: { issues: enIssues } }}>{children}</I18nProvider>;
}

function renderInput(type: IssueProperty["type"] = "text", initial: IssuePropertyValue | undefined = "original") {
  const onChange = vi.fn();
  const tree = (value: IssuePropertyValue | undefined) => (
    <Locale>
      <CustomPropertyValueInput property={{ ...property, type }} value={value} onChange={onChange} defaultOpen trigger="Edit" />
      <button>Outside</button>
    </Locale>
  );
  const view = render(tree(initial));
  return { onChange, showValue: (value: IssuePropertyValue | undefined) => view.rerender(tree(value)) };
}

function renderQueryEditor(currentProperty = property, initial = issue) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity }, mutations: { retry: false } } });
  const key = issueKeys.detail("ws-1", initial.id);
  client.setQueryData(key, initial);
  vi.mocked(api.getIssue).mockResolvedValue(initial);
  function Editor() {
    const { data } = useQuery(issueDetailOptions("ws-1", initial.id));
    return data && <CustomPropertyValueEditor issue={data} property={currentProperty} defaultOpen />;
  }
  render(<QueryClientProvider client={client}><Locale><Editor /></Locale></QueryClientProvider>);
  return { client, key };
}

beforeEach(() => vi.resetAllMocks());

describe("scalar property editing sessions", () => {
  it.each([
    ["text", "original", "  local draft  ", "remote"],
    ["number", 12, "34.50", 99],
    ["url", "https://old.example", "  https://draft.example  ", "https://remote.example"],
    ["text", "original", "  local draft  ", undefined],
    ["number", 12, "34.50", undefined],
    ["url", "https://old.example", "  https://draft.example  ", undefined],
  ] as const)("keeps dirty %s input from %s to %s when remote becomes %s and refetches", async (type, initial, draft, remote) => {
    const currentProperty = { ...property, type };
    const { client, key } = renderQueryEditor(currentProperty, { ...issue, properties: { [property.id]: initial } });
    const input = await screen.findByRole(type === "number" ? "spinbutton" : "textbox");
    fireEvent.change(input, { target: { value: draft } });

    // Exercise the same production handler used by issue_properties:changed.
    const properties = remote === undefined ? {} : { [property.id]: remote };
    const triggerName = remote === undefined ? "Empty" : String(remote);
    vi.mocked(api.getIssue).mockResolvedValue({ ...issue, revision: 2, properties });
    await act(async () => onIssuePropertiesChanged(client, "ws-1", issue.id, properties, 2));
    expect(client.getQueryData<Issue>(key)).toMatchObject({ properties, revision: 2 });
    await waitFor(() => expect(screen.getByRole("button", { name: triggerName })).toBeInTheDocument());
    expect(input).toHaveDisplayValue(draft);

    await act(async () => onIssuePropertiesChanged(client, "ws-1", issue.id, properties, 2));
    expect(client.getQueryData<Issue>(key)?.properties[property.id]).toBe(remote);
    expect(input).toHaveDisplayValue(draft);

    vi.mocked(api.getIssue).mockResolvedValue({ ...issue, revision: 3, properties: {} });
    await act(async () => onIssuePropertiesChanged(client, "ws-1", issue.id, {}, 3));
    expect(client.getQueryData<Issue>(key)).toMatchObject({ properties: {}, revision: 3 });
    await waitFor(() => expect(screen.getByRole("button", { name: "Empty" })).toBeInTheDocument());
    expect(input).toHaveDisplayValue(draft);

    vi.mocked(api.getIssue).mockResolvedValue({ ...issue, revision: 4, properties });
    await act(async () => { await client.refetchQueries({ queryKey: key }); });
    expect(api.getIssue).toHaveBeenCalledWith(issue.id);
    expect(client.getQueryData<Issue>(key)).toMatchObject({ properties, revision: 4 });
    await waitFor(() => expect(screen.getByRole("button", { name: triggerName })).toBeInTheDocument());
    expect(input).toHaveDisplayValue(draft);
    expect(api.setIssueProperty).not.toHaveBeenCalled();
    expect(api.unsetIssueProperty).not.toHaveBeenCalled();
    client.clear();
  });

  it.each([
    ["text", "original", "remote"],
    ["number", 12, 99],
    ["url", "https://old.example", "https://remote.example"],
  ] as const)("continues syncing a clean open %s editor", async (type, initial, remote) => {
    const { showValue, onChange } = renderInput(type, initial);
    const input = await screen.findByRole(type === "number" ? "spinbutton" : "textbox");
    expect(input).toHaveDisplayValue(String(initial));
    showValue(remote);
    expect(input).toHaveDisplayValue(String(remote));
    showValue(undefined);
    expect(input).toHaveDisplayValue("");
    showValue(initial);
    expect(input).toHaveDisplayValue(String(initial));
    expect(onChange).not.toHaveBeenCalled();
  });

  it("keeps a clean editor live and becomes clean again when reverted to its adopted baseline", async () => {
    const { showValue, onChange } = renderInput();
    const input = await screen.findByRole("textbox");
    showValue("adopted");
    expect(input).toHaveValue("adopted");
    fireEvent.change(input, { target: { value: "local" } });
    showValue("latest");
    expect(input).toHaveValue("local");
    fireEvent.change(input, { target: { value: "adopted" } });
    await waitFor(() => expect(input).toHaveValue("latest"));
    showValue(undefined);
    expect(input).toHaveValue("");
    expect(onChange).not.toHaveBeenCalled();
  });

  it("treats editing whitespace as dirty and does not equate a remote value with the adopted baseline", async () => {
    const { showValue } = renderInput();
    const input = await screen.findByRole("textbox");
    fireEvent.change(input, { target: { value: " original " } });
    showValue(" original ");
    showValue("another remote value");
    expect(input).toHaveValue(" original ");
  });

  it("keeps a URL draft on internal blur and opens the latest saved URL", async () => {
    const user = userEvent.setup();
    const open = vi.spyOn(window, "open").mockImplementation(() => null);
    const { showValue, onChange } = renderInput("url", "https://old.example");
    const input = await screen.findByRole("textbox");
    await user.clear(input);
    await user.type(input, "https://draft.example");
    await user.tab();
    expect(screen.getByRole("button", { name: "Open link" })).toHaveFocus();
    showValue("https://latest.example");
    await user.click(screen.getByRole("button", { name: "Open link" }));
    expect(open).toHaveBeenCalledWith("https://latest.example", "_blank", "noopener,noreferrer");
    expect(input).toHaveValue("https://draft.example");
    expect(onChange).not.toHaveBeenCalled();
    open.mockRestore();
  });

  it.each(["Escape", "outside"])("discards unsubmitted edits on %s and reopens with the latest value", async (dismiss) => {
    const user = userEvent.setup();
    const { showValue, onChange } = renderInput();
    fireEvent.change(await screen.findByRole("textbox"), { target: { value: "local" } });
    showValue("remote");
    if (dismiss === "Escape") await user.keyboard("{Escape}");
    else await user.click(screen.getByRole("button", { name: "Outside" }));
    await waitFor(() => expect(screen.queryByRole("textbox")).not.toBeInTheDocument());
    showValue("latest");
    await user.click(screen.getByRole("button", { name: "Edit" }));
    expect(await screen.findByRole("textbox")).toHaveValue("latest");
    expect(onChange).not.toHaveBeenCalled();
  });

  it("resets dirty state on a controlled close and synchronizes again after reopening", async () => {
    const onChange = vi.fn();
    function Controlled() {
      const [open, setOpen] = useState(true);
      const [value, setValue] = useState("original");
      return <Locale>
        <CustomPropertyValueInput property={property} value={value} open={open} onOpenChange={setOpen} onChange={onChange} />
        <button onClick={() => { setOpen(false); setValue("remote"); }}>Close</button>
      </Locale>;
    }
    render(<Controlled />);
    fireEvent.change(await screen.findByRole("textbox"), { target: { value: "local" } });
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    await waitFor(() => expect(screen.queryByRole("textbox")).not.toBeInTheDocument());
    await userEvent.click(screen.getByRole("button", { name: "remote" }));
    expect(await screen.findByRole("textbox")).toHaveValue("remote");
    expect(onChange).not.toHaveBeenCalled();
  });

  it("initializes an initially closed editor from the latest value when opened", async () => {
    const onChange = vi.fn();
    const tree = (value: string) => <Locale>
      <CustomPropertyValueInput property={property} value={value} onChange={onChange} />
    </Locale>;
    const view = render(tree("original"));
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
    view.rerender(tree("latest"));
    await userEvent.click(screen.getByRole("button", { name: "latest" }));
    expect(await screen.findByRole("textbox")).toHaveValue("latest");
    expect(onChange).not.toHaveBeenCalled();
  });

  it.each(["issue", "property"])("isolates a dirty session when switching %s", async (target) => {
    const client = new QueryClient();
    const tree = (currentIssue: Issue, currentProperty = property) => <QueryClientProvider client={client}><Locale>
      <CustomPropertyValueEditor issue={currentIssue} property={currentProperty} defaultOpen />
    </Locale></QueryClientProvider>;
    const view = render(tree(issue));
    fireEvent.change(await screen.findByRole("textbox"), { target: { value: "local" } });
    const nextProperty = target === "property" ? { ...property, id: "p-2" } : property;
    view.rerender(tree({ ...issue, id: target === "issue" ? "issue-2" : issue.id, properties: { [nextProperty.id]: "next" } }, nextProperty));
    expect(await screen.findByRole("textbox")).toHaveValue("next");
    client.clear();
  });

  it("isolates property drafts in the mutation-free create-flow input", async () => {
    const onChange = vi.fn();
    const tree = (id: string) => <Locale>
      <CustomPropertyValueInput property={{ ...property, id }} value="same saved value" onChange={onChange} defaultOpen />
    </Locale>;
    const view = render(tree("p-1"));
    fireEvent.change(await screen.findByRole("textbox"), { target: { value: "local" } });
    view.rerender(tree("p-2"));
    expect(await screen.findByRole("textbox")).toHaveValue("same saved value");
    expect(onChange).not.toHaveBeenCalled();
  });

  it.each([
    ["text", "  local  ", "local"],
    ["number", "34.5", 34.5],
    ["url", "  https://draft.example  ", "https://draft.example"],
  ] as const)("commits %s with the existing form conversion and closes", async (type, draft, expected) => {
    const { showValue, onChange } = renderInput(type, type === "number" ? 12 : "original");
    const input = await screen.findByRole(type === "number" ? "spinbutton" : "textbox");
    fireEvent.change(input, { target: { value: draft } });
    showValue(type === "number" ? 99 : "remote");
    // user-event does not emulate implicit Enter submission for number inputs.
    if (type === "number") fireEvent.submit(input.closest("form")!);
    else await userEvent.type(input, "{Enter}");
    expect(onChange).toHaveBeenCalledExactlyOnceWith(expected);
    await waitFor(() => expect(input).not.toBeInTheDocument());
  });

  it.each(["remote", undefined])("uses the current saved value %s to decide whether an empty Enter clears", async (remote) => {
    const { showValue, onChange } = renderInput();
    const input = await screen.findByRole("textbox");
    fireEvent.change(input, { target: { value: "   " } });
    showValue(remote);
    await userEvent.type(input, "{Enter}");
    if (remote === undefined) expect(onChange).not.toHaveBeenCalled();
    else expect(onChange).toHaveBeenCalledExactlyOnceWith(undefined);
    await waitFor(() => expect(input).not.toBeInTheDocument());
  });

  it("preserves scalar mutation failure closing, toast and rollback", async () => {
    vi.mocked(api.setIssueProperty).mockRejectedValue(new Error("Write failed"));
    const { client, key } = renderQueryEditor();
    const input = await screen.findByRole("textbox");
    fireEvent.change(input, { target: { value: "local" } });
    await userEvent.type(input, "{Enter}");
    await waitFor(() => expect(input).not.toBeInTheDocument());
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("Write failed"));
    expect(client.getQueryData<Issue>(key)?.properties[property.id]).toBe("original");
    client.clear();
  });

  it("keeps archived scalar properties read-only", async () => {
    const { client } = renderQueryEditor({ ...property, archived: true });
    expect(await screen.findByRole("button", { name: "No value" })).toBeInTheDocument();
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
    expect(api.setIssueProperty).not.toHaveBeenCalled();
    client.clear();
  });
});
