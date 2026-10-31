import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Autopilot, AutopilotRun, AutopilotTrigger } from "@multica/core/types";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { renderWithI18n } from "../../test/i18n";

const update = vi.fn();
const runNow = vi.fn();
let detail: { autopilot: Autopilot; triggers: AutopilotTrigger[]; collaborators: [] };
let runs: AutopilotRun[];

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    autopilots: () => "/ws/autopilots",
    autopilotDetail: (id: string) => `/ws/autopilots/${id}`,
    issueDetail: (id: string) => `/ws/issues/${id}`,
    projectDetail: (id: string) => `/ws/projects/${id}`,
    agentDetail: (id: string) => `/ws/agents/${id}`,
  }),
}));
vi.mock("@multica/core/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/api")>();
  return { ...actual, api: { ...actual.api, getBaseUrl: () => "https://api.test" } };
});
vi.mock("@multica/core/autopilots/queries", () => ({
  autopilotDetailOptions: () => ({ queryKey: ["autopilot"], queryFn: async () => detail }),
  autopilotRunsOptions: () => ({ queryKey: ["runs"], queryFn: async () => runs }),
  autopilotRunOptions: () => ({ queryKey: ["run"], queryFn: async () => null }),
}));
vi.mock("@multica/core/autopilots/mutations", () => ({
  useUpdateAutopilot: () => ({ mutate: update }),
  useDeleteAutopilot: () => ({ mutateAsync: vi.fn() }),
  useCreateAutopilotTrigger: () => ({ mutateAsync: vi.fn() }),
  useDeleteAutopilotTrigger: () => ({ mutateAsync: vi.fn() }),
  useRotateAutopilotTriggerWebhookToken: () => ({ mutateAsync: vi.fn(), isPending: false }),
}));
vi.mock("@multica/core/projects/queries", () => ({
  projectDetailOptions: () => ({ queryKey: ["project"], queryFn: async () => null }),
  projectListOptions: () => ({ queryKey: ["projects"], queryFn: async () => [] }),
}));
vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"], queryFn: async () => [] }),
  agentListOptions: () => ({ queryKey: ["agents"], queryFn: async () => [] }),
  squadListOptions: () => ({ queryKey: ["squads"], queryFn: async () => [] }),
}));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: (_type: string, id: string) => (id === "agent-1" ? "Eve" : "Jiayuan") }),
}));
vi.mock("./use-run-autopilot-now", () => ({
  useRunAutopilotNow: () => ({ runNow, isPending: false }),
}));
vi.mock("./autopilot-dialog", () => ({
  AutopilotDialog: () => <div role="dialog">Edit autopilot dialog</div>,
}));
vi.mock("./edit-schedule-trigger-dialog", () => ({ EditScheduleTriggerDialog: () => null }));
vi.mock("./webhook-deliveries-section", () => ({ WebhookDeliveriesSection: () => null }));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => null }));
vi.mock("../../common/task-transcript", () => ({ TranscriptButton: () => null }));
vi.mock("../../search/register-context", () => ({ useRegisterCommandBarContext: () => {} }));
vi.mock("../../editor", () => ({
  ReadonlyContent: ({ content }: { content: string }) => <div>{content}</div>,
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

import { AutopilotDetailPage } from "./autopilot-detail-page";

function makeAutopilot(overrides: Partial<Autopilot> = {}): Autopilot {
  return {
    id: "ap-1",
    workspace_id: "ws",
    title: "Daily Sync",
    description: "Summarize what changed yesterday.",
    status: "active",
    assignee_type: "agent",
    assignee_id: "agent-1",
    execution_mode: "create_issue",
    project_id: null,
    created_by_type: "member",
    created_by_id: "member-1",
    subscribers: [],
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  } as unknown as Autopilot;
}

const scheduleTrigger = {
  id: "trg-1",
  autopilot_id: "ap-1",
  kind: "schedule",
  enabled: true,
  cron_expression: "0 9 * * 1-5",
  timezone: "Asia/Shanghai",
  next_run_at: "2126-07-14T01:00:00Z",
  webhook_token: null,
  label: null,
  last_fired_at: null,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
} as AutopilotTrigger;

function run(overrides: Partial<AutopilotRun>): AutopilotRun {
  return {
    id: "run",
    autopilot_id: "ap-1",
    source: "schedule",
    status: "issue_created",
    issue_id: null,
    task_id: null,
    failure_reason: null,
    triggered_at: "2026-10-30T01:00:00Z",
    completed_at: null,
    created_at: "2026-10-30T01:00:00Z",
    ...overrides,
  } as unknown as AutopilotRun;
}

// The panel's resize handle claims every pointerdown in jsdom, where all boxes
// measure 0×0 — so the menu trigger only sees a plain click.
function openMoreActions() {
  fireEvent.click(screen.getByRole("button", { name: "More actions" }));
}

function mount() {
  const adapter: NavigationAdapter = {
    pathname: "/ws/autopilots/ap-1",
    searchParams: new URLSearchParams(),
    hash: "",
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    getShareableUrl: (p) => p,
  };
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={client}>
      <NavigationProvider value={adapter}>
        <AutopilotDetailPage autopilotId="ap-1" />
      </NavigationProvider>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  update.mockReset();
  runNow.mockReset();
  detail = { autopilot: makeAutopilot(), triggers: [scheduleTrigger], collaborators: [] };
  runs = [
    run({ id: "r1", issue_id: "issue-24" }),
    run({ id: "r2", status: "failed", failure_reason: "Runtime is offline" }),
  ];
});

describe("AutopilotDetailPage", () => {
  it("puts the list in the breadcrumb and runs the autopilot from the one solid action", async () => {
    const user = userEvent.setup();
    mount();
    const header = await screen.findByRole("banner");
    await within(header).findByText("Daily Sync");

    expect(within(header).getByRole("link", { name: "Autopilots" })).toHaveAttribute("href", "/ws/autopilots");
    await user.click(within(header).getByRole("button", { name: "Run now" }));
    expect(runNow).toHaveBeenCalledWith("ap-1");
  });

  it("can't run a paused autopilot", async () => {
    detail.autopilot = makeAutopilot({ status: "paused" });
    mount();

    expect(await screen.findByRole("button", { name: "Run now" })).toBeDisabled();
  });

  it("keeps edit, copy link and delete in the ⋯ menu", async () => {
    const user = userEvent.setup();
    mount();
    await screen.findByRole("heading", { name: "Daily Sync", level: 1 });

    openMoreActions();
    expect(await screen.findByRole("menuitem", { name: "Copy link" })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "Delete autopilot" })).toBeInTheDocument();
    await user.click(screen.getByRole("menuitem", { name: "Edit" }));
    expect(await screen.findByRole("dialog")).toHaveTextContent("Edit autopilot dialog");
  });

  it("pauses from the panel's status switch", async () => {
    const user = userEvent.setup();
    mount();

    await user.click(await screen.findByRole("switch", { name: "Pause autopilot" }));
    expect(update).toHaveBeenCalledWith({ id: "ap-1", status: "paused" }, expect.anything());
  });

  it("lays out prompt, triggers and run history as sections", async () => {
    mount();
    const identity = await screen.findByRole("heading", { name: "Daily Sync", level: 1 });
    expect(identity.parentElement?.parentElement).toHaveTextContent("Eve creates an issue each run");

    const prompt = screen.getByRole("heading", { name: "Prompt", level: 2 }).closest("section")!;
    expect(within(prompt).getByText("Summarize what changed yesterday.")).toBeVisible();
    expect(within(prompt).getByRole("button", { name: "Edit" })).toBeVisible();

    const triggers = screen.getByRole("heading", { name: "Triggers", level: 2 }).closest("section")!;
    expect(within(triggers).getByText("1")).toBeVisible();
    expect(within(triggers).getByRole("button", { name: "Add trigger" })).toBeVisible();
    expect(within(triggers).getByText(/Asia\/Shanghai/)).toBeVisible();

    const history = screen.getByRole("heading", { name: "Run history", level: 2 }).closest("section")!;
    expect(await within(history).findByText("2")).toBeVisible();
    expect(within(history).getByRole("link", { name: /Issue linked/ })).toHaveAttribute("href", "/ws/issues/issue-24");
    expect(within(history).getByText("Runtime is offline")).toHaveClass("text-destructive");
  });

  it("shows a reader the page without write controls", async () => {
    detail.autopilot = makeAutopilot({ can_write: false } as Partial<Autopilot>);
    mount();
    await screen.findByRole("heading", { name: "Daily Sync", level: 1 });

    expect(screen.queryByRole("button", { name: "Run now" })).toBeNull();
    expect(screen.getByRole("switch", { name: "Pause autopilot" })).toHaveAttribute("aria-disabled", "true");
    expect(screen.queryByRole("button", { name: "Add trigger" })).toBeNull();
    openMoreActions();
    expect(await screen.findByRole("menuitem", { name: "Copy link" })).toBeInTheDocument();
    expect(screen.queryByRole("menuitem", { name: "Edit" })).toBeNull();
    expect(screen.queryByRole("menuitem", { name: "Delete autopilot" })).toBeNull();
  });
});
