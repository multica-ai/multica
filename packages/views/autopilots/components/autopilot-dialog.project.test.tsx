// @vitest-environment jsdom

import { useImperativeHandle, useRef, useState } from "react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@multica/core/api";
import type { AutopilotExecutionMode } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// Regression cover for MUL-6681 (GH #7550): the dialog rendered the Project
// section only for create_issue autopilots and sent `project_id: null` for
// run_only ones. A run_only autopilot could therefore never be bound to a
// project from the UI, and editing anything else on one — a title, a cron —
// silently cleared a binding made via the CLI. That binding is not cosmetic:
// it is the only project context a run_only task has, so losing it drops the
// run out of its project's worktree and into a bare workdir.

const mockCreateAutopilot = vi.hoisted(() => vi.fn());
const mockUpdateAutopilot = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-test" }));
vi.mock("@multica/core/paths", () => ({ useCurrentWorkspace: () => ({ name: "Acme" }) }));

vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: (wsId: string) => ({
    queryKey: ["agents", wsId],
    queryFn: async () => [
      {
        id: "agent-1",
        name: "Scout",
        description: "Researches things",
        archived_at: null,
        runtime_id: "runtime-1",
      },
    ],
  }),
  squadListOptions: (wsId: string) => ({
    queryKey: ["squads", wsId],
    queryFn: async () => [],
  }),
}));

vi.mock("@multica/core/projects/queries", () => ({
  projectListOptions: (wsId: string) => ({
    queryKey: ["projects", wsId],
    queryFn: async () => [{ id: "proj-1", title: "Fleet", icon: null }],
  }),
}));

vi.mock("@multica/core/autopilots/queries", () => ({
  autopilotKeys: {
    detail: (wsId: string, id: string) => ["autopilots", wsId, "detail", id],
  },
  cronPreviewOptions: (wsId: string, expr: string, tz: string) => ({
    queryKey: ["cron-preview", wsId, expr, tz],
    queryFn: async () => ({ next_runs: ["2126-07-14T01:00:00Z"] }),
    retry: false,
  }),
}));

vi.mock("@multica/core/autopilots/mutations", () => ({
  useCreateAutopilot: () => ({ mutateAsync: mockCreateAutopilot }),
  useCreateAutopilotTrigger: () => ({ mutateAsync: vi.fn().mockResolvedValue({ id: "trg-new" }) }),
  useUpdateAutopilot: () => ({ mutateAsync: mockUpdateAutopilot }),
  useUpdateAutopilotTrigger: () => ({ mutateAsync: vi.fn() }),
}));

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

vi.mock("../../editor", () => ({
  TitleEditor: ({ ref, defaultValue, placeholder, onChange, onSubmit }: any) => {
    const [value, setValue] = useState(defaultValue ?? "");
    const inputRef = useRef<HTMLInputElement>(null);
    useImperativeHandle(ref, () => ({
      getText: () => value,
      focus: () => inputRef.current?.focus(),
      focusAtCoords: () => inputRef.current?.focus(),
    }));
    return (
      <input
        ref={inputRef}
        aria-label="title"
        value={value}
        placeholder={placeholder}
        onChange={(e) => {
          setValue(e.target.value);
          onChange?.(e.target.value);
        }}
        onKeyDown={(e) => {
          if (e.key === "Enter") onSubmit?.();
        }}
      />
    );
  },
  ContentEditor: ({ defaultValue, placeholder, onUpdate }: any) => {
    const [value, setValue] = useState(defaultValue ?? "");
    return (
      <textarea
        aria-label="runbook"
        placeholder={placeholder}
        value={value}
        onChange={(event) => {
          setValue(event.target.value);
          onUpdate?.(event.target.value);
        }}
      />
    );
  },
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => <span data-testid="actor-avatar">{actorId}</span>,
}));

vi.mock("./subscriber-multi-select", () => ({
  SubscriberMultiSelect: () => <div data-testid="subscriber-multi-select" />,
}));

// Stand-in picker: renders the dialog's own trigger (so the selected project
// title is asserted against the real markup) plus a button that reports a
// selection, which is how the create-mode test binds a project.
vi.mock("../../projects/components/project-picker", () => ({
  ProjectPicker: ({
    triggerRender,
    onUpdate,
  }: {
    triggerRender: React.ReactElement;
    onUpdate: (updates: { project_id: string | null }) => void;
  }) => (
    <div>
      {triggerRender}
      <button type="button" onClick={() => onUpdate({ project_id: "proj-1" })}>
        pick Fleet
      </button>
    </div>
  ),
}));

