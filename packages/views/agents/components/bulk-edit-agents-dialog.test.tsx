// @vitest-environment jsdom

import type { ComponentType, ComponentProps, ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import { setCurrentWorkspace } from "@multica/core/platform";
import { useAgentBulkEditPresetsStore } from "@multica/core/agents/stores";
import type { BulkUpdateAgentsRequest, MemberWithUser, RuntimeDevice } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enAgents from "../../locales/en/agents.json";

vi.mock("./model-dropdown", () => ({
  ModelDropdown: ({ value, onChange, disabled }: { value: string; onChange: (value: string) => void; disabled?: boolean }) => (
    <input
      aria-label="Model"
      disabled={disabled}
      value={value}
      onChange={(e) => onChange(e.currentTarget.value)}
    />
  ),
}));

vi.mock("../../runtimes/components/provider-logo", () => ({
  ProviderLogo: () => null,
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => null,
}));

import { BulkEditAgentsDialog } from "./bulk-edit-agents-dialog";

// These are interaction-heavy tests: each drives many sequential userEvent
// operations against a large dialog. On CI the views test task is co-scheduled
// with the webpack build on a 2-core runner, which starves the event loop;
// userEvent's default per-event setTimeout(0) yields then stall, inflating
// runtimes far past the 5s default. delay:null drops those per-event yields and
// the widened timeout covers the residual render cost under contention.
vi.setConfig({ testTimeout: 30000 });

const RES = { en: { common: enCommon, agents: enAgents } };
const ME = "user-me";

const members: MemberWithUser[] = [
  {
    id: "m-me",
    user_id: ME,
    workspace_id: "ws-1",
    role: "owner",
    name: "Me",
    email: "me@example.com",
    avatar_url: null,
    created_at: "2026-01-01T00:00:00Z",
  },
];

function makeRuntime(overrides: Partial<RuntimeDevice> = {}): RuntimeDevice {
  return {
    id: "rt-1",
    workspace_id: "ws-1",
    daemon_id: null,
    name: "Codex Runtime",
    runtime_mode: "local",
    provider: "codex",
    launch_header: "",
    status: "online",
    device_info: "host.local",
    metadata: {},
    owner_id: ME,
    visibility: "private",
    last_seen_at: "2026-04-27T11:59:50Z",
    created_at: "2026-04-01T00:00:00Z",
    updated_at: "2026-04-01T00:00:00Z",
    ...overrides,
  };
}

function Wrap({ children }: { children: ReactNode }) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return (
    <I18nProvider locale="en" resources={RES}>
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    </I18nProvider>
  );
}

beforeEach(() => {
  localStorage.clear();
  setCurrentWorkspace("acme", "ws-1");
  useAgentBulkEditPresetsStore.setState({ presets: [] });
});

async function chooseEnvOperation(
  user: ReturnType<typeof userEvent.setup>,
  index: number,
  option: "Set / replace" | "Remove",
) {
  const envOperations = screen.getAllByRole("combobox", { name: "Env operation" });
  await user.click(envOperations[index]!);
  await user.click(await screen.findByRole("option", { name: option }));
}

async function chooseCustomArgOperation(
  user: ReturnType<typeof userEvent.setup>,
  index: number,
  option: "Add" | "Replace" | "Remove",
) {
  const operations = screen.getAllByRole("combobox", { name: "Custom arg operation" });
  await user.click(operations[index]!);
  await user.click(await screen.findByRole("option", { name: option }));
}

