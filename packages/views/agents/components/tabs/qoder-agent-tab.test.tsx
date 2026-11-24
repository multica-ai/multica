import { vi, it, expect, afterEach } from "vitest";
import { render, screen, fireEvent, cleanup, waitFor } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import type { Agent } from "@multica/core/types";
import enRuntimes from "../../../locales/en/runtimes.json";
import { QoderAgentTab } from "./qoder-agent-tab";

vi.mock("../qoder-agent-select", () => ({
  QoderAgentSelect: ({ value }: { value: string }) => <span>{value}</span>,
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

afterEach(cleanup);

function mount(runtimeConfig: Record<string, unknown>) {
  const onSave = vi.fn().mockResolvedValue(undefined);
  render(
    <I18nProvider locale="en" resources={{ en: { runtimes: enRuntimes } }}>
      <QoderAgentTab
        agent={{ runtime_config: runtimeConfig } as unknown as Agent}
        onSave={onSave}
      />
    </I18nProvider>,
  );
  return onSave;
}

it("saves a default repository alongside the QCA Agent binding", async () => {
  const onSave = mount({ qoder_agent_id: "agent_a", keep: true });
  const save = screen.getByRole("button", { name: "Save association" });
  expect(save).toBeDisabled();
  fireEvent.change(screen.getByLabelText("GitHub URL"), {
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
        qoder_agent_id: "agent_a",
        repository: { url: "https://github.com/acme/app", ref: "main" },
      },
    }),
  );
});

it("removes the default repository when the URL is cleared", async () => {
  const onSave = mount({
    qoder_agent_id: "agent_a",
    repository: { url: "https://github.com/acme/app", ref: "" },
  });
  fireEvent.change(screen.getByLabelText("GitHub URL"), {
    target: { value: "" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save association" }));
  await waitFor(() =>
    expect(onSave).toHaveBeenCalledWith({
      runtime_config: { qoder_agent_id: "agent_a" },
    }),
  );
});
