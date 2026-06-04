import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

const mockGetAgentToolConfig = vi.hoisted(() => vi.fn());
const mockListFirtalRegistryDataSources = vi.hoisted(() => vi.fn());
const mockSetAgentToolConfig = vi.hoisted(() => vi.fn());

vi.mock("../../api", () => ({
  getAgentToolConfig: mockGetAgentToolConfig,
  listFirtalRegistryDataSources: mockListFirtalRegistryDataSources,
  setAgentToolConfig: mockSetAgentToolConfig,
  listAgentToolOverrides: vi.fn(),
  setAgentToolOverride: vi.fn(),
  clearAgentToolOverride: vi.fn(),
}));

import { FirtalRegistryConfigDialog } from "./firtal-registry-config-dialog";

function renderDialog(open = true) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <FirtalRegistryConfigDialog
        agentId="agent-1"
        open={open}
        onOpenChange={() => {}}
      />
    </QueryClientProvider>,
  );
}

describe("FirtalRegistryConfigDialog", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetAgentToolConfig.mockResolvedValue({
      allowed_data_sources: ["ds-a"],
    });
    mockListFirtalRegistryDataSources.mockResolvedValue([
      { id: "ds-a", name: "Source A" },
      { id: "ds-b", name: "Source B" },
    ]);
    mockSetAgentToolConfig.mockResolvedValue(undefined);
  });

  it("toggles allow-all and saves allowed_data_sources_all", async () => {
    const user = userEvent.setup();
    renderDialog();

    await screen.findByText("Source A");
    const allowAll = screen.getByRole("switch", { name: /Tillad alle data sources/i });
    await user.click(allowAll);

    await user.click(screen.getByRole("button", { name: "Gem" }));

    await waitFor(() => {
      expect(mockSetAgentToolConfig).toHaveBeenCalledWith(
        "agent-1",
        "firtal_registry",
        { allowed_data_sources_all: true },
      );
    });
  });

  it("saves selected data source ids when allow-all is off", async () => {
    const user = userEvent.setup();
    renderDialog();

    await screen.findByText("Source B");
    const checkbox = screen.getByRole("checkbox", { name: /Source B/i });
    await user.click(checkbox);

    await user.click(screen.getByRole("button", { name: "Gem" }));

    await waitFor(() => {
      expect(mockSetAgentToolConfig).toHaveBeenCalledWith(
        "agent-1",
        "firtal_registry",
        { allowed_data_sources: ["ds-a", "ds-b"] },
      );
    });
  });
});