describe("BulkEditAgentsDialog", () => {
  it("uses a stable dialog shell with top-level help", async () => {
    const user = userEvent.setup({ delay: null });
    render(
      <BulkEditAgentsDialog
        title="Bulk edit agents"
        runtimes={[makeRuntime()]}
        members={members}
        currentUserId={ME}
        affects={2}
        onApply={vi.fn()}
        onClose={vi.fn()}
      />,
      { wrapper: Wrap },
    );

    const dialog = screen.getByRole("dialog", { name: "Bulk edit agents" });
    expect(dialog).toHaveClass("!h-[85vh]", "flex", "overflow-hidden");
    const titleHelp = within(dialog).getByRole("button", { name: "Bulk edit help" });
    await user.hover(titleHelp);
    expect(await screen.findByText(/Disabled fields are left unchanged/)).toBeInTheDocument();
    expect(await screen.findByText(/Example: enable Model and leave it empty/)).toBeInTheDocument();
  });

  it("submits only enabled runtime/model fields", async () => {
    const user = userEvent.setup({ delay: null });
    const onApply = vi.fn().mockResolvedValue(undefined);
    const onClose = vi.fn();
    render(
      <BulkEditAgentsDialog
        title="Set runtime/model for all agents"
        runtimes={[makeRuntime()]}
        members={members}
        currentUserId={ME}
        onApply={onApply}
        onClose={onClose}
        affects={2}
      />,
      { wrapper: Wrap },
    );

    expect(screen.getByText("This will update 2 agents.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Apply" })).toBeDisabled();

    await user.click(screen.getByRole("checkbox", { name: "Runtime" }));
    await waitFor(() => expect(screen.getByText("Codex Runtime")).toBeInTheDocument());
    await user.click(screen.getByRole("checkbox", { name: "Model" }));
    await user.type(screen.getByRole("textbox", { name: "Model" }), " gpt-5.5 ");
    await user.click(screen.getByRole("button", { name: "Apply" }));

    await waitFor(() =>
      expect(onApply).toHaveBeenCalledWith({
        runtime_id: "rt-1",
        model: "gpt-5.5",
      } satisfies BulkUpdateAgentsRequest),
    );
    expect(onClose).toHaveBeenCalled();
  });

  it("clears the model override when Model is enabled but left empty", async () => {
    const user = userEvent.setup({ delay: null });
    const onApply = vi.fn().mockResolvedValue(undefined);
    render(
      <BulkEditAgentsDialog
        title="Bulk edit agents"
        runtimes={[makeRuntime()]}
        members={members}
        currentUserId={ME}
        affects={2}
        onApply={onApply}
        onClose={vi.fn()}
      />,
      { wrapper: Wrap },
    );

    // Enabling Model and leaving the input empty is the documented
    // clear-override UX (agents-runtime-model.mdx): Apply must be enabled and
    // the request must carry an explicit empty model so the server NULLs it.
    await user.click(screen.getByRole("checkbox", { name: "Model" }));
    const apply = screen.getByRole("button", { name: "Apply" });
    expect(apply).toBeEnabled();
    await user.click(apply);

    await waitFor(() =>
      expect(onApply).toHaveBeenCalledWith({
        model: "",
      } satisfies BulkUpdateAgentsRequest),
    );
  });

  it("submits enabled concurrency, custom arg operations, and env patch fields", async () => {
    const user = userEvent.setup({ delay: null });
    const onApply = vi.fn().mockResolvedValue(undefined);
    render(
      <BulkEditAgentsDialog
        title="Bulk edit agents"
        runtimes={[makeRuntime()]}
        members={members}
        currentUserId={ME}
        affects={2}
        onApply={onApply}
        onClose={vi.fn()}
        envKeyOptions={[
          { key: "API_KEY", agent_count: 2 },
          { key: "OLD_KEY", agent_count: 1 },
        ]}
      />,
      { wrapper: Wrap },
    );

    await user.click(screen.getByRole("checkbox", { name: "Max concurrent tasks" }));
    await user.clear(screen.getByRole("spinbutton", { name: "Max concurrent tasks" }));
    await user.type(screen.getByRole("spinbutton", { name: "Max concurrent tasks" }), "7");
    await user.click(screen.getByRole("checkbox", { name: "Custom args" }));
    await user.type(screen.getByLabelText("Custom arg to add"), "--foo");
    await user.click(screen.getByRole("button", { name: "Add custom arg operation" }));
    await chooseCustomArgOperation(user, 1, "Replace");
    await user.type(screen.getByLabelText("Existing custom arg"), "--old");
    await user.type(screen.getByRole("textbox", { name: "Replacement custom arg" }), "--new");
    await user.click(screen.getByRole("button", { name: "Add custom arg operation" }));
    await chooseCustomArgOperation(user, 2, "Remove");
    await user.type(screen.getByLabelText("Custom arg to remove"), "--remove-me");
    await user.click(screen.getByRole("checkbox", { name: "Environment variables" }));
    expect(screen.getByRole("button", { name: "Environment variables help" })).toBeInTheDocument();
    await user.type(screen.getByLabelText("Set env key"), "API_KEY");
    await user.type(screen.getByRole("textbox", { name: "Set env value" }), "secret");
    await user.click(screen.getByRole("button", { name: "Add env operation" }));
    await chooseEnvOperation(user, 1, "Remove");
    const removeKeys = screen.getAllByLabelText("Remove env key");
    await user.type(removeKeys[0]!, "OLD_KEY");
    await user.click(screen.getByRole("button", { name: "Apply" }));

    await waitFor(() =>
      expect(onApply).toHaveBeenCalledWith({
        max_concurrent_tasks: 7,
        custom_args_patch: [
          { action: "add", value: "--foo" },
          { action: "replace", value: "--old", replacement: "--new" },
          { action: "remove", value: "--remove-me" },
        ],
        env_set: { API_KEY: "secret" },
        env_remove: ["OLD_KEY"],
      }),
    );
  });

  it("splits multi-token custom arg additions like the single-agent Custom Args tab", async () => {
    const user = userEvent.setup({ delay: null });
    const onApply = vi.fn().mockResolvedValue(undefined);
    render(
      <BulkEditAgentsDialog
        title="Bulk edit agents"
        runtimes={[makeRuntime()]}
        members={members}
        currentUserId={ME}
        affects={2}
        onApply={onApply}
        onClose={vi.fn()}
      />,
      { wrapper: Wrap },
    );

    await user.click(screen.getByRole("checkbox", { name: "Custom args" }));
    await user.type(screen.getByLabelText("Custom arg to add"), "--max-turns 100");
    await user.click(screen.getByRole("button", { name: "Apply" }));

    await waitFor(() =>
      expect(onApply).toHaveBeenCalledWith({
        custom_args_patch: [
          { action: "add", value: "--max-turns" },
          { action: "add", value: "100" },
        ],
      } satisfies BulkUpdateAgentsRequest),
    );
  });

  it("lets users choose existing args while editing custom arg rows", async () => {
    const user = userEvent.setup({ delay: null });
    const onApply = vi.fn().mockResolvedValue(undefined);
    render(
      <BulkEditAgentsDialog
        title="Bulk edit agents"
        runtimes={[makeRuntime()]}
        members={members}
        currentUserId={ME}
        affects={2}
        onApply={onApply}
        onClose={vi.fn()}
        customArgOptions={[
          { value: "--permission-mode", agentCount: 2 },
          { value: "acceptEdits", agentCount: 1 },
          { value: "--verbose", agentCount: 1 },
        ]}
      />,
      { wrapper: Wrap },
    );

    await user.click(screen.getByRole("checkbox", { name: "Custom args" }));
    expect(screen.getByText("Existing args")).toBeInTheDocument();
    await user.hover(screen.getByRole("button", { name: "Existing args help" }));
    expect(await screen.findByText(/Choose an arg to fill the next empty row/)).toBeInTheDocument();
    expect(screen.queryByLabelText("Existing custom args")).toBeNull();
    expect(screen.queryByRole("button", { name: "Add --permission-mode" })).toBeNull();
    await user.click(screen.getByRole("combobox", { name: "Choose existing custom arg" }));
    await user.click(await screen.findByRole("option", { name: /--permission-mode/ }));
    const firstArg = screen.getByLabelText("Existing custom arg");
    expect(firstArg).toHaveValue("--permission-mode");
    await user.type(screen.getByRole("textbox", { name: "Replacement custom arg" }), "acceptEdits");
    await user.click(screen.getByRole("combobox", { name: "Choose existing custom arg" }));
    await user.click(await screen.findByRole("option", { name: /--verbose/ }));
    await chooseCustomArgOperation(user, 1, "Remove");
    expect(screen.getAllByLabelText("Custom arg to remove")[0]).toHaveValue("--verbose");
    await user.click(screen.getAllByRole("button", { name: "Remove custom arg" })[1]!);
    expect(screen.getAllByRole("combobox", { name: "Custom arg operation" })).toHaveLength(1);
    expect(screen.queryByRole("button", { name: "Replace first custom arg with acceptEdits" })).toBeNull();

    await user.click(screen.getByRole("button", { name: "Apply" }));
    await waitFor(() =>
      expect(onApply).toHaveBeenCalledWith({
        custom_args_patch: [
          { action: "replace", value: "--permission-mode", replacement: "acceptEdits" },
        ],
      }),
    );
  });

  it("lets users choose existing env keys without loading secret values", async () => {
    const user = userEvent.setup({ delay: null });
    const onApply = vi.fn().mockResolvedValue(undefined);
    render(
      <BulkEditAgentsDialog
        title="Bulk edit agents"
        runtimes={[makeRuntime()]}
        members={members}
        currentUserId={ME}
        affects={2}
        onApply={onApply}
        onClose={vi.fn()}
        envKeyOptions={[
          { key: "ANTHROPIC_API_KEY", agent_count: 2 },
          { key: "OLD_KEY", agent_count: 1 },
        ]}
      />,
      { wrapper: Wrap },
    );

    await user.click(screen.getByRole("checkbox", { name: "Environment variables" }));
    expect(screen.getByText("Existing keys")).toBeInTheDocument();
    await user.hover(screen.getByRole("button", { name: "Existing env keys help" }));
    expect(await screen.findByText(/Choose a key to fill the next empty env row/)).toBeInTheDocument();
    expect(screen.queryByLabelText("Existing env keys")).toBeNull();
    expect(screen.queryByRole("button", { name: "Add ANTHROPIC_API_KEY" })).toBeNull();
    await user.click(screen.getByRole("combobox", { name: "Choose existing env key" }));
    await user.click(await screen.findByRole("option", { name: /ANTHROPIC_API_KEY/ }));
    expect(screen.getByLabelText("Set env key")).toHaveValue("ANTHROPIC_API_KEY");
    await user.type(screen.getByRole("textbox", { name: "Set env value" }), "new-secret");
    await user.click(screen.getByRole("combobox", { name: "Choose existing env key" }));
    await user.click(await screen.findByRole("option", { name: /OLD_KEY/ }));
    await chooseEnvOperation(user, 1, "Remove");
    expect(screen.getByLabelText("Remove env key")).toHaveValue("OLD_KEY");
    expect(screen.queryByText("sk-secret")).toBeNull();
    expect(screen.queryByRole("button", { name: "Set / replace ANTHROPIC_API_KEY" })).toBeNull();

    await user.click(screen.getByRole("button", { name: "Apply" }));
    await waitFor(() =>
      expect(onApply).toHaveBeenCalledWith({
        env_set: { ANTHROPIC_API_KEY: "new-secret" },
        env_remove: ["OLD_KEY"],
      }),
    );
  });

  it("shows env key load failures instead of claiming there are no existing keys", async () => {
    const user = userEvent.setup({ delay: null });
    const Dialog = BulkEditAgentsDialog as ComponentType<ComponentProps<typeof BulkEditAgentsDialog> & {
      envKeysError?: boolean;
    }>;
    render(
      <Dialog
        title="Bulk edit agents"
        runtimes={[makeRuntime()]}
        members={members}
        currentUserId={ME}
        affects={2}
        onApply={vi.fn()}
        onClose={vi.fn()}
        envKeysError
      />,
      { wrapper: Wrap },
    );

    await user.click(screen.getByRole("checkbox", { name: "Environment variables" }));
    expect(screen.getByText("Couldn't load existing env keys. You can still type a key manually.")).toBeInTheDocument();
    expect(screen.queryByText("No existing env keys for these agents. Type a new key.")).toBeNull();
    expect(screen.getByLabelText("Set env key")).toBeInTheDocument();
  });

  it("keeps env choice visible while existing keys are loading or absent", async () => {
    const user = userEvent.setup({ delay: null });
    const { rerender } = render(
      <BulkEditAgentsDialog
        title="Bulk edit agents"
        runtimes={[makeRuntime()]}
        members={members}
        currentUserId={ME}
        affects={2}
        onApply={vi.fn()}
        onClose={vi.fn()}
        envKeysLoading
      />,
      { wrapper: Wrap },
    );

    await user.click(screen.getByRole("checkbox", { name: "Environment variables" }));
    expect(screen.getByText("Loading existing keys…")).toBeInTheDocument();
    expect(screen.getByLabelText("Set env key")).toBeInTheDocument();

    rerender(
      <BulkEditAgentsDialog
        title="Bulk edit agents"
        runtimes={[makeRuntime()]}
        members={members}
        currentUserId={ME}
        affects={2}
        onApply={vi.fn()}
        onClose={vi.fn()}
      />,
    );

    expect(screen.getByText("No existing env keys for these agents. Type a new key.")).toBeInTheDocument();
    expect(screen.getByLabelText("Set env key")).toBeInTheDocument();
  });

  it("saves and loads local presets by name without restoring env values", async () => {
    const user = userEvent.setup({ delay: null });
    render(
      <BulkEditAgentsDialog
        title="Bulk edit agents"
        runtimes={[makeRuntime()]}
        members={members}
        currentUserId={ME}
        affects={2}
        onApply={vi.fn()}
        onClose={vi.fn()}
      />,
      { wrapper: Wrap },
    );

    expect(screen.getByText("Local presets")).toBeInTheDocument();
    expect(screen.getByText(/Saved in this browser/)).toBeInTheDocument();

    await user.click(screen.getByRole("checkbox", { name: "Model" }));
    await user.type(screen.getByRole("textbox", { name: "Model" }), "claude-sonnet-4");
    await user.click(screen.getByRole("checkbox", { name: "Environment variables" }));
    await user.type(screen.getByLabelText("Set env key"), "ANTHROPIC_API_KEY");
    await user.type(screen.getByRole("textbox", { name: "Set env value" }), "sk-secret");
    await user.type(screen.getByRole("textbox", { name: "Preset name" }), "Claude preset");
    await user.click(screen.getByRole("button", { name: "Save local preset" }));

    expect(localStorage.getItem("multica_agent_bulk_edit_presets:acme")).not.toContain("sk-secret");
    const presetId = useAgentBulkEditPresetsStore.getState().presets[0]!.id;
    const presetSelect = screen.getByRole("combobox", { name: "Local preset" });
    expect(presetSelect).toHaveTextContent("Claude preset");
    expect(presetSelect).not.toHaveTextContent(presetId);
    expect(screen.getByRole("button", { name: "Load preset" })).toBeEnabled();
    expect(screen.queryByRole("button", { name: "Apply local preset" })).toBeNull();

    await user.clear(screen.getByRole("textbox", { name: "Model" }));
    await user.clear(screen.getByLabelText("Set env key"));
    await user.clear(screen.getByRole("textbox", { name: "Set env value" }));
    await user.click(screen.getByRole("combobox", { name: "Local preset" }));
    await user.click(await screen.findByRole("option", { name: "Claude preset" }));
    expect(screen.getByRole("combobox", { name: "Local preset" })).toHaveTextContent("Claude preset");
    expect(screen.getByRole("combobox", { name: "Local preset" })).not.toHaveTextContent(presetId);
    await user.click(screen.getByRole("button", { name: "Load preset" }));

    expect(screen.getByRole("textbox", { name: "Model" })).toHaveValue("claude-sonnet-4");
    expect(screen.getByLabelText("Set env key")).toHaveValue("ANTHROPIC_API_KEY");
    expect(screen.getByRole("textbox", { name: "Set env value" })).toHaveValue("");
    expect(screen.getByText(/Enter new values before applying/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Apply" })).toBeDisabled();
  });

  it("keeps env rows stable and explains set/remove operations", async () => {
    const user = userEvent.setup({ delay: null });
    render(
      <BulkEditAgentsDialog
        title="Bulk edit agents"
        runtimes={[makeRuntime()]}
        members={members}
        currentUserId={ME}
        affects={2}
        onApply={vi.fn()}
        onClose={vi.fn()}
        envKeyOptions={[{ key: "API_KEY", agent_count: 2 }]}
      />,
      { wrapper: Wrap },
    );

    await user.click(screen.getByRole("checkbox", { name: "Environment variables" }));
    const setHelp = screen.getByRole("button", { name: "Set / replace env row help" });
    const envRow = screen
      .getByRole("combobox", { name: "Env operation" })
      .closest("div");
    expect(envRow).toHaveClass("grid-cols-1", "sm:grid-cols-[minmax(8rem,0.65fr)_minmax(0,1fr)_minmax(0,1fr)_auto_auto]");
    expect(screen.getByRole("combobox", { name: "Env operation" })).toHaveTextContent("Set / replace");
    await user.hover(setHelp);
    expect(await screen.findByText(/Set API_KEY to a new value/)).toBeInTheDocument();

    await chooseEnvOperation(user, 0, "Remove");
    expect(screen.getByRole("combobox", { name: "Env operation" })).toHaveTextContent("Remove");
    expect(screen.getByRole("textbox", { name: "Remove env value not used" })).toBeDisabled();
    const removeHelp = screen.getByRole("button", { name: "Remove env row help" });
    await user.hover(removeHelp);
    expect(await screen.findByText(/Remove OLD_KEY from agents where it exists/)).toBeInTheDocument();
  });

  it("explains how custom arg operations patch the targeted agents args", async () => {
    const user = userEvent.setup({ delay: null });
    render(
      <BulkEditAgentsDialog
        title="Bulk edit agents"
        runtimes={[makeRuntime()]}
        members={members}
        currentUserId={ME}
        affects={2}
        onApply={vi.fn()}
        onClose={vi.fn()}
      />,
      { wrapper: Wrap },
    );

    await user.click(screen.getByRole("checkbox", { name: "Custom args" }));
    const customArgsHelp = screen.getByRole("button", { name: "Custom args help" });
    await user.hover(customArgsHelp);
    expect(await screen.findByText(/applies Add, Replace, or Remove operations/)).toBeInTheDocument();
    expect(await screen.findByText(/same whitespace splitting as the Custom Args tab/)).toBeInTheDocument();
    expect(await screen.findByText(/exact existing arg values/)).toBeInTheDocument();
    const customArgHelp = screen.getByRole("button", { name: "Add custom arg row help" });
    await user.hover(customArgHelp);
    expect(await screen.findByText(/Add this arg to each targeted agent/)).toBeInTheDocument();
    expect(await screen.findByText(/--max-turns 100 adds --max-turns and 100/)).toBeInTheDocument();
  });

  it("uses the last env operation for a duplicate key", async () => {
    const user = userEvent.setup({ delay: null });
    const onApply = vi.fn().mockResolvedValue(undefined);
    render(
      <BulkEditAgentsDialog
        title="Bulk edit agents"
        runtimes={[makeRuntime()]}
        members={members}
        currentUserId={ME}
        affects={2}
        onApply={onApply}
        onClose={vi.fn()}
        envKeyOptions={[{ key: "API_KEY", agent_count: 2 }]}
      />,
      { wrapper: Wrap },
    );

    await user.click(screen.getByRole("checkbox", { name: "Environment variables" }));
    await user.type(screen.getByLabelText("Set env key"), "API_KEY");
    await user.type(screen.getByRole("textbox", { name: "Set env value" }), "old-secret");
    await user.click(screen.getByRole("button", { name: "Add env operation" }));
    await chooseEnvOperation(user, 1, "Set / replace");
    const setKeys = screen.getAllByLabelText("Set env key");
    const setValues = screen.getAllByRole("textbox", { name: "Set env value" });
    await user.type(setKeys[1]!, "API_KEY");
    await user.type(setValues[1]!, "new-secret");
    await user.click(screen.getByRole("button", { name: "Apply" }));

    await waitFor(() =>
      expect(onApply).toHaveBeenCalledWith({
        env_set: { API_KEY: "new-secret" },
      } satisfies BulkUpdateAgentsRequest),
    );
  });

  it("keeps apply disabled when only custom args is enabled and left empty", async () => {
    const user = userEvent.setup({ delay: null });
    const onApply = vi.fn().mockResolvedValue(undefined);
    render(
      <BulkEditAgentsDialog
        title="Bulk edit agents"
        runtimes={[makeRuntime()]}
        members={members}
        currentUserId={ME}
        affects={2}
        onApply={onApply}
        onClose={vi.fn()}
      />,
      { wrapper: Wrap },
    );

    await user.click(screen.getByRole("checkbox", { name: "Custom args" }));
    expect(screen.getByRole("button", { name: "Apply" })).toBeDisabled();
    expect(onApply).not.toHaveBeenCalled();
  });

  it("keeps the dialog open when apply fails", async () => {
    const user = userEvent.setup({ delay: null });
    const onApply = vi.fn().mockRejectedValue(new Error("server boom"));
    const onClose = vi.fn();
    render(
      <BulkEditAgentsDialog
        title="Set runtime/model for all agents"
        runtimes={[makeRuntime()]}
        members={members}
        currentUserId={ME}
        onApply={onApply}
        onClose={onClose}
      />,
      { wrapper: Wrap },
    );

    await user.click(screen.getByRole("checkbox", { name: "Model" }));
    await user.click(screen.getByRole("button", { name: "Apply" }));

    await waitFor(() => expect(onApply).toHaveBeenCalled());
    expect(onClose).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Apply" })).toBeInTheDocument();
  });
});
