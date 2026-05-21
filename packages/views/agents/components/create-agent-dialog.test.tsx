// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, fireEvent, cleanup } from "@testing-library/react";
import type { Agent, MemberWithUser, RuntimeDevice } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import enCommon from "../../locales/en/common.json";
import enAgents from "../../locales/en/agents.json";

const navigationStub: NavigationAdapter = {
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/",
  searchParams: new URLSearchParams(),
  getShareableUrl: (path: string) => path,
};

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

// ModelDropdown talks to the api; the create dialog only needs it as a
// stand-in here, so swap it out.
vi.mock("./model-dropdown", () => ({
  ModelDropdown: () => null,
}));

// Provider logos don't matter for these assertions but they pull in SVGs.
vi.mock("../../runtimes/components/provider-logo", () => ({
  ProviderLogo: () => null,
}));

// Avatars hit the api for member metadata.
vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => null,
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

import { CreateAgentDialog } from "./create-agent-dialog";

const ME = "user-me";
const OTHER = "user-other";

const members: MemberWithUser[] = [
  {
    id: "m-me",
    user_id: ME,
    workspace_id: "ws-1",
    role: "member",
    name: "Me",
    email: "me@example.com",
    avatar_url: null,
    created_at: "2026-01-01T00:00:00Z",
  },
  {
    id: "m-other",
    user_id: OTHER,
    workspace_id: "ws-1",
    role: "member",
    name: "Other",
    email: "other@example.com",
    avatar_url: null,
    created_at: "2026-01-01T00:00:00Z",
  },
];

function makeRuntime(overrides: Partial<RuntimeDevice>): RuntimeDevice {
  return {
    id: "rt",
    workspace_id: "ws-1",
    daemon_id: null,
    name: "Test Runtime",
    runtime_mode: "local",
    provider: "claude",
    launch_header: "",
    status: "online",
    device_info: "host.local",
    metadata: {},
    owner_id: ME,
    visibility: "private",
    timezone: "UTC",
    last_seen_at: "2026-04-27T11:59:50Z",
    created_at: "2026-04-01T00:00:00Z",
    updated_at: "2026-04-01T00:00:00Z",
    ...overrides,
  };
}

function makeTemplate(runtimeId: string): Agent {
  return {
    id: "agent-template",
    workspace_id: "ws-1",
    runtime_id: runtimeId,
    name: "Template Agent",
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
    owner_id: ME,
    skills: [],
    created_at: "2026-04-01T00:00:00Z",
    updated_at: "2026-04-01T00:00:00Z",
    archived_at: null,
    archived_by: null,
  };
}

function renderDialog(runtimes: RuntimeDevice[], template?: Agent) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const onCreate = vi.fn().mockResolvedValue(undefined);
  const onClose = vi.fn();
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={queryClient}>
        <WorkspaceSlugProvider slug="test-ws">
        <NavigationProvider value={navigationStub}>
          <CreateAgentDialog
            runtimes={runtimes}
            members={members}
            currentUserId={ME}
            template={template}
            onClose={onClose}
            onCreate={onCreate}
          />
        </NavigationProvider>
        </WorkspaceSlugProvider>
      </QueryClientProvider>
    </I18nProvider>,
  );
  return { onCreate, onClose };
}

