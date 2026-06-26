// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, fireEvent, cleanup } from "@testing-library/react";
import type {
  Agent,
  MemberWithUser,
  RuntimeDevice,
  RuntimeModel,
} from "@multica/core/types";
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
// stand-in here, so swap it out. The Reasoning picker, however, IS under
// test, so we mock the runtime-models query layer it depends on with a
// controllable catalog instead of mocking the component itself.
vi.mock("./model-dropdown", () => ({
  ModelDropdown: () => null,
}));

// Per-runtime model catalogs so a runtime/provider switch resolves a
// different reasoning vocabulary, exercising the stale-token clearing.
const modelsByRuntime: Record<
  string,
  { models: RuntimeModel[]; supported: boolean }
> = {};

vi.mock("@multica/core/runtimes", () => ({
  runtimeModelsOptions: (runtimeId: string | null | undefined) => ({
    queryKey: ["runtime-models", runtimeId ?? "none"],
    queryFn: async () =>
      modelsByRuntime[runtimeId ?? ""] ?? { models: [], supported: true },
    enabled: Boolean(runtimeId),
    retry: false,
  }),
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
  toast: { error: vi.fn(), success: vi.fn(), warning: vi.fn() },
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
    name: "Claude (host.local)",
    runtime_mode: "local",
    provider: "claude",
    launch_header: "claude (stream-json)",
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

function makeTemplate(overrides: Partial<Agent>): Agent {
  return {
    id: "agent-template",
    workspace_id: "ws-1",
    runtime_id: "rt",
    name: "Template Agent",
    description: "",
    instructions: "",
    avatar_url: null,
    runtime_mode: "local",
    runtime_config: {},
    custom_args: [],
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
    ...overrides,
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

const tick = () => new Promise((r) => setTimeout(r, 0));

// A name is required before Create will submit; manual-create starts blank.
function typeName(value = "My Agent") {
  fireEvent.change(screen.getByPlaceholderText("e.g. Deep Research Agent"), {
    target: { value },
  });
}

const openMachinePicker = () =>
  fireEvent.click(screen.getByTestId("machine-picker-trigger"));
const openAgentRuntimePicker = () =>
  fireEvent.click(screen.getByTestId("agent-runtime-trigger"));

afterEach(() => {
  cleanup();
  document.body.innerHTML = "";
  for (const key of Object.keys(modelsByRuntime)) delete modelsByRuntime[key];
});

describe("CreateAgentDialog machine + agent-runtime cascade (MUL-3772)", () => {
  beforeEach(() => vi.clearAllMocks());

  it("groups two CLIs on one daemon into a machine and cascades the runtime picker", async () => {
    const claude = makeRuntime({
      id: "rt-claude",
      daemon_id: "d1",
      name: "Claude (Workstation)",
      device_info: "Workstation",
      provider: "claude",
    });
    const codex = makeRuntime({
      id: "rt-codex",
      daemon_id: "d1",
      name: "Codex (Workstation)",
      device_info: "Workstation",
      provider: "codex",
      launch_header: "codex app-server",
    });
    const { onCreate } = renderDialog([claude, codex]);

    // Machine box shows the host label; agent-runtime box seeds to the
    // provider-sorted first runtime (claude < codex).
    expect(
      screen.getByText("Workstation", { selector: "span.truncate" }),
    ).toBeInTheDocument();
    expect(
      screen.getByText("Claude", { selector: "span.truncate" }),
    ).toBeInTheDocument();

    // Cascade: open the Agent runtime picker and switch to Codex.
    openAgentRuntimePicker();
    fireEvent.click(screen.getByText("Codex", { selector: "span.truncate" }));

    typeName();
    fireEvent.click(screen.getByText("Create"));
    await tick();
    expect(onCreate).toHaveBeenCalledTimes(1);
    expect(onCreate.mock.calls[0]?.[0].runtime_id).toBe("rt-codex");
  });

  it("renders the agent-runtime selector read-only for a single-runtime machine", () => {
    const only = makeRuntime({
      id: "rt-solo",
      daemon_id: "d-solo",
      name: "Claude (Solo)",
      device_info: "Solo",
    });
    renderDialog([only]);

    // The agent-runtime title is present but it is NOT inside a button
    // (no popover trigger) — single-runtime machines have nothing to pick.
    const title = screen.getByText("Claude", { selector: "span.truncate" });
    expect(title.closest("button")).toBeNull();
  });

  const mineAndOthersPrivate = (): RuntimeDevice[] => [
    makeRuntime({
      id: "rt-mine",
      daemon_id: "d-mine",
      name: "Claude (Mine)",
      device_info: "Mine",
      owner_id: ME,
      visibility: "private",
    }),
    makeRuntime({
      id: "rt-others",
      daemon_id: "d-other",
      name: "Claude (Theirs)",
      device_info: "Theirs",
      owner_id: OTHER,
      visibility: "private",
    }),
  ];

  it("filters another member's machine out of the picker under Mine", () => {
    renderDialog(mineAndOthersPrivate());
    openMachinePicker();
    expect(screen.queryByText("Theirs")).toBeNull();
  });

  it("shows another member's private machine locked under All", () => {
    renderDialog(mineAndOthersPrivate());
    // hasOtherMachines surfaces the Mine/All toggle; flip to All before
    // opening so the other-owned machine is in scope.
    fireEvent.click(screen.getByText("All"));
    openMachinePicker();

    const lockedRow = screen.getByText("Theirs").closest("button");
    expect(lockedRow).not.toBeNull();
    expect((lockedRow as HTMLButtonElement).disabled).toBe(true);
    expect((lockedRow as HTMLButtonElement).title).toMatch(/Private runtime/i);
  });

  it("seeds to a usable machine, not a locked private one that sorts first", () => {
    const othersPrivate = makeRuntime({
      id: "rt-others",
      daemon_id: "d-other",
      name: "Claude (Theirs)",
      device_info: "Theirs",
      owner_id: OTHER,
      visibility: "private",
    });
    const mine = makeRuntime({
      id: "rt-mine",
      daemon_id: "d-mine",
      name: "Claude (Mine)",
      device_info: "Mine",
      owner_id: ME,
      visibility: "private",
    });
    renderDialog([othersPrivate, mine]);

    expect(
      screen.getByText("Mine", { selector: "span.truncate" }),
    ).toBeInTheDocument();
    expect(screen.queryByText("Theirs", { selector: "span.truncate" })).toBeNull();
  });

  it("treats a cloud runtime (no daemon) as its own machine with a Cloud badge", () => {
    const cloud = makeRuntime({
      id: "rt-cloud",
      daemon_id: null,
      runtime_mode: "cloud",
      name: "Codex cloud",
      device_info: "Cloud · us-west",
      provider: "codex",
      owner_id: null,
      visibility: "public",
    });
    renderDialog([cloud]);

    // A workspace cloud runtime is owned by nobody, so it lives under "All",
    // not "Mine" — flip the filter to bring it into scope.
    fireEvent.click(screen.getByText("All"));

    expect(
      screen.getByText("Cloud · us-west", { selector: "span.truncate" }),
    ).toBeInTheDocument();
    expect(
      screen.getByText("Codex cloud", { selector: "span.truncate" }),
    ).toBeInTheDocument();
    // Cloud badge is rendered in the machine trigger.
    expect(screen.getAllByText("Cloud").length).toBeGreaterThan(0);
  });
});

describe("CreateAgentDialog Create gate (MUL-3772)", () => {
  beforeEach(() => vi.clearAllMocks());

  it("in duplicate mode, falls back off a now-locked template runtime", async () => {
    const othersPrivate = makeRuntime({
      id: "rt-others",
      daemon_id: "d-other",
      name: "Claude (Theirs)",
      device_info: "Theirs",
      owner_id: OTHER,
      visibility: "private",
    });
    const mine = makeRuntime({
      id: "rt-mine",
      daemon_id: "d-mine",
      name: "Claude (Mine)",
      device_info: "Mine",
      owner_id: ME,
      visibility: "private",
    });
    const template = makeTemplate({ runtime_id: "rt-others" });
    const { onCreate } = renderDialog([othersPrivate, mine], template);

    expect(
      screen.getByText("Mine", { selector: "span.truncate" }),
    ).toBeInTheDocument();

    fireEvent.click(screen.getByText("Create"));
    await tick();
    expect(onCreate).toHaveBeenCalledTimes(1);
    expect(onCreate.mock.calls[0]?.[0].runtime_id).toBe("rt-mine");
  });

  it("disables Create when the only runtime is locked", () => {
    const onlyOthersPrivate = makeRuntime({
      id: "rt-locked",
      daemon_id: "d-other",
      name: "Claude (Theirs)",
      device_info: "Theirs",
      owner_id: OTHER,
      visibility: "private",
    });
    const template = makeTemplate({ runtime_id: "rt-locked" });
    renderDialog([onlyOthersPrivate], template);

    const createBtn = screen
      .getAllByRole("button")
      .find((b) => b.textContent === "Create");
    expect(createBtn).toBeDefined();
    expect((createBtn as HTMLButtonElement).disabled).toBe(true);
  });
});

function reasoningModel(
  id: string,
  levels: { value: string; label: string }[],
): RuntimeModel {
  return { id, label: id, default: true, thinking: { supported_levels: levels } };
}

describe("CreateAgentDialog reasoning picker (MUL-3772 REQ-2)", () => {
  beforeEach(() => vi.clearAllMocks());

  it("hides the reasoning row when the model exposes no levels", async () => {
    modelsByRuntime["rt-1"] = {
      models: [{ id: "haiku", label: "Haiku", default: true }],
      supported: true,
    };
    renderDialog([makeRuntime({ id: "rt-1", daemon_id: "d1" })]);
    await tick();
    expect(screen.queryByText("Reasoning")).toBeNull();
  });

  it("shows the row for a reasoning-capable model and submits the chosen level", async () => {
    modelsByRuntime["rt-1"] = {
      models: [
        reasoningModel("opus", [
          { value: "low", label: "Low" },
          { value: "high", label: "High" },
        ]),
      ],
      supported: true,
    };
    const { onCreate } = renderDialog([
      makeRuntime({ id: "rt-1", daemon_id: "d1" }),
    ]);

    // Row appears once the catalog query settles.
    await screen.findByText("Reasoning");

    // Open the reasoning popover (trigger shows "Follow CLI config") and
    // pick "High".
    fireEvent.click(screen.getByText("Follow CLI config"));
    fireEvent.click(screen.getByText("High"));

    typeName();
    fireEvent.click(screen.getByText("Create"));
    await tick();
    expect(onCreate).toHaveBeenCalledTimes(1);
    expect(onCreate.mock.calls[0]?.[0].thinking_level).toBe("high");
  });

  it("omits thinking_level when left on follow-CLI-config", async () => {
    modelsByRuntime["rt-1"] = {
      models: [reasoningModel("opus", [{ value: "high", label: "High" }])],
      supported: true,
    };
    const { onCreate } = renderDialog([
      makeRuntime({ id: "rt-1", daemon_id: "d1" }),
    ]);
    await screen.findByText("Reasoning");

    typeName();
    fireEvent.click(screen.getByText("Create"));
    await tick();
    expect(onCreate).toHaveBeenCalledTimes(1);
    expect(onCreate.mock.calls[0]?.[0].thinking_level).toBeUndefined();
  });

  it("clears a stale level when switching the agent runtime to another provider", async () => {
    // Two CLIs on one machine with disjoint reasoning vocabularies. Picking a
    // Claude-only level then switching to the Codex runtime must not carry the
    // now provider-invalid token into the payload (the backend 400s on it).
    modelsByRuntime["rt-claude"] = {
      models: [
        reasoningModel("opus", [
          { value: "high", label: "High" },
          { value: "max", label: "Max" },
        ]),
      ],
      supported: true,
    };
    modelsByRuntime["rt-codex"] = {
      models: [
        reasoningModel("gpt5", [
          { value: "none", label: "None" },
          { value: "low", label: "Low" },
        ]),
      ],
      supported: true,
    };
    const { onCreate } = renderDialog([
      makeRuntime({
        id: "rt-claude",
        daemon_id: "d1",
        provider: "claude",
        name: "Claude (Mac)",
        device_info: "Mac",
      }),
      makeRuntime({
        id: "rt-codex",
        daemon_id: "d1",
        provider: "codex",
        name: "Codex (Mac)",
        device_info: "Mac",
        launch_header: "codex app-server",
      }),
    ]);

    // Seeds to Claude (provider-sorted); pick the Claude-only "Max".
    await screen.findByText("Reasoning");
    fireEvent.click(screen.getByText("Follow CLI config"));
    fireEvent.click(screen.getByText("Max"));

    // Switch the agent runtime to Codex — a different provider.
    openAgentRuntimePicker();
    fireEvent.click(screen.getByText("Codex", { selector: "span.truncate" }));
    await tick();

    // The stale "Max" is gone; the row reseeds to follow-CLI-config and the
    // payload omits the token entirely.
    expect(screen.queryByText("Max")).toBeNull();
    typeName();
    fireEvent.click(screen.getByText("Create"));
    await tick();
    expect(onCreate).toHaveBeenCalledTimes(1);
    expect(onCreate.mock.calls[0]?.[0].runtime_id).toBe("rt-codex");
    expect(onCreate.mock.calls[0]?.[0].thinking_level).toBeUndefined();
  });

  it("drops a duplicate-mode orphan level the current model does not advertise", async () => {
    // Duplicate clones thinking_level "high", but the target runtime's model
    // has no reasoning catalog — the orphan must not be submittable.
    modelsByRuntime["rt-1"] = {
      models: [{ id: "haiku", label: "Haiku", default: true }],
      supported: true,
    };
    const template = makeTemplate({
      runtime_id: "rt-1",
      thinking_level: "high",
    });
    const { onCreate } = renderDialog(
      [makeRuntime({ id: "rt-1", daemon_id: "d1" })],
      template,
    );

    // Once the catalog loads, the orphan clears and the row disappears.
    await vi.waitFor(() =>
      expect(screen.queryByText("Reasoning")).toBeNull(),
    );

    fireEvent.click(screen.getByText("Create"));
    await tick();
    expect(onCreate).toHaveBeenCalledTimes(1);
    expect(onCreate.mock.calls[0]?.[0].thinking_level).toBeUndefined();
  });
});