vi.mock("./pickers/timezone-picker", () => ({
  TimezonePicker: ({ value }: { value: string }) => <div data-testid="timezone-picker">{value}</div>,
}));

import { AutopilotDialog } from "./autopilot-dialog";

const AUTOPILOT_ID = "ap-1";

function editDialogTree(
  mode: AutopilotExecutionMode,
  projectId: string | null,
  revision?: number,
  title = "Push fleet repo to GitHub",
  description = "",
) {
  return (
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <AutopilotDialog
        mode="edit"
        open
        onOpenChange={vi.fn()}
        autopilotId={AUTOPILOT_ID}
        initial={{
          title,
          description,
          revision,
          project_id: projectId,
          assignee_type: "agent",
          assignee_id: "agent-1",
          execution_mode: mode,
          subscriber_user_ids: [],
        }}
        triggers={[]}
        collaborators={[]}
        canManageAccess={false}
      />
    </QueryClientProvider>
  );
}

function renderEditDialog(
  mode: AutopilotExecutionMode,
  projectId: string | null,
  revision?: number,
  title?: string,
  description?: string,
) {
  return renderWithI18n(
    editDialogTree(mode, projectId, revision, title, description),
  );
}

function renderCreateDialog() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <AutopilotDialog
        mode="create"
        open
        onOpenChange={vi.fn()}
        initial={{
          assignee_type: "agent",
          assignee_id: "agent-1",
          execution_mode: "run_only",
        }}
      />
    </QueryClientProvider>,
  );
}

const saveButton = () => screen.getByRole("button", { name: "Save" });