describe("CreateAgentDialog runtime visibility gate", () => {
  beforeEach(() => vi.clearAllMocks());
  // Base UI Dialog renders into a portal on document.body and leaves
  // focus-guard / inert wrapper divs around after the React tree unmounts.
  // The auto-cleanup from @testing-library/react drops the container but
  // not the portal residue, so two-tests-in-a-row queries see double
  // matches ("All", "My Runtime"). Force cleanup + wipe body between tests.
  afterEach(() => {
    cleanup();
    document.body.innerHTML = "";
  });

  it("disables another member's private runtime in the picker", () => {
    const mine = makeRuntime({ id: "rt-mine", name: "My Runtime", owner_id: ME, visibility: "private" });
    const othersPrivate = makeRuntime({
      id: "rt-others-private",
      name: "Others Private",
      owner_id: OTHER,
      visibility: "private",
    });
    renderDialog([mine, othersPrivate]);

    // Flip to "All" so other-owned runtimes show.
    fireEvent.click(screen.getByText("All"));
    // Open the picker.
    fireEvent.click(
      screen.getByText("My Runtime", { selector: "span.truncate" }),
    );

    const disabledRow = screen
      .getByText("Others Private")
      .closest("button") as HTMLButtonElement;
    expect(disabledRow).not.toBeNull();
    expect(disabledRow.disabled).toBe(true);
    expect(disabledRow.title).toMatch(/Private runtime/i);
  });

  it("lets a plain member pick another member's public runtime", () => {
    const mine = makeRuntime({ id: "rt-mine", name: "My Runtime", owner_id: ME, visibility: "private" });
    const othersPublic = makeRuntime({
      id: "rt-others-public",
      name: "Others Public",
      owner_id: OTHER,
      visibility: "public",
    });
    renderDialog([mine, othersPublic]);

    fireEvent.click(screen.getByText("All"));
    fireEvent.click(
      screen.getByText("My Runtime", { selector: "span.truncate" }),
    );

    const publicRow = screen
      .getByText("Others Public")
      .closest("button") as HTMLButtonElement;
    expect(publicRow).not.toBeNull();
    expect(publicRow.disabled).toBe(false);
  });

  it("defaults the selected runtime to a usable one, not a locked private", () => {
    const othersPrivate = makeRuntime({
      id: "rt-others-private",
      name: "Others Private",
      owner_id: OTHER,
      visibility: "private",
    });
    const mine = makeRuntime({
      id: "rt-mine",
      name: "My Runtime",
      owner_id: ME,
      visibility: "private",
    });
    renderDialog([othersPrivate, mine]);

    // The trigger label shows the selected runtime name. The picker must
    // not seed with the other-owned private runtime even if it sorted
    // first in the input list.
    expect(screen.queryByText("Others Private", { selector: "span.truncate" })).toBeNull();
    expect(screen.getByText("My Runtime", { selector: "span.truncate" })).toBeInTheDocument();
  });

  it("in duplicate mode, does not pre-fill the template's runtime when it's now locked", async () => {
    // Template runtime is owned by someone else and now private — the
    // duplicate flow used to seed with it anyway, leaving the user with
    // a Create button that 403s server-side. Now we fall back to the
    // first usable runtime instead.
    const othersPrivate = makeRuntime({
      id: "rt-others-private",
      name: "Others Private",
      owner_id: OTHER,
      visibility: "private",
    });
    const mine = makeRuntime({
      id: "rt-mine",
      name: "My Runtime",
      owner_id: ME,
      visibility: "private",
    });
    const template = makeTemplate("rt-others-private");
    const { onCreate } = renderDialog([othersPrivate, mine], template);

    expect(
      screen.getByText("My Runtime", { selector: "span.truncate" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByText("Others Private", { selector: "span.truncate" }),
    ).toBeNull();

    // Sanity check: with a usable selection seeded, Create should submit.
    fireEvent.click(screen.getByText("Create"));
    await new Promise((r) => setTimeout(r, 0));
    expect(onCreate).toHaveBeenCalledTimes(1);
    expect(onCreate.mock.calls[0]?.[0].runtime_id).toBe("rt-mine");
  });

  it("disables Create when the selected runtime is locked (template + no usable fallback)", () => {
    // Edge case: template points at a locked runtime AND the workspace
    // has no usable alternatives in scope. The defense-in-depth gate on
    // the Create button must keep the user from submitting a 403.
    const onlyOthersPrivate = makeRuntime({
      id: "rt-only-others-private",
      name: "Only Others Private",
      owner_id: OTHER,
      visibility: "private",
    });
    // Flip the picker to "All" so the locked runtime is at least
    // visible — that's the scope where the selected-but-locked state
    // can persist after the initial seed search returns nothing.
    const template = makeTemplate("rt-only-others-private");
    renderDialog([onlyOthersPrivate], template);

    // The Create button is rendered by lucide-free CTA text "Create".
    const createBtn = screen
      .getAllByRole("button")
      .find((b) => b.textContent === "Create");
    expect(createBtn).toBeDefined();
    expect((createBtn as HTMLButtonElement).disabled).toBe(true);
  });
});
