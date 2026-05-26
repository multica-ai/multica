import type React from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { AgentRuntime } from "@multica/core/types/agent";

const mockCerebroRequest = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>(
    "@multica/core/api",
  );
  return {
    ...actual,
    api: { ...actual.api, cerebroRequest: mockCerebroRequest },
  };
});

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

import { SandboxProfileCard } from "./sandbox-profile-card";
import { fetchSandboxProfiles } from "../sandbox-profile-data";

const CATALOG = {
  profiles: [
    {
      id: "developer",
      label: "Developer",
      description: "Full developer access inside the sandbox.",
      selectable: true,
    },
    {
      id: "read_only",
      label: "Read-only",
      description: "Locked down. No shell or scripting access.",
      selectable: true,
    },
    {
      id: "custom",
      label: "Custom",
      description: "Hand-tuned rules that don't match a preset.",
      selectable: false,
    },
  ],
};

function makeRuntime(profile?: string): AgentRuntime {
  return {
    id: "rt-1",
    workspace_id: "ws-1",
    daemon_id: "daemon-1",
    name: "sara-mac-mini",
    runtime_mode: "local",
    provider: "claude",
    launch_header: "",
    status: "online",
    device_info: "host.local",
    metadata: {},
    owner_id: null,
    sandbox_enabled: null,
    sandbox_profile: profile,
    persona_sandbox: "",
    visibility: "private",
    timezone: "UTC",
    capabilities: {},
    last_seen_at: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

function renderWithClient(node: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{node}</QueryClientProvider>);
}

beforeEach(() => {
  mockCerebroRequest.mockReset();
});

describe("fetchSandboxProfiles (fail closed)", () => {
  it("returns the catalog on a well-formed response", async () => {
    mockCerebroRequest.mockResolvedValue(CATALOG);
    const out = await fetchSandboxProfiles("ws-1");
    expect(out.map((p) => p.id)).toEqual(["developer", "read_only", "custom"]);
  });

  it("falls back to an empty list when the response shape drifts", async () => {
    // A drifted/garbage payload must not crash the runtime page.
    mockCerebroRequest.mockResolvedValue({ unexpected: true });
    expect(await fetchSandboxProfiles("ws-1")).toEqual([]);
  });

  it("tolerates a null array without throwing", async () => {
    mockCerebroRequest.mockResolvedValue({ profiles: null });
    expect(await fetchSandboxProfiles("ws-1")).toEqual([]);
  });
});

describe("SandboxProfileCard", () => {
  it("renders the selectable presets and marks the current one", async () => {
    mockCerebroRequest.mockResolvedValue(CATALOG);

    renderWithClient(
      <SandboxProfileCard runtime={makeRuntime("read_only")} wsId="ws-1" canEdit />,
    );

    expect(await screen.findByText("Developer")).toBeInTheDocument();
    const readOnly = screen.getByRole("radio", { name: /Read-only/i });
    expect(readOnly).toHaveAttribute("aria-checked", "true");
    const developer = screen.getByRole("radio", { name: /Developer/i });
    expect(developer).toHaveAttribute("aria-checked", "false");
  });

  it("defaults the current profile to developer when the field is missing", async () => {
    mockCerebroRequest.mockResolvedValue(CATALOG);

    renderWithClient(
      <SandboxProfileCard runtime={makeRuntime(undefined)} wsId="ws-1" canEdit />,
    );

    const developer = await screen.findByRole("radio", { name: /Developer/i });
    expect(developer).toHaveAttribute("aria-checked", "true");
  });

  it("PATCHes the runtime sandbox-policy with the chosen profile on select", async () => {
    const user = userEvent.setup();
    mockCerebroRequest.mockResolvedValue(CATALOG);

    renderWithClient(
      <SandboxProfileCard runtime={makeRuntime("developer")} wsId="ws-1" canEdit />,
    );

    const readOnly = await screen.findByRole("radio", { name: /Read-only/i });
    await user.click(readOnly);

    await waitFor(() => {
      const patch = mockCerebroRequest.mock.calls.find(
        (c) =>
          typeof c[0] === "string" &&
          c[0] === "/api/runtimes/rt-1/sandbox-policy" &&
          (c[1] as RequestInit | undefined)?.method === "PATCH",
      );
      expect(patch).toBeTruthy();
      expect(JSON.parse((patch![1] as RequestInit).body as string)).toEqual({
        profile: "read_only",
      });
    });
  });

  it("shows the custom state when the runtime is on a hand-tuned policy", async () => {
    mockCerebroRequest.mockResolvedValue(CATALOG);

    renderWithClient(
      <SandboxProfileCard runtime={makeRuntime("custom")} wsId="ws-1" canEdit />,
    );

    expect(await screen.findByText(/Hand-tuned rules/i)).toBeInTheDocument();
  });

  it("disables selection when the viewer cannot edit", async () => {
    mockCerebroRequest.mockResolvedValue(CATALOG);

    renderWithClient(
      <SandboxProfileCard
        runtime={makeRuntime("developer")}
        wsId="ws-1"
        canEdit={false}
      />,
    );

    const readOnly = await screen.findByRole("radio", { name: /Read-only/i });
    expect(readOnly).toBeDisabled();
  });
});
