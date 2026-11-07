import { vi, it, expect, afterEach } from "vitest";
import { render, screen, fireEvent, cleanup, waitFor } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import type { Agent } from "@multica/core/types";
import enRuntimes from "../../../locales/en/runtimes.json";
import { ClaudeAgentTab } from "./claude-agent-tab";

vi.mock("../claude-agent-select", () => ({
  ClaudeAgentSelect: ({ value }: { value: string }) => <span>{value}</span>,
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

afterEach(cleanup);

function mount(runtimeConfig: Record<string, unknown>, canEdit = true) {
  const onSave = vi.fn().mockResolvedValue(undefined);
  render(
    <I18nProvider locale="en" resources={{ en: { runtimes: enRuntimes } }}>
      <ClaudeAgentTab
        canEdit={canEdit}
        agent={{ runtime_config: runtimeConfig } as unknown as Agent}
        onSave={onSave}
      />
    </I18nProvider>,
  );
  return onSave;
}

it("saves a default repository alongside the Claude agent binding", async () => {
  const onSave = mount({ claude_agent_id: "agent_a", keep: true });
  const save = screen.getByRole("button", { name: "Save" });
  expect(save).toBeDisabled();
  fireEvent.change(screen.getByLabelText("Repository URL"), {
    target: { value: " https://github.com/acme/app " },
  });
  fireEvent.change(screen.getByLabelText("Branch"), {
    target: { value: "main" },
  });
  fireEvent.click(save);
  await waitFor(() =>
    expect(onSave).toHaveBeenCalledWith({
      runtime_config: {
        keep: true,
        claude_agent_id: "agent_a",
        repository: { url: "https://github.com/acme/app", ref: "main" },
      },
    }),
  );
});

it("removes the default repository when the URL is cleared", async () => {
  const onSave = mount({
    claude_agent_id: "agent_a",
    repository: { url: "https://github.com/acme/app", ref: "" },
  });
  fireEvent.change(screen.getByLabelText("Repository URL"), {
    target: { value: "" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() =>
    expect(onSave).toHaveBeenCalledWith({
      runtime_config: { claude_agent_id: "agent_a" },
    }),
  );
});

it("only offers the branch once there is a repository", () => {
  mount({ claude_agent_id: "agent_a" });
  expect(screen.getByLabelText("Branch")).toBeDisabled();
  fireEvent.change(screen.getByLabelText("Repository URL"), {
    target: { value: "https://github.com/acme/app" },
  });
  expect(screen.getByLabelText("Branch")).toBeEnabled();
});

it("is read-only when the viewer cannot edit the agent", () => {
  mount({ claude_agent_id: "agent_a" }, false);
  expect(screen.getByLabelText("Repository URL")).toBeDisabled();
  expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
});
