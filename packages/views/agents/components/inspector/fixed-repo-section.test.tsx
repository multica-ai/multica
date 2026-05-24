import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Agent } from "@multica/core/types";
import { FixedRepoSection } from "./fixed-repo-section";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../../locales/en/common.json";
import enAgents from "../../../locales/en/agents.json";

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

function makeAgent(overrides: Partial<Agent> = {}): Agent {
  return {
    id: "agent-1",
    workspace_id: "ws-1",
    runtime_id: "rt-1",
    name: "Agent",
    description: "",
    instructions: "",
    avatar_url: null,
    runtime_mode: "local",
    runtime_config: {},
    custom_env: {},
    custom_args: [],
    custom_env_redacted: false,
    visibility: "private",
    status: "idle",
    max_concurrent_tasks: 1,
    model: "",
    fixed_repo_enabled: false,
    fixed_repo_paths: [],
    vcs_type: "",
    init_script: "",
    cleanup_script: "",
    owner_id: "user-1",
    skills: [],
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    archived_at: null,
    archived_by: null,
    ...overrides,
  };
}

function renderSection(
  agent: Agent,
  canEdit: boolean,
  onUpdate: (data: Record<string, unknown>) => Promise<void> = vi.fn().mockResolvedValue(undefined),
) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });

  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={queryClient}>
        <FixedRepoSection
          enabled={agent.fixed_repo_enabled}
          paths={agent.fixed_repo_paths}
          vcsType={agent.vcs_type}
          initScript={agent.init_script}
          cleanupScript={agent.cleanup_script}
          canEdit={canEdit}
          onUpdate={onUpdate}
        />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

