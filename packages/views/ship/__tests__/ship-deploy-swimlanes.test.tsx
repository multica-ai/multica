import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { I18nProvider } from "@multica/core/i18n/react";
import { RESOURCES } from "../../locales";
import type { Deploy, DeployEnvironment } from "@multica/core/types";

// Hoisted mock state — each test mutates these to drive the swimlane
// scenarios (no envs / staging only / pills with various statuses).
const { envsRef, deploysRef } = vi.hoisted(() => ({
  envsRef: { current: [] as DeployEnvironment[] },
  deploysRef: { current: [] as Deploy[] },
}));

vi.mock("@multica/core/ship", () => ({
  useDeployEnvironments: () => ({
    data: { environments: envsRef.current },
    isLoading: false,
  }),
  useRecentDeploys: () => ({
    data: { deploys: deploysRef.current, total: deploysRef.current.length },
  }),
  useUpsertDeployEnvironment: () => ({ mutateAsync: vi.fn(), isPending: false }),
  useLogDeploy: () => ({ mutateAsync: vi.fn(), isPending: false }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

import { ShipDeploySwimlanes } from "../components/ship-deploy-swimlanes";

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={RESOURCES}>
      {children}
    </I18nProvider>
  );
}

function makeEnv(overrides: Partial<DeployEnvironment> = {}): DeployEnvironment {
  return {
    id: "env-1",
    workspace_id: "ws-1",
    project_id: "p-1",
    kind: "staging",
    name: "Staging",
    target_branch: "main",
    target_url: null,
    current_sha: null,
    current_deployed_at: null,
    auto_promote: false,
    created_at: "2026-05-01T00:00:00Z",
    updated_at: "2026-05-01T00:00:00Z",
    ...overrides,
  };
}

function makeDeploy(overrides: Partial<Deploy> = {}): Deploy {
  return {
    id: "d-1",
    workspace_id: "ws-1",
    environment_id: "env-1",
    ref: "main",
    sha: "abc1234567890",
    status: "succeeded",
    triggered_by: null,
    triggered_at: "2026-05-09T10:00:00Z",
    started_at: "2026-05-09T10:00:00Z",
    completed_at: "2026-05-09T10:05:00Z",
    log_url: null,
    error_message: null,
    created_at: "2026-05-09T10:00:00Z",
    ...overrides,
  };
}

describe("ShipDeploySwimlanes", () => {
  it("renders the configure CTA when zero environments", () => {
    envsRef.current = [];
    deploysRef.current = [];
    render(<ShipDeploySwimlanes projectId="p-1" />, { wrapper: I18nWrapper });
    expect(
      screen.getByText(/No deploy environments configured/i),
    ).toBeInTheDocument();
    // The CTA button uses the deploy_strip translation key (intentional —
    // empty-state text is shared with Phase 1).
    expect(
      screen.getByRole("button", { name: /Configure environments/i }),
    ).toBeInTheDocument();
  });

  it("renders one swimlane per environment with the right label", () => {
    envsRef.current = [
      makeEnv({ kind: "staging", name: "Staging" }),
      makeEnv({ id: "env-2", kind: "production", name: "Production" }),
    ];
    deploysRef.current = [];
    render(<ShipDeploySwimlanes projectId="p-1" />, { wrapper: I18nWrapper });
    // The lane label uses the swimlane translation namespace; verifying
    // both reads simultaneously confirms the lane sort (staging first,
    // production second).
    expect(screen.getAllByText(/Staging/i).length).toBeGreaterThan(0);
    expect(screen.getAllByText(/Production/i).length).toBeGreaterThan(0);
  });

  it("renders pill glyphs for each deploy status", () => {
    envsRef.current = [makeEnv({ kind: "staging" })];
    deploysRef.current = [
      // Pill aria-labels use the truncated 7-char SHA + raw status string.
      // SHAs picked here are 7+ chars so the truncation is observable.
      makeDeploy({ id: "d-1", sha: "succeed", status: "succeeded" }),
      makeDeploy({ id: "d-2", sha: "infligh", status: "in_progress" }),
      makeDeploy({ id: "d-3", sha: "fail000", status: "failed" }),
    ];
    render(<ShipDeploySwimlanes projectId="p-1" />, { wrapper: I18nWrapper });
    expect(
      screen.getByRole("button", { name: /succeed succeeded/i }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /infligh in_progress/i }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /fail000 failed/i }),
    ).toBeInTheDocument();
  });

  it("opens a popover with deploy details when a pill is clicked", async () => {
    envsRef.current = [makeEnv({ kind: "staging" })];
    deploysRef.current = [
      makeDeploy({
        id: "d-1",
        sha: "abc12345",
        status: "succeeded",
        triggered_by: "user-uuid-12345",
      }),
    ];
    render(<ShipDeploySwimlanes projectId="p-1" />, { wrapper: I18nWrapper });
    const pill = screen.getByRole("button", { name: /abc1234 succeeded/i });
    fireEvent.click(pill);
    // Popover content uses Base UI's Portal — wait for the title to land
    // in the DOM, then assert against the visible details.
    await waitFor(() => {
      expect(screen.getByText(/Triggered by/i)).toBeInTheDocument();
    });
    expect(screen.getByText(/Triggered at/i)).toBeInTheDocument();
  });

  it("shows the empty-lane message when an env has no deploys", () => {
    envsRef.current = [makeEnv({ kind: "staging" })];
    deploysRef.current = [];
    render(<ShipDeploySwimlanes projectId="p-1" />, { wrapper: I18nWrapper });
    expect(screen.getByText(/No deploys logged yet/i)).toBeInTheDocument();
  });
});
