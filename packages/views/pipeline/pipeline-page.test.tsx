/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Initiative, Pipeline } from "@multica/core/pipeline";
import type { Squad } from "@multica/core/types";
import { renderWithI18n } from "../test/i18n";
import { PipelinePage } from "./pipeline-page";

const api = vi.hoisted(() => ({
  listPipelines: vi.fn(),
  listInitiatives: vi.fn(),
  listSquads: vi.fn(),
  getInitiative: vi.fn(),
  listSquadMembers: vi.fn(),
  createInitiative: vi.fn(),
}));
const role = vi.hoisted(() => ({ value: "admin" as "owner" | "admin" | "member" }));
const nav = vi.hoisted(() => ({
  pathname: "/acme/pipeline",
  searchParams: new URLSearchParams(),
  replace: vi.fn(),
  push: vi.fn(),
}));

vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api,
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/permissions", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/permissions")>()),
  useCurrentMember: () => ({ userId: "u-1", role: role.value, member: null, isLoading: false }),
}));
// The panel embeds the backing issue's thread; these tests are about the board.
vi.mock("../issues/components/issue-detail", () => ({
  IssueDetail: () => <div data-testid="issue-detail" />,
}));
vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useWorkspacePaths: () => ({
    issueDetail: (id: string) => `/acme/issues/${id}`,
    knowledge: () => "/acme/knowledge",
  }),
}));
vi.mock("@multica/core/workspace/hooks", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/workspace/hooks")>()),
  useActorName: () => ({ getActorName: (type: string, id: string) => `${type} ${id}` }),
}));
vi.mock("../navigation", () => ({
  AppLink: ({ children, ...props }: React.ComponentProps<"a">) => <a {...props}>{children}</a>,
  useNavigation: () => nav,
}));

const PIPELINE: Pipeline = {
  id: "pl-1",
  name: "Default",
  is_default: true,
  stages: [
    {
      key: "validate",
      name: "Validate",
      owner_squad_id: "sq-research",
      objective: "Decide with evidence.",
      instructions: "",
      artifacts: [{ kind: "validation_report", required: true }],
      exit: { mode: "gate", criteria: "A go or no-go." },
      sla_days: 14,
    },
    {
      key: "define",
      name: "Define",
      owner_squad_id: null,
      objective: "",
      instructions: "",
      artifacts: [],
      exit: { mode: "attest", criteria: "" },
      sla_days: null,
    },
  ],
};

const SQUADS = [{ id: "sq-research", name: "Research", archived_at: null }] as Squad[];

function initiative(id: string, extra: Partial<Initiative> = {}): Initiative {
  return {
    id,
    identifier: `INI-${id}`,
    title: `Initiative ${id}`,
    thesis: "",
    pipeline_id: "pl-1",
    stage_key: "validate",
    owner_squad_id: "sq-research",
    status: "active",
    issue_id: `iss-${id}`,
    build_project_id: null,
    stage_entered_at: "2026-10-20T00:00:00Z",
    days_in_stage: 13,
    sla_state: "ok",
    artifacts_required: 3,
    artifacts_present: 1,
    pending_gate_proposal_id: null,
    ...extra,
  };
}

function renderPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  renderWithI18n(
    <QueryClientProvider client={queryClient}>
      <PipelinePage />
    </QueryClientProvider>,
  );
}

describe("PipelinePage", () => {
  beforeEach(() => {
    role.value = "admin";
    nav.searchParams = new URLSearchParams();
    nav.replace.mockReset();
    api.listSquadMembers.mockResolvedValue([]);
    api.listPipelines.mockResolvedValue([PIPELINE]);
    api.listSquads.mockResolvedValue(SQUADS);
    api.listInitiatives.mockResolvedValue([
      initiative("1", { title: "Invoicing for small firms", sla_state: "stalled" }),
      initiative("2", { title: "Team wiki", stage_key: "define", owner_squad_id: null, days_in_stage: 2, artifacts_required: 2, artifacts_present: 2 }),
    ]);
  });
  afterEach(cleanup);

  it("draws one column per stage with a card for each initiative", async () => {
    renderPage();

    expect(await screen.findByRole("heading", { name: "Validate" })).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Define" })).toBeTruthy();

    const first = await screen.findByRole("button", { name: /Invoicing for small firms/ });
    expect(within(first).getByText("INI-1")).toBeTruthy();
    expect(within(first).getByText("Research")).toBeTruthy();
    expect(within(first).getByText("13 days in stage")).toBeTruthy();
    expect(within(first).getByText("1/3 artifacts")).toBeTruthy();
    expect(within(first).getByText("Stalled")).toBeTruthy();

    const second = screen.getByRole("button", { name: /Team wiki/ });
    expect(within(second).getByText("No owner")).toBeTruthy();
    expect(within(second).queryByText("Stalled")).toBeNull();
  });

  it("does not offer drag and drop on cards", async () => {
    renderPage();
    const card = await screen.findByRole("button", { name: /Invoicing for small firms/ });
    expect(card.getAttribute("draggable")).toBeNull();
  });

  it("loads the default pipeline's initiatives, with the owner squad and status from the URL", async () => {
    nav.searchParams = new URLSearchParams("squad=sq-research&status=shelved");
    api.listInitiatives.mockResolvedValue([initiative("9", { title: "Parked idea", status: "shelved" })]);
    renderPage();

    await waitFor(() =>
      expect(api.listInitiatives).toHaveBeenCalledWith({
        pipeline_id: "pl-1",
        owner_squad_id: "sq-research",
        status: "shelved",
      }),
    );
    expect(await screen.findByRole("button", { name: /Parked idea/ })).toBeTruthy();
  });

  it("hides shelved and closed work under Open, the default", async () => {
    api.listInitiatives.mockResolvedValue([
      initiative("1", { title: "Running work" }),
      initiative("2", { title: "Parked work", status: "shelved" }),
      initiative("3", { title: "Finished work", status: "closed" }),
    ]);
    renderPage();

    expect(await screen.findByRole("button", { name: /Running work/ })).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Parked work/ })).toBeNull();
    expect(screen.queryByRole("button", { name: /Finished work/ })).toBeNull();
    expect(api.listInitiatives).toHaveBeenCalledWith({ pipeline_id: "pl-1" });
  });

  it("offers New initiative to an owner or admin only", async () => {
    renderPage();
    expect(await screen.findByRole("button", { name: "New initiative" })).toBeTruthy();
    cleanup();

    role.value = "member";
    renderPage();
    expect(await screen.findByRole("button", { name: /Invoicing for small firms/ })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "New initiative" })).toBeNull();
  });

  it("opens the create dialog from New initiative", async () => {
    renderPage();
    fireEvent.click(await screen.findByRole("button", { name: "New initiative" }));
    expect(await screen.findByRole("dialog", { name: "New initiative" })).toBeTruthy();
  });

  it("says so when no initiative matches the filters", async () => {
    api.listInitiatives.mockResolvedValue([]);
    renderPage();

    expect(await screen.findByText("No initiatives match these filters.")).toBeTruthy();
  });
});