describe("FixedRepoSection", () => {
  describe("when canEdit is false", () => {
    it("shows static text without toggle or edit button", () => {
      const agent = makeAgent({ fixed_repo_enabled: false });
      renderSection(agent, false);

      expect(screen.getByText("Fixed Repository")).toBeInTheDocument();
      expect(screen.getByText("Disabled")).toBeInTheDocument();
      expect(screen.queryByRole("switch")).not.toBeInTheDocument();
      expect(screen.queryByLabelText("Edit fixed repository settings")).not.toBeInTheDocument();
    });

    it("shows Enabled text when fixed_repo_enabled is true", () => {
      const agent = makeAgent({ fixed_repo_enabled: true });
      renderSection(agent, false);

      expect(screen.getByText("Enabled")).toBeInTheDocument();
    });
  });

  describe("when canEdit is true", () => {
    it("shows toggle switch and disabled state by default", () => {
      const agent = makeAgent();
      renderSection(agent, true);

      expect(screen.getByRole("switch")).toBeInTheDocument();
      expect(screen.getByText("Disabled")).toBeInTheDocument();
      expect(screen.queryByLabelText("Edit fixed repository settings")).not.toBeInTheDocument();
    });

    it("shows edit button when enabled", () => {
      const agent = makeAgent({ fixed_repo_enabled: true });
      renderSection(agent, true);

      expect(screen.getByText("Enabled")).toBeInTheDocument();
      expect(screen.getByLabelText("Edit fixed repository settings")).toBeInTheDocument();
    });

    it("calls onUpdate when toggle is switched", async () => {
      const user = userEvent.setup();
      const agent = makeAgent();
      const onUpdate = vi.fn().mockResolvedValue(undefined);
      renderSection(agent, true, onUpdate);

      const switchEl = screen.getByRole("switch");
      await user.click(switchEl);

      expect(onUpdate).toHaveBeenCalledWith({ fixed_repo_enabled: true });
    });
  });

  describe("edit dialog", () => {
    it("opens dialog when edit button is clicked", async () => {
      const user = userEvent.setup();
      const agent = makeAgent({ fixed_repo_enabled: true });
      renderSection(agent, true);

      const editBtn = screen.getByLabelText("Edit fixed repository settings");
      await user.click(editBtn);

      expect(screen.getByText("Fixed Repository Settings")).toBeInTheDocument();
      expect(screen.getByText("VCS Type")).toBeInTheDocument();
      expect(screen.getByText("Paths")).toBeInTheDocument();
      expect(screen.getByText("Init Script")).toBeInTheDocument();
      expect(screen.getByText("Cleanup Script")).toBeInTheDocument();
    });

    it("shows existing paths in dialog", async () => {
      const user = userEvent.setup();
      const agent = makeAgent({
        fixed_repo_enabled: true,
        fixed_repo_paths: ["/data/repos/project-a", "/data/repos/project-b"],
      });
      renderSection(agent, true);

      await user.click(screen.getByLabelText("Edit fixed repository settings"));

      expect(screen.getByText("/data/repos/project-a")).toBeInTheDocument();
      expect(screen.getByText("/data/repos/project-b")).toBeInTheDocument();
    });

    it("can add a new path", async () => {
      const user = userEvent.setup();
      const agent = makeAgent({
        fixed_repo_enabled: true,
        fixed_repo_paths: [],
      });
      const onUpdate = vi.fn().mockResolvedValue(undefined);
      renderSection(agent, true, onUpdate);

      await user.click(screen.getByLabelText("Edit fixed repository settings"));

      const input = screen.getByPlaceholderText("/path/to/repo");
      await user.type(input, "/data/repos/new-project");

      const addBtn = screen.getByRole("button", { name: "" }); // Plus icon button
      await user.click(addBtn);

      expect(screen.getByText("/data/repos/new-project")).toBeInTheDocument();
    });

    it("can remove a path", async () => {
      const user = userEvent.setup();
      const agent = makeAgent({
        fixed_repo_enabled: true,
        fixed_repo_paths: ["/data/repos/project-a"],
      });
      renderSection(agent, true);

      await user.click(screen.getByLabelText("Edit fixed repository settings"));

      const removeBtn = screen.getByLabelText("Remove /data/repos/project-a");
      await user.click(removeBtn);

      expect(screen.getByText("No paths configured")).toBeInTheDocument();
    });

    it("save button is disabled when no changes", async () => {
      const user = userEvent.setup();
      const agent = makeAgent({
        fixed_repo_enabled: true,
        vcs_type: "git",
        fixed_repo_paths: ["/data/repos/project"],
      });
      renderSection(agent, true);

      await user.click(screen.getByLabelText("Edit fixed repository settings"));

      const saveBtn = screen.getByRole("button", { name: "Save" });
      expect(saveBtn).toBeDisabled();
    });

    it("calls onUpdate with all fields on save", async () => {
      const user = userEvent.setup();
      const agent = makeAgent({
        fixed_repo_enabled: true,
        vcs_type: "",
        fixed_repo_paths: [],
        init_script: "",
        cleanup_script: "",
      });
      const onUpdate = vi.fn().mockResolvedValue(undefined);
      renderSection(agent, true, onUpdate);

      await user.click(screen.getByLabelText("Edit fixed repository settings"));

      // Add a path
      const input = screen.getByPlaceholderText("/path/to/repo");
      await user.type(input, "/data/repos/project");
      await user.click(screen.getByRole("button", { name: "" })); // Plus icon

      // Change VCS type - click on the select trigger
      const selectTrigger = screen.getByRole("combobox");
      await user.click(selectTrigger);

      // Click on git option
      await user.click(screen.getByRole("option", { name: "git" }));

      // Add init script
      const initInput = screen.getByPlaceholderText("/path/to/init.sh");
      await user.type(initInput, "/scripts/init.sh");

      // Save
      const saveBtn = screen.getByRole("button", { name: "Save" });
      await user.click(saveBtn);

      expect(onUpdate).toHaveBeenCalledWith({
        vcs_type: "git",
        fixed_repo_paths: ["/data/repos/project"],
        init_script: "/scripts/init.sh",
        cleanup_script: "",
      });
    });
  });
});