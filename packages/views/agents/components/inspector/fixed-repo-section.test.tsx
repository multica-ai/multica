// @vitest-environment jsdom

import { describe, expect, it, vi, afterEach } from "vitest";
import { render, screen, fireEvent, cleanup } from "@testing-library/react";
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

function openDialog() {
  const editBtn = screen.getByLabelText("Edit fixed repository settings");
  fireEvent.click(editBtn);
}

/** Find the "+" button next to the path input by its disabled state when input is empty. */
function getAddPathButton(): HTMLButtonElement {
  // The add button is the disabled button (input is empty before typing)
  // After typing, it becomes enabled. Find it by its position next to the input.
  const input = screen.getByPlaceholderText("/path/to/repo");
  const container = input.parentElement!;
  // The button is a sibling of the input in the same flex row
  const buttons = container.querySelectorAll("button");
  return Array.from(buttons).find((b) => b.querySelector("svg"))!;
}

describe("FixedRepoSection", () => {
  afterEach(() => {
    cleanup();
  });

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

    it("calls onUpdate when toggle is switched", () => {
      const agent = makeAgent();
      const onUpdate = vi.fn().mockResolvedValue(undefined);
      renderSection(agent, true, onUpdate);

      fireEvent.click(screen.getByRole("switch"));

      expect(onUpdate).toHaveBeenCalledWith({ fixed_repo_enabled: true });
    });
  });

  describe("edit dialog", () => {
    it("opens dialog when edit button is clicked", () => {
      const agent = makeAgent({ fixed_repo_enabled: true });
      renderSection(agent, true);

      openDialog();

      expect(screen.getByText("Fixed Repository Settings")).toBeInTheDocument();
      expect(screen.getByText("VCS Type")).toBeInTheDocument();
      expect(screen.getByText("Paths")).toBeInTheDocument();
      expect(screen.getByText("Init Script")).toBeInTheDocument();
      expect(screen.getByText("Cleanup Script")).toBeInTheDocument();
    });

    it("shows existing paths in dialog", () => {
      const agent = makeAgent({
        fixed_repo_enabled: true,
        fixed_repo_paths: ["/data/repos/project-a", "/data/repos/project-b"],
      });
      renderSection(agent, true);

      openDialog();

      expect(screen.getByText("/data/repos/project-a")).toBeInTheDocument();
      expect(screen.getByText("/data/repos/project-b")).toBeInTheDocument();
    });

    it("can add a new path", () => {
      const agent = makeAgent({
        fixed_repo_enabled: true,
        fixed_repo_paths: [],
      });
      renderSection(agent, true);

      openDialog();

      const input = screen.getByPlaceholderText("/path/to/repo");
      fireEvent.change(input, { target: { value: "/data/repos/new-project" } });

      const addBtn = getAddPathButton();
      fireEvent.click(addBtn);

      expect(screen.getByText("/data/repos/new-project")).toBeInTheDocument();
    });

    it("can add a path by pressing Enter", () => {
      const agent = makeAgent({
        fixed_repo_enabled: true,
        fixed_repo_paths: [],
      });
      renderSection(agent, true);

      openDialog();

      const input = screen.getByPlaceholderText("/path/to/repo");
      fireEvent.change(input, { target: { value: "/data/repos/new-project" } });
      fireEvent.keyDown(input, { key: "Enter" });

      expect(screen.getByText("/data/repos/new-project")).toBeInTheDocument();
    });

    it("can remove a path", () => {
      const agent = makeAgent({
        fixed_repo_enabled: true,
        fixed_repo_paths: ["/data/repos/project-a"],
      });
      renderSection(agent, true);

      openDialog();

      const removeBtn = screen.getByLabelText("Remove /data/repos/project-a");
      fireEvent.click(removeBtn);

      expect(screen.getByText("No paths configured")).toBeInTheDocument();
    });

    it("save button is disabled when no changes", () => {
      const agent = makeAgent({
        fixed_repo_enabled: true,
        vcs_type: "git",
        fixed_repo_paths: ["/data/repos/project"],
      });
      renderSection(agent, true);

      openDialog();

      const saveBtn = screen.getByRole("button", { name: "Save" });
      expect(saveBtn).toBeDisabled();
    });

    it("calls onUpdate with changed fields on save", () => {
      const agent = makeAgent({
        fixed_repo_enabled: true,
        vcs_type: "",
        fixed_repo_paths: [],
        init_script: "",
        cleanup_script: "",
      });
      const onUpdate = vi.fn().mockResolvedValue(undefined);
      renderSection(agent, true, onUpdate);

      openDialog();

      // Add a path
      const input = screen.getByPlaceholderText("/path/to/repo");
      fireEvent.change(input, { target: { value: "/data/repos/project" } });
      fireEvent.keyDown(input, { key: "Enter" });

      // Add init script
      const initInput = screen.getByPlaceholderText("/path/to/init.sh");
      fireEvent.change(initInput, { target: { value: "/scripts/init.sh" } });

      // Save
      const saveBtn = screen.getByRole("button", { name: "Save" });
      fireEvent.click(saveBtn);

      expect(onUpdate).toHaveBeenCalledWith({
        vcs_type: "",
        fixed_repo_paths: ["/data/repos/project"],
        init_script: "/scripts/init.sh",
        cleanup_script: "",
      });
    });
  });
});
