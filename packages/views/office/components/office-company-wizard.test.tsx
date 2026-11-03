/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { CompanyTemplateApplyResult } from "@multica/core/office";
import { COMPANY_SQUAD_KEYS } from "@multica/core/squads";
import type { Squad } from "@multica/core/types";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { renderWithI18n } from "../../test/i18n";
import { OfficeCompanyWizard, companyWizardPhase } from "./office-company-wizard";

const api = vi.hoisted(() => ({ applyCompanyTemplate: vi.fn() }));
const toast = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api,
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (s: { user: { id: string } }) => unknown) =>
    selector({ user: { id: "user-me" } }),
}));

// The runtime list is seeded in the query cache; the picker is replaced by a button that picks rt-1.
vi.mock("@multica/core/runtimes", () => ({
  runtimeListOptions: () => ({ queryKey: ["runtimes-test"], queryFn: async () => [] }),
  isRuntimeUsableForUser: () => true,
}));

vi.mock("../../agents/components/runtime-picker", () => ({
  RuntimePicker: ({ onSelect }: { onSelect: (id: string) => void }) => (
    <button type="button" onClick={() => onSelect("rt-1")}>
      pick runtime
    </button>
  ),
}));

vi.mock("sonner", () => ({ toast }));

const RUNTIME = { id: "rt-1", owner_id: "user-me", status: "online", visibility: "private" };

const DONE: CompanyTemplateApplyResult = {
  steps: [
    { key: "squad:market_research", status: "created", message: "Market Research created" },
    { key: "agent:Market Scout", status: "reused", message: "Market Scout already there" },
  ],
  pipeline_id: null,
  unowned_stages: [],
};

function renderWizard(squads: Squad[] = []) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  queryClient.setQueryData(["runtimes-test"], [RUNTIME]);
  queryClient.setQueryData(workspaceKeys.members("ws-1"), []);
  queryClient.setQueryData(workspaceKeys.squads("ws-1"), squads);
  const onClose = vi.fn();
  renderWithI18n(
    <QueryClientProvider client={queryClient}>
      <OfficeCompanyWizard open onClose={onClose} />
    </QueryClientProvider>,
  );
  return { onClose };
}

const squad = (name: string): Squad =>
  ({ id: `sq-${name}`, name, archived_at: null }) as unknown as Squad;

describe("companyWizardPhase", () => {
  const none = { loading: false, applying: false, result: null, errored: false };

  it("shows the choices once nothing is loading and nothing has run", () => {
    expect(companyWizardPhase(none)).toBe("choosing");
    expect(companyWizardPhase({ ...none, loading: true })).toBe("loading");
  });

  it("shows applying while the request is in flight, whatever else is set", () => {
    expect(companyWizardPhase({ ...none, applying: true, loading: true })).toBe("applying");
  });

  it("shows done only when every step succeeded", () => {
    expect(companyWizardPhase({ ...none, result: DONE })).toBe("done");
    const withFailure = { ...DONE, steps: [...DONE.steps, { key: "x", status: "failed" as const, message: "no" }] };
    expect(companyWizardPhase({ ...none, result: withFailure })).toBe("failed");
  });

  it("shows failed for an empty result and for a request that threw", () => {
    expect(companyWizardPhase({ ...none, result: { steps: [], pipeline_id: null, unowned_stages: [] } })).toBe("failed");
    expect(companyWizardPhase({ ...none, errored: true })).toBe("failed");
  });
});

