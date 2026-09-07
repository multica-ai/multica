// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import type { AgentRuntime } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enAgents from "../../locales/en/agents.json";
import enRuntimes from "../../locales/en/runtimes.json";
import { pendingManagedRuntimeFromSetup } from "./managed-runtime-setup";

const queryResults = vi.hoisted(() => new Map<string, unknown>());

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<
    typeof import("@tanstack/react-query")
  >();
  return {
    ...actual,
    useQuery: (options: { queryKey?: unknown }) => {
      const key = JSON.stringify(options?.queryKey);
      return queryResults.has(key)
        ? { data: queryResults.get(key), isSuccess: true }
        : { data: [], isSuccess: false };
    },
  };
});
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: { user: { id: string } }) => unknown) =>
    selector({ user: { id: "user-1" } }),
}));
vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ runtimeDetail: () => "/runtime" }),
}));
vi.mock("../../navigation", () => ({
  useRowLink: () => () => ({}),
  useIntentNavigate: () => () => {},
}));
vi.mock("./provider-logo", () => ({ ProviderLogo: () => null }));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => null }));

import { RuntimeList } from "./runtime-list";

const resources = {
  en: { agents: enAgents, runtimes: enRuntimes },
};

const piRuntime: AgentRuntime = {
  id: "runtime-pi",
  workspace_id: "ws-1",
  daemon_id: "daemon-1",
  name: "Pi (MacBook)",
  runtime_mode: "local",
  provider: "pi",
  launch_header: "pi",
  status: "online",
  device_info: "MacBook",
  metadata: {},
  default_model_config: {},
  has_default_model_api_key: false,
  owner_id: null,
  visibility: "private",
  profile_id: null,
  last_seen_at: "2026-08-06T08:00:00Z",
  created_at: "2026-08-06T08:00:00Z",
  updated_at: "2026-08-06T08:00:00Z",
};

function renderRuntimeList(runtime: AgentRuntime) {
  render(
    <I18nProvider locale="en" resources={resources}>
      <RuntimeList runtimes={[runtime]} now={Date.parse("2026-08-06T08:00:00Z")} />
    </I18nProvider>,
  );
}

function seedRuntimeModels(
  runtimeId: string,
  models: Array<{ id: string; label: string }>,
) {
  queryResults.set(JSON.stringify(["runtimes", "models", runtimeId]), {
    models,
    supported: true,
    cached: false,
  });
}

describe("managed runtime setup row", () => {
  beforeEach(() => {
    queryResults.clear();
  });

  it.each([
    ["installing", "Installing..."],
    ["ready", "Ready · waiting for daemon"],
    ["failed", "Installation failed"],
  ] as const)(
    "renders Pi with the %s status without making the row interactive",
    (phase, label) => {
      const runtime = pendingManagedRuntimeFromSetup({
        setup: {
          provider: "pi",
          phase,
          startedAt: "2026-08-04T00:00:00Z",
        },
        workspaceId: "ws-1",
        localMachineName: "MacBook",
      });

      renderRuntimeList(runtime);

      expect(screen.getByText("Pi")).toBeInTheDocument();
      expect(screen.getByText(label)).toBeInTheDocument();
      expect(screen.queryByRole("link")).not.toBeInTheDocument();
    },
  );

  it("shows Pi as online when local model discovery finds models", () => {
    seedRuntimeModels("runtime-pi", [
      { id: "deepseek/deepseek-v4-pro", label: "deepseek/deepseek-v4-pro" },
    ]);

    renderRuntimeList(piRuntime);

    expect(screen.getByText("Online")).toBeInTheDocument();
    expect(screen.queryByText("Needs model setup")).not.toBeInTheDocument();
  });

  it("asks for model setup only after Pi local model discovery is empty", () => {
    seedRuntimeModels("runtime-pi", []);

    renderRuntimeList(piRuntime);

    expect(screen.getByText("Needs model setup")).toBeInTheDocument();
    expect(screen.queryByText("Online")).not.toBeInTheDocument();
  });
});