describe("AutopilotDialog project section", () => {
  beforeEach(() => {
    mockCreateAutopilot.mockReset().mockResolvedValue({ id: AUTOPILOT_ID });
    mockUpdateAutopilot.mockReset().mockResolvedValue({ id: AUTOPILOT_ID });
  });

  afterEach(() => cleanup());

  it("offers the project picker to a run_only autopilot", async () => {
    renderEditDialog("run_only", "proj-1");

    expect(screen.getByText("Project")).toBeInTheDocument();
    // The bound project reads back instead of the empty-state label.
    expect(await screen.findByText("Fleet")).toBeInTheDocument();
    expect(screen.queryByText("No project")).not.toBeInTheDocument();
  });

  it("says the project also decides where runs execute", () => {
    renderEditDialog("run_only", null);

    expect(
      screen.getByText("Also sets where runs execute — the project's repository or local directory"),
    ).toBeInTheDocument();
  });

  it("keeps a run_only autopilot's project on save instead of clearing it", async () => {
    const user = userEvent.setup();
    renderEditDialog("run_only", "proj-1");

    await user.type(screen.getByLabelText("title"), " v2");
    await user.click(saveButton());

    await waitFor(() => expect(mockUpdateAutopilot).toHaveBeenCalledTimes(1));
    expect(mockUpdateAutopilot.mock.calls[0]?.[0]).toMatchObject({
      id: AUTOPILOT_ID,
      execution_mode: "run_only",
      project_id: "proj-1",
    });
  });

  it("keeps a create_issue autopilot's project on save", async () => {
    const user = userEvent.setup();
    renderEditDialog("create_issue", "proj-1");

    await user.click(saveButton());

    await waitFor(() => expect(mockUpdateAutopilot).toHaveBeenCalledTimes(1));
    expect(mockUpdateAutopilot.mock.calls[0]?.[0]).toMatchObject({ project_id: "proj-1" });
  });

  it("binds a project chosen while creating a run_only autopilot", async () => {
    const user = userEvent.setup();
    renderCreateDialog();

    await user.type(screen.getByLabelText("title"), "Push fleet repo to GitHub");
    await user.click(screen.getByRole("button", { name: "pick Fleet" }));
    await user.click(screen.getByRole("button", { name: "Create autopilot" }));

    await waitFor(() => expect(mockCreateAutopilot).toHaveBeenCalledTimes(1));
    expect(mockCreateAutopilot.mock.calls[0]?.[0]).toMatchObject({
      execution_mode: "run_only",
      project_id: "proj-1",
    });
  });

  it("includes the loaded revision for edit saves and omits it for creates", async () => {
    const user = userEvent.setup();
    renderEditDialog("run_only", "proj-1", 7);
    await user.type(screen.getByLabelText("title"), " v2");
    await user.click(saveButton());
    await waitFor(() => expect(mockUpdateAutopilot).toHaveBeenCalledTimes(1));
    expect(mockUpdateAutopilot.mock.calls[0]?.[0]).toMatchObject({
      id: AUTOPILOT_ID,
      expected_revision: 7,
    });

    mockUpdateAutopilot.mockReset().mockResolvedValue({ id: AUTOPILOT_ID });
    cleanup();
    renderCreateDialog();
    await user.type(screen.getByLabelText("title"), "New autopilot");
    await user.click(screen.getByRole("button", { name: "Create autopilot" }));
    await waitFor(() => expect(mockCreateAutopilot).toHaveBeenCalledTimes(1));
    expect(mockCreateAutopilot.mock.calls[0]?.[0]).not.toHaveProperty("expected_revision");
  });

  it("retains an edit draft and blocks the TitleEditor Enter retry until resolved", async () => {
    mockUpdateAutopilot
      .mockRejectedValueOnce(new ApiError("stale", 409, "Conflict", { code: "revision_conflict" }))
      .mockResolvedValueOnce({ id: AUTOPILOT_ID });
    const user = userEvent.setup();
    const view = renderEditDialog("run_only", "proj-1", 7, "Original title", "Original server text");
    await user.type(screen.getByLabelText("title"), " local");
    await user.clear(screen.getByLabelText("runbook"));
    await user.type(screen.getByLabelText("runbook"), "Local runbook draft");
    await user.click(saveButton());
    await waitFor(() => expect(screen.getByRole("alert").textContent).toContain("Loading the latest server version"));
    expect(mockUpdateAutopilot.mock.calls[0]?.[0]).toMatchObject({
      description: "Local runbook draft",
      expected_revision: 7,
    });

    view.rerender(editDialogTree("run_only", "proj-1", 8, "Server title", "Fresh server text"));
    expect(screen.getByRole("alert").textContent).toContain("Server version · revision 8");
    expect(screen.getByRole("alert").textContent).toContain("Fresh server text");
    expect((screen.getByRole("button", { name: "Save" }) as HTMLButtonElement).disabled).toBe(true);

    fireEvent.keyDown(screen.getByLabelText("title"), { key: "Enter" });
    expect(mockUpdateAutopilot).toHaveBeenCalledTimes(1);

    await user.click(screen.getByRole("button", { name: "Keep my draft" }));
    fireEvent.keyDown(screen.getByLabelText("title"), { key: "Enter" });
    await waitFor(() => expect(mockUpdateAutopilot).toHaveBeenCalledTimes(2));
    expect(mockUpdateAutopilot.mock.calls[1]?.[0]).toMatchObject({
      description: "Local runbook draft",
      expected_revision: 8,
    });
  });

  it("adopts the refreshed server version before an autopilot retry", async () => {
    mockUpdateAutopilot
      .mockRejectedValueOnce(new ApiError("stale", 409, "Conflict", { code: "revision_conflict" }))
      .mockResolvedValueOnce({ id: AUTOPILOT_ID });
    const user = userEvent.setup();
    const view = renderEditDialog("run_only", "proj-1", 7, "Original title", "Original server text");
    await user.clear(screen.getByLabelText("runbook"));
    await user.type(screen.getByLabelText("runbook"), "Local runbook draft");
    await user.click(saveButton());
    await waitFor(() => expect(screen.getByRole("alert").textContent).toContain("Loading the latest server version"));

    view.rerender(editDialogTree("run_only", "proj-1", 8, "Server title", "Fresh server text"));
    await user.click(screen.getByRole("button", { name: "Use server version" }));
    await user.click(saveButton());
    await waitFor(() => expect(mockUpdateAutopilot).toHaveBeenCalledTimes(2));
    expect(mockUpdateAutopilot.mock.calls[1]?.[0]).toMatchObject({
      description: "Fresh server text",
      expected_revision: 8,
    });
  });
});