describe("OfficeCompanyWizard", () => {
  beforeEach(() => {
    api.applyCompanyTemplate.mockReset();
    toast.success.mockReset();
    toast.error.mockReset();
  });
  afterEach(cleanup);

  it("starts with every squad selected and the leave-paused estimate", () => {
    renderWizard();

    expect(screen.getAllByRole("checkbox")).toHaveLength(COMPANY_SQUAD_KEYS.length);
    expect(screen.getByRole("radio", { name: /Leave paused/ })).toHaveAttribute("aria-checked", "true");
    // Daily brief (7) and the market scan (1) run under leave_paused.
    expect(screen.getByText(/About 8 runs a week, about \$4\.00 a week\./)).toBeInTheDocument();
    expect(screen.getByText(/default of \$0\.50 per run/)).toBeInTheDocument();
  });

  it("names the stages that lose their owner when a squad is unticked", async () => {
    renderWizard();
    expect(screen.queryByText(/Stages without an owner/)).toBeNull();

    fireEvent.click(screen.getByRole("checkbox", { name: /Product Discovery/ }));

    expect(await screen.findByText(/Stages without an owner: Validate\./)).toBeInTheDocument();
  });

  it("raises the estimate when the start option runs every routine", () => {
    renderWizard();

    fireEvent.click(screen.getByRole("radio", { name: /Start all routines/ }));

    expect(screen.getByText(/About 34\.7 runs a week/)).toBeInTheDocument();
  });

  it("needs a runtime before it can build, then sends the chosen squads, runtime, model and start", async () => {
    api.applyCompanyTemplate.mockResolvedValue(DONE);
    renderWizard();
    const build = screen.getByRole("button", { name: "Build company" });
    expect(build).toBeDisabled();

    fireEvent.click(screen.getByRole("button", { name: "pick runtime" }));
    fireEvent.change(screen.getByLabelText("Model (optional)"), { target: { value: "gpt-x" } });
    fireEvent.click(screen.getByRole("radio", { name: /Start per floor/ }));
    expect(build).toBeEnabled();
    fireEvent.click(build);

    await waitFor(() => expect(api.applyCompanyTemplate).toHaveBeenCalledTimes(1));
    expect(api.applyCompanyTemplate).toHaveBeenCalledWith({
      runtime_id: "rt-1",
      model: "gpt-x",
      squads: [...COMPANY_SQUAD_KEYS],
      start: "per_floor",
    });
  });

  it("refuses to build with no squad selected", () => {
    renderWizard();
    fireEvent.click(screen.getByRole("button", { name: "pick runtime" }));
    fireEvent.click(screen.getByRole("button", { name: "Clear" }));

    expect(screen.getByText("Choose at least one squad.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Build company" })).toBeDisabled();
  });

  it("shows each step on partial failure and runs the same request again", async () => {
    const failed: CompanyTemplateApplyResult = {
      steps: [
        { key: "squad:build", status: "created", message: "Build created" },
        { key: "agent:SDLC Developer", status: "failed", message: "runtime offline" },
      ],
      pipeline_id: null,
      unowned_stages: [],
    };
    api.applyCompanyTemplate.mockResolvedValueOnce(failed).mockResolvedValueOnce(DONE);
    renderWizard();
    fireEvent.click(screen.getByRole("button", { name: "pick runtime" }));
    fireEvent.click(screen.getByRole("button", { name: "Build company" }));

    expect(await screen.findByText("Finished with problems")).toBeInTheDocument();
    expect(screen.getByText("Build created")).toBeInTheDocument();
    expect(screen.getByText("runtime offline")).toBeInTheDocument();
    expect(screen.getByText("Failed")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Run again" }));

    expect(await screen.findByText("Your company is set up")).toBeInTheDocument();
    expect(api.applyCompanyTemplate).toHaveBeenCalledTimes(2);
    expect(api.applyCompanyTemplate.mock.calls[1]![0]).toEqual(api.applyCompanyTemplate.mock.calls[0]![0]);
    expect(toast.success).toHaveBeenCalled();
  });

  it("says nothing is confirmed when the request throws", async () => {
    api.applyCompanyTemplate.mockRejectedValue(new Error("network down"));
    renderWizard();
    fireEvent.click(screen.getByRole("button", { name: "pick runtime" }));
    fireEvent.click(screen.getByRole("button", { name: "Build company" }));

    expect(await screen.findByText(/The company could not be built/)).toBeInTheDocument();
    expect(toast.error).toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Run again" })).toBeEnabled();
  });

  it("says nothing is confirmed when the answer has no steps", async () => {
    api.applyCompanyTemplate.mockResolvedValue({ steps: [], pipeline_id: null, unowned_stages: [] });
    renderWizard();
    fireEvent.click(screen.getByRole("button", { name: "pick runtime" }));
    fireEvent.click(screen.getByRole("button", { name: "Build company" }));

    expect(await screen.findByText(/could not be read, so nothing is confirmed/)).toBeInTheDocument();
  });

  it("marks a squad that already exists by name", () => {
    renderWizard([squad("Market Research")]);

    expect(screen.getByRole("checkbox", { name: /Market Research/ })).toBeInTheDocument();
    expect(screen.getAllByText("Already there", { selector: "span" })).toHaveLength(1);
  });
});
